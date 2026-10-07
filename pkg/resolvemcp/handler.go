package resolvemcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/server"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// Handler exposes registered database models as MCP tools and resources.
type Handler struct {
	allowedFns map[string]struct{} // nil: every function is allowed
	db         common.Database
	registry   common.ModelRegistry
	hooks      *HookRegistry
	mcpServer  *server.MCPServer
	config     Config
	name       string
	version    string
	oauth2Regs []oauth2Registration
	oauthSrv   *security.OAuthServer
	functions  functionRegistry
	confirms   *confirmStore
}

// NewHandler creates a Handler with the given database, model registry, and config.
func NewHandler(db common.Database, registry common.ModelRegistry, cfg Config) *Handler {
	h := &Handler{
		db:        db,
		registry:  registry,
		hooks:     NewHookRegistry(),
		mcpServer: server.NewMCPServer("resolvemcp", "1.0.0", server.WithInstructions(guideFor(cfg.ReadOnly, cfg.AllowFunctionCalls))),
		config:    cfg.withDefaults(),
		confirms:  newConfirmStore(),
		name:      "resolvemcp",
		version:   "1.0.0",
	}
	if len(cfg.AllowedFunctions) > 0 {
		h.allowedFns = make(map[string]struct{}, len(cfg.AllowedFunctions))
		for _, n := range cfg.AllowedFunctions {
			h.allowedFns[n] = struct{}{}
		}
	}
	registerMetaTools(h)
	if cfg.EnableAnnotations && !cfg.ReadOnly {
		registerAnnotationTool(h)
	}
	return h
}

// Hooks returns the hook registry.
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// GetDatabase returns the underlying database.
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

// MCPServer returns the underlying MCP server, e.g. to add custom tools.
func (h *Handler) MCPServer() *server.MCPServer {
	return h.mcpServer
}

// SSEServer returns an http.Handler that serves MCP over SSE.
// Config.BasePath must be set. Config.BaseURL is used when set; if empty it is
// detected automatically from each incoming request.
func (h *Handler) SSEServer() http.Handler {
	if h.config.BaseURL != "" {
		return h.newSSEServer(h.config.BaseURL, h.config.BasePath)
	}
	return &dynamicSSEHandler{h: h}
}

// StreamableHTTPServer returns an http.Handler that serves MCP over the streamable HTTP transport.
// Unlike SSE (which requires two endpoints), streamable HTTP uses a single endpoint for all
// client-server communication (POST for requests, GET for server-initiated messages).
// Mount the returned handler at the desired path; the path itself becomes the MCP endpoint.
func (h *Handler) StreamableHTTPServer() http.Handler {
	return server.NewStreamableHTTPServer(h.mcpServer)
}

// newSSEServer creates a concrete *server.SSEServer for known baseURL and basePath values.
func (h *Handler) newSSEServer(baseURL, basePath string) *server.SSEServer {
	return server.NewSSEServer(
		h.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithStaticBasePath(basePath),
	)
}

// dynamicSSEHandler detects BaseURL from each request and delegates to a cached
// *server.SSEServer per detected baseURL. Used when Config.BaseURL is empty.
type dynamicSSEHandler struct {
	h    *Handler
	mu   sync.Mutex
	pool map[string]*server.SSEServer
}

// maxSSEPool bounds the per-base-URL server cache; Host and X-Forwarded-Proto are client
// controlled, so without a bound a client could grow it forever.
const maxSSEPool = 32

func (d *dynamicSSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !d.h.hostAllowed(r.Host) {
		http.Error(w, "host not allowed", http.StatusBadRequest)
		return
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto != "" && proto != "http" && proto != "https" {
		http.Error(w, "invalid forwarded protocol", http.StatusBadRequest)
		return
	}
	baseURL := requestBaseURL(r)

	d.mu.Lock()
	if d.pool == nil {
		d.pool = make(map[string]*server.SSEServer)
	}
	s, ok := d.pool[baseURL]
	if !ok {
		if len(d.pool) >= maxSSEPool {
			d.mu.Unlock()
			logger.Warn("resolvemcp: SSE base URL cache full; set Config.BaseURL or Config.AllowedHosts")
			http.Error(w, "too many hosts", http.StatusServiceUnavailable)
			return
		}
		s = d.h.newSSEServer(baseURL, d.h.config.BasePath)
		d.pool[baseURL] = s
	}
	d.mu.Unlock()

	s.ServeHTTP(w, r)
}

// hostAllowed reports whether host may be used to build the SSE message URL. With no
// Config.AllowedHosts every host is accepted (the pool cap still applies).
func (h *Handler) hostAllowed(host string) bool {
	if len(h.config.AllowedHosts) == 0 {
		return true
	}
	for _, a := range h.config.AllowedHosts {
		if strings.EqualFold(a, host) {
			return true
		}
	}
	return false
}

// requestBaseURL builds the base URL from an incoming request.
// It honours the X-Forwarded-Proto header for deployments behind a proxy.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

// RegisterModel registers a model. It becomes visible to the fixed meta tools (list_tables,
// select_table, ...); no per-model tools are created.
func (h *Handler) RegisterModel(schema, entity string, model interface{}) error {
	fullName := buildModelName(schema, entity)
	if err := h.registry.RegisterModel(fullName, model); err != nil {
		return err
	}
	return nil
}

// RegisterModelWithRules registers a model and sets per-entity operation rules
// (CanRead, CanCreate, CanUpdate, CanDelete, CanPublic*, SecurityDisabled).
// Requires RegisterSecurityHooks to have been called for the rules to be enforced.
func (h *Handler) RegisterModelWithRules(schema, entity string, model interface{}, rules modelregistry.ModelRules) error {
	reg, ok := h.registry.(*modelregistry.DefaultModelRegistry)
	if !ok {
		return fmt.Errorf("resolvemcp: registry does not support model rules (use NewHandlerWithGORM/Bun/DB)")
	}
	fullName := buildModelName(schema, entity)
	if err := reg.RegisterModelWithRules(fullName, model, rules); err != nil {
		return err
	}
	return nil
}

// SetModelRules updates the operation rules for an already-registered model.
// Requires RegisterSecurityHooks to have been called for the rules to be enforced.
func (h *Handler) SetModelRules(schema, entity string, rules modelregistry.ModelRules) error {
	reg, ok := h.registry.(*modelregistry.DefaultModelRegistry)
	if !ok {
		return fmt.Errorf("resolvemcp: registry does not support model rules (use NewHandlerWithGORM/Bun/DB)")
	}
	return reg.SetModelRules(buildModelName(schema, entity), rules)
}

// buildModelName builds the registry key for a model (same format as resolvespec).
func buildModelName(schema, entity string) string {
	if schema == "" {
		return entity
	}
	return fmt.Sprintf("%s.%s", schema, entity)
}

// getTableName returns the fully qualified table name for a model.
func (h *Handler) getTableName(schema, entity string, model interface{}) string {
	schemaName, tableName := h.getSchemaAndTable(schema, entity, model)
	if schemaName != "" {
		if h.db.DriverName() == "sqlite" {
			return fmt.Sprintf("%s_%s", schemaName, tableName)
		}
		return fmt.Sprintf("%s.%s", schemaName, tableName)
	}
	return tableName
}

func (h *Handler) getSchemaAndTable(defaultSchema, entity string, model interface{}) (schema, table string) {
	if tableProvider, ok := model.(common.TableNameProvider); ok {
		tableName := tableProvider.TableName()
		if idx := strings.LastIndex(tableName, "."); idx != -1 {
			return tableName[:idx], tableName[idx+1:]
		}
		if schemaProvider, ok := model.(common.SchemaProvider); ok {
			return schemaProvider.SchemaName(), tableName
		}
		return defaultSchema, tableName
	}
	if schemaProvider, ok := model.(common.SchemaProvider); ok {
		return schemaProvider.SchemaName(), entity
	}
	return defaultSchema, entity
}

// errRecordNotFound is the one error update and delete return for a row that does not exist,
// is hidden by row security, or vanished mid-write, so ids cannot be enumerated by error text.
var errRecordNotFound = errors.New("record not found")

// recoverPanic catches a panic from the current goroutine and returns it as an error.
// Usage: defer recoverPanic(&returnedErr)
func recoverPanic(err *error) {
	if r := recover(); r != nil {
		logger.Error("[resolvemcp] panic recovered: %v\n%s", r, debug.Stack())
		*err = errInternal
	}
}

// executeRead reads records from the database and returns raw data + metadata.
func (h *Handler) executeRead(ctx context.Context, schema, entity, id string, options common.RequestOptions) (interface{}, *common.Metadata, error) {
	return h.executeReadCounted(ctx, schema, entity, id, options, true)
}

// executeReadCounted is executeRead with control over the total-row COUNT, which costs a full
// scan of the filtered set and is only run when the caller asks for it.
func (h *Handler) executeReadCounted(ctx context.Context, schema, entity, id string, options common.RequestOptions, count bool) (_ interface{}, _ *common.Metadata, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()
	if err := h.checkReadLimits(&options); err != nil {
		return nil, nil, err
	}
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, nil, invalidArg("model not found: %s", buildModelName(schema, entity))
	}

	unwrapped, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid model: %w", err)
	}

	model = unwrapped.Model
	modelType := unwrapped.ModelType
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(h.withModelRules(ctx, schema, entity), schema, entity, tableName, model, unwrapped.ModelPtr)

	validator := common.NewColumnValidator(model)
	options = validator.FilterRequestOptions(options)

	// BeforeHandle hook
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		Model:     model,
		Operation: "read",
		Options:   options,
		ID:        id,
		Tx:        h.db,
	}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, nil, err
	}

	// Hooks and queries share one transaction so transaction-local state set by
	// hooks (e.g. RLS settings) applies to every statement.
	var data interface{}
	var metadata *common.Metadata
	err = h.runInTx(ctx, hookCtx, func(common.Database) error {
		var err error
		data, metadata, err = h.readInTx(ctx, hookCtx, model, modelType, tableName, id, options, count)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return data, metadata, nil
}

// readInTx runs the read hooks and queries on hookCtx.Tx.
func (h *Handler) readInTx(ctx context.Context, hookCtx *HookContext, model interface{}, modelType reflect.Type, tableName, id string, options common.RequestOptions, count bool) (interface{}, *common.Metadata, error) {
	sliceType := reflect.SliceOf(reflect.PointerTo(modelType))
	modelPtr := reflect.New(sliceType).Interface()

	query := hookCtx.Tx.NewSelect().Model(modelPtr)

	tempInstance := reflect.New(modelType).Interface()
	if provider, ok := tempInstance.(common.TableNameProvider); !ok || provider.TableName() == "" {
		query = query.Table(tableName)
	}

	// Column selection
	if len(options.Columns) == 0 && len(options.ComputedColumns) > 0 {
		options.Columns = reflection.GetSQLModelColumns(model)
	}
	for _, col := range options.Columns {
		query = query.Column(reflection.ExtractSourceColumn(col))
	}
	for _, cu := range options.ComputedColumns {
		query = query.ColumnExpr(fmt.Sprintf("(%s) AS %s", cu.Expression, cu.Name))
	}

	// Filters
	query = h.applyFilters(query, options.Filters, model)

	// Custom operators
	for _, customOp := range options.CustomOperators {
		query = query.Where(customOp.SQL)
	}

	// Sorting
	for _, sort := range options.Sort {
		direction := "ASC"
		if strings.EqualFold(sort.Direction, "desc") {
			direction = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sort.Column, direction))
	}

	// Cursor pagination
	if options.CursorForward != "" || options.CursorBackward != "" {
		pkName := reflection.GetPrimaryKeyName(model)
		modelColumns := reflection.GetModelColumns(model)

		if len(options.Sort) == 0 {
			options.Sort = []common.SortOption{{Column: pkName, Direction: "ASC"}}
		}

		// expandJoins is empty for resolvemcp — no custom SQL join support yet
		cursorFilter, err := getCursorFilter(tableName, pkName, modelColumns, options, nil)
		if err != nil {
			return nil, nil, invalidArg("invalid cursor")
		}

		if cursorFilter != "" {
			sanitized := common.SanitizeWhereClause(cursorFilter, reflection.ExtractTableNameOnly(tableName), &options)
			sanitized = common.EnsureOuterParentheses(sanitized)
			if sanitized != "" {
				query = query.Where(sanitized)
			}
		}
	}

	// Count — must happen before preloads are applied; Bun panics when counting with relations.
	total := 0
	if count {
		var err error
		total, err = query.Count(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("error counting records: %w", err)
		}
	}

	// Pagination
	if options.Limit != nil && *options.Limit > 0 {
		query = query.Limit(*options.Limit)
	}
	if options.Offset != nil && *options.Offset > 0 {
		query = query.Offset(*options.Offset)
	}

	// Preloads — applied after count to avoid Bun panic when counting with relations.
	if len(options.Preload) > 0 {
		if err := h.validatePreloads(model, options.Preload); err != nil {
			return nil, nil, err
		}
		var preloadErr error
		query, preloadErr = h.applyPreloads(model, query, options.Preload)
		if preloadErr != nil {
			return nil, nil, fmt.Errorf("failed to apply preloads: %w", preloadErr)
		}
	}

	// BeforeRead hook
	hookCtx.Query = query
	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		return nil, nil, err
	}

	var data interface{}
	if id != "" {
		pkName := reflection.GetPrimaryKeyName(model)
		query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		// Scan through the model configured on the query. Bun rejects Scan with
		// a destination when the query preloads a has-many relation.
		if err := query.ScanModel(ctx); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil, errRecordNotFound
			}
			return nil, nil, fmt.Errorf("query error: %w", err)
		}

		// The configured model is a slice so the same query construction works
		// for both collection and single-record reads. Extract its one result.
		scannedResults := reflect.ValueOf(modelPtr).Elem()
		if scannedResults.Len() == 0 {
			return nil, nil, errRecordNotFound
		}
		data = scannedResults.Index(0).Interface()
	} else {
		// Use the model already configured on the query. This is required by
		// Bun whenever the query includes a has-many preload.
		if err := query.ScanModel(ctx); err != nil {
			return nil, nil, fmt.Errorf("query error: %w", err)
		}
		data = reflect.ValueOf(modelPtr).Elem().Interface()
	}

	limit := 0
	offset := 0
	if options.Limit != nil {
		limit = *options.Limit
	}
	if options.Offset != nil {
		offset = *options.Offset
	}

	// Count is the number of records in this page, not the total.
	var pageCount int64
	if id != "" {
		pageCount = 1
	} else {
		pageCount = int64(reflect.ValueOf(data).Len())
	}

	metadata := &common.Metadata{
		Total:    int64(total),
		Filtered: int64(total),
		Count:    pageCount,
		Limit:    limit,
		Offset:   offset,
	}

	// AfterRead hook
	hookCtx.Result = data
	if err := h.hooks.Execute(AfterRead, hookCtx); err != nil {
		return nil, nil, err
	}

	return data, metadata, nil
}

// executeCreate inserts one or more records.
func (h *Handler) executeCreate(ctx context.Context, schema, entity string, data interface{}) (_ interface{}, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()
	if items, ok := data.([]interface{}); ok && len(items) > h.config.MaxBatch {
		return nil, NewClientError(CodeLimitExceeded, fmt.Sprintf("batch of %d exceeds the maximum of %d", len(items), h.config.MaxBatch))
	}
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, invalidArg("model not found: %s", buildModelName(schema, entity))
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(h.withModelRules(ctx, schema, entity), schema, entity, tableName, model, result.ModelPtr)

	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		Model:     model,
		Operation: "create",
		Data:      data,
		Tx:        h.db,
	}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, err
	}

	pkName := reflection.GetPrimaryKeyName(model)
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}

	var (
		single      bool
		originals   []map[string]interface{}
		insertedIDs []interface{}
		results     []interface{}
	)
	err = h.runInTx(ctx, hookCtx, func(tx common.Database) error {
		if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
			return err
		}
		// Use potentially modified data
		switch v := hookCtx.Data.(type) {
		case map[string]interface{}:
			single = true
			originals = []map[string]interface{}{v}
		case []interface{}:
			originals = make([]map[string]interface{}, 0, len(v))
			for _, item := range v {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					return invalidArg("each item must be an object")
				}
				originals = append(originals, itemMap)
			}
		default:
			return invalidArg("data must be an object or array of objects")
		}

		insertedIDs = make([]interface{}, 0, len(originals))
		for _, itemMap := range originals {
			cols, err := writeColumns(model, itemMap)
			if err != nil {
				return err
			}
			if len(cols) == 0 {
				return invalidArg("no writable fields in data")
			}
			reflection.RemoveNonWritableColumns(model, cols)
			q := tx.NewInsert().Table(tableName)
			for key, value := range cols {
				q = q.Value(key, value)
			}
			if pkName == "" {
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
				insertedIDs = append(insertedIDs, nil)
				continue
			}
			var returnedID interface{}
			if err := q.Returning(pkName).Scan(ctx, &returnedID); err != nil {
				return err
			}
			insertedIDs = append(insertedIDs, returnedID)
		}

		// Re-fetch inside the same transaction to capture DB-generated defaults/triggers, then
		// AfterCreate: the write is only committed when the whole sequence succeeds, so a
		// failure here cannot leave a committed insert behind an error the client may retry.
		results = make([]interface{}, 0, len(insertedIDs))
		for i, pkVal := range insertedIDs {
			if pkVal == nil {
				results = append(results, originals[i])
				continue
			}
			fetchedRecord := reflect.New(modelType).Interface()
			if err := tx.NewSelect().Model(fetchedRecord).
				Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), pkVal).
				ScanModel(ctx); err == nil {
				results = append(results, mergeWithInput(fetchedRecord, originals[i]))
			} else {
				logger.Warn("Failed to re-fetch created record with %s=%v: %v", pkName, pkVal, err)
				results = append(results, originals[i])
			}
		}
		if single {
			hookCtx.Result = results[0]
		} else {
			hookCtx.Result = results
		}
		if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
			return fmt.Errorf("AfterCreate hook failed: %w", err)
		}
		return nil
	})
	if err != nil {
		if single {
			return nil, fmt.Errorf("create error: %w", err)
		}
		if _, ok := hookCtx.Data.([]interface{}); ok {
			return nil, fmt.Errorf("batch create error: %w", err)
		}
		return nil, err
	}
	if single {
		return results[0], nil
	}
	return results, nil
}

// executeUpdate updates a record by ID.
func (h *Handler) executeUpdate(ctx context.Context, schema, entity, id string, data interface{}) (_ interface{}, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, invalidArg("model not found: %s", buildModelName(schema, entity))
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(h.withModelRules(ctx, schema, entity), schema, entity, tableName, model, result.ModelPtr)

	updates, ok := data.(map[string]interface{})
	if !ok {
		return nil, invalidArg("data must be an object")
	}

	if id == "" {
		if idVal, exists := updates["id"]; exists {
			id = fmt.Sprintf("%v", idVal)
		}
	}
	if id == "" {
		return nil, invalidArg("update requires an id")
	}

	pkName := reflection.GetPrimaryKeyName(model)

	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		Model:     model,
		Operation: "update",
		ID:        id,
		Data:      updates,
		Tx:        h.db,
	}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, err
	}

	var updateResult interface{}
	err = h.runInTx(ctx, hookCtx, func(tx common.Database) error {
		if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
			return err
		}
		if modifiedData, ok := hookCtx.Data.(map[string]interface{}); ok {
			updates = modifiedData
		}

		// SET only the validated incoming keys; the primary key addresses the row, it is not
		// rewritten. nil and "" are real values (NULL / empty string).
		setCols, err := writeColumns(model, updates)
		if err != nil {
			return err
		}
		for col := range setCols {
			if strings.EqualFold(col, pkName) {
				delete(setCols, col)
			}
		}
		if len(setCols) == 0 {
			return invalidArg("no updatable fields in data")
		}

		// Load the target through the BeforeScan hooks (row security) so a row the caller
		// cannot see is reported as not found and never written.
		modelType := reflect.TypeOf(model)
		if modelType.Kind() == reflect.Pointer {
			modelType = modelType.Elem()
		}
		existingRecord := reflect.New(modelType).Interface()
		hookCtx.Query = tx.NewSelect().Model(existingRecord).Column("*").
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
			return err
		}
		if err := hookCtx.Query.ScanModel(ctx); err != nil {
			if err == sql.ErrNoRows {
				return errRecordNotFound
			}
			return fmt.Errorf("error fetching existing record: %w", err)
		}

		existingMap := make(map[string]interface{})
		jsonData, err := json.Marshal(existingRecord)
		if err != nil {
			return fmt.Errorf("error marshaling existing record: %w", err)
		}
		if err := json.Unmarshal(jsonData, &existingMap); err != nil {
			return fmt.Errorf("error unmarshaling existing record: %w", err)
		}
		for key, v := range updates {
			existingMap[key] = v
		}

		reflection.RemoveNonWritableColumns(model, setCols)
		q := tx.NewUpdate().Table(tableName).SetMap(setCols).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		res, err := q.Exec(ctx)
		if err != nil {
			return fmt.Errorf("error updating record: %w", err)
		}
		if res.RowsAffected() == 0 {
			return errRecordNotFound
		}

		hookCtx.Result = existingMap

		// Re-fetch inside the same transaction to capture DB-generated changes, then
		// AfterUpdate; see executeCreate.
		fetchedRecord := reflect.New(modelType).Interface()
		if err := tx.NewSelect().Model(fetchedRecord).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id).
			ScanModel(ctx); err == nil {
			if jsonData, marshalErr := json.Marshal(fetchedRecord); marshalErr == nil {
				var fetchedMap map[string]interface{}
				if json.Unmarshal(jsonData, &fetchedMap) == nil {
					existingMap = fetchedMap
					hookCtx.Result = fetchedMap
				}
			}
		}
		updateResult = existingMap
		return h.hooks.Execute(AfterUpdate, hookCtx)
	})
	if err != nil {
		return nil, err
	}
	return updateResult, nil
}

// executeDelete deletes a record by ID.
func (h *Handler) executeDelete(ctx context.Context, schema, entity, id string) (_ interface{}, retErr error) {
	defer recoverPanic(&retErr)
	ctx, cancel := h.callContext(ctx)
	defer cancel()
	if id == "" {
		return nil, invalidArg("delete requires an id")
	}

	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, invalidArg("model not found: %s", buildModelName(schema, entity))
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(h.withModelRules(ctx, schema, entity), schema, entity, tableName, model, result.ModelPtr)

	pkName := reflection.GetPrimaryKeyName(model)

	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		Model:     model,
		Operation: "delete",
		ID:        id,
		Tx:        h.db,
	}
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		return nil, err
	}

	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}

	var recordToDelete interface{}

	err = h.runInTx(ctx, hookCtx, func(tx common.Database) error {
		if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
			return err
		}
		record := reflect.New(modelType).Interface()
		hookCtx.Query = tx.NewSelect().Model(record).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
			return err
		}
		if err := hookCtx.Query.ScanModel(ctx); err != nil {
			if err == sql.ErrNoRows {
				return errRecordNotFound
			}
			return fmt.Errorf("error fetching record: %w", err)
		}

		res, err := tx.NewDelete().Table(tableName).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete error: %w", err)
		}
		if res.RowsAffected() == 0 {
			return errRecordNotFound
		}

		recordToDelete = record
		hookCtx.Result = record
		return h.hooks.Execute(AfterDelete, hookCtx)
	})
	if err != nil {
		return nil, err
	}

	logger.Info("[resolvemcp] Deleted record %s from %s.%s", id, schema, entity)
	return recordToDelete, nil
}

// applyFilters applies all filters with OR grouping logic. model, when
// non-nil, lets citext columns be recognised so LIKE/ILIKE compares them
// natively instead of casting to TEXT (which would defeat a citext index).
func (h *Handler) applyFilters(query common.SelectQuery, filters []common.FilterOption, model interface{}) common.SelectQuery {
	if len(filters) == 0 {
		return query
	}

	i := 0
	for i < len(filters) {
		startORGroup := i+1 < len(filters) && strings.EqualFold(filters[i+1].LogicOperator, "OR")

		if startORGroup {
			orGroup := []common.FilterOption{filters[i]}
			j := i + 1
			for j < len(filters) && strings.EqualFold(filters[j].LogicOperator, "OR") {
				orGroup = append(orGroup, filters[j])
				j++
			}
			query = h.applyFilterGroup(query, orGroup, model)
			i = j
		} else {
			condition, args := h.buildFilterCondition(filters[i], model)
			if condition != "" {
				query = query.Where(condition, args...)
			}
			i++
		}
	}

	return query
}

func (h *Handler) applyFilterGroup(query common.SelectQuery, filters []common.FilterOption, model interface{}) common.SelectQuery {
	var conditions []string
	var args []interface{}

	for _, filter := range filters {
		condition, filterArgs := h.buildFilterCondition(filter, model)
		if condition != "" {
			conditions = append(conditions, condition)
			args = append(args, filterArgs...)
		}
	}

	if len(conditions) == 0 {
		return query
	}
	if len(conditions) == 1 {
		return query.Where(conditions[0], args...)
	}
	return query.Where("("+strings.Join(conditions, " OR ")+")", args...)
}

func (h *Handler) buildFilterCondition(filter common.FilterOption, model interface{}) (condition string, args []interface{}) {
	// citext columns are already case-insensitive; casting to TEXT would
	// switch to case-sensitive matching and defeat a citext index.
	likeColumn := filter.Column
	if !reflection.IsCitextColumn(model, filter.Column) {
		likeColumn = fmt.Sprintf("CAST(%s AS TEXT)", filter.Column)
	}

	switch filter.Operator {
	case "eq", "=":
		return fmt.Sprintf("%s = ?", filter.Column), []interface{}{filter.Value}
	case "neq", "!=", "<>":
		return fmt.Sprintf("%s != ?", filter.Column), []interface{}{filter.Value}
	case "gt", ">":
		return fmt.Sprintf("%s > ?", filter.Column), []interface{}{filter.Value}
	case "gte", ">=":
		return fmt.Sprintf("%s >= ?", filter.Column), []interface{}{filter.Value}
	case "lt", "<":
		return fmt.Sprintf("%s < ?", filter.Column), []interface{}{filter.Value}
	case "lte", "<=":
		return fmt.Sprintf("%s <= ?", filter.Column), []interface{}{filter.Value}
	case "like":
		return fmt.Sprintf("%s LIKE ?", likeColumn), []interface{}{filter.Value}
	case "ilike":
		return fmt.Sprintf("%s ILIKE ?", likeColumn), []interface{}{filter.Value}
	case "in":
		condition, args := common.BuildInCondition(filter.Column, filter.Value)
		return condition, args
	case "is_null":
		return fmt.Sprintf("%s IS NULL", filter.Column), nil
	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", filter.Column), nil
	}
	return "", nil
}

// mergeWithInput merges a database record with the original request data.
// DB values take precedence (capturing triggers/defaults), while extra
// input keys that have no DB column are preserved in the response.
func mergeWithInput(dbRecord interface{}, input map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(input))
	for k, v := range input {
		result[k] = v
	}
	jsonData, err := json.Marshal(dbRecord)
	if err != nil {
		return result
	}
	var dbMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &dbMap); err != nil {
		return result
	}
	for k, v := range dbMap {
		result[k] = v
	}
	return result
}

func (h *Handler) applyPreloads(model interface{}, query common.SelectQuery, preloads []common.PreloadOption) (common.SelectQuery, error) {
	for i := range preloads {
		preload := &preloads[i]
		if preload.Relation == "" {
			continue
		}
		query = query.PreloadRelation(preload.Relation)
	}
	return query, nil
}

// runInTx runs body in a transaction with hookCtx.Tx set to it and OnTxBegin fired
// first. Every transaction the handler opens goes through here.
func (h *Handler) runInTx(ctx context.Context, hookCtx *HookContext, body func(tx common.Database) error) error {
	return common.RunRequestTx(ctx, h.db, hookCtx, func() error {
		return h.hooks.Execute(OnTxBegin, hookCtx)
	}, body)
}

// callContext bounds one tool call by Config.QueryTimeout.
func (h *Handler) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.config.QueryTimeout)
}

// checkReadLimits applies the paging caps to options in place: a missing limit takes
// DefaultLimit, a larger one is clamped to MaxLimit, and an offset above MaxOffset is rejected.
func (h *Handler) checkReadLimits(options *common.RequestOptions) error {
	limit := h.config.DefaultLimit
	if options.Limit != nil && *options.Limit > 0 {
		limit = *options.Limit
	}
	if limit > h.config.MaxLimit {
		limit = h.config.MaxLimit
	}
	options.Limit = &limit
	if options.Offset != nil {
		if *options.Offset < 0 {
			return invalidArg("offset must not be negative")
		}
		if *options.Offset > h.config.MaxOffset {
			return NewClientError(CodeLimitExceeded, fmt.Sprintf("offset exceeds the maximum of %d; use cursor paging", h.config.MaxOffset))
		}
	}
	return nil
}

var preloadSegmentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validatePreloads checks preload paths against the model: the first segment must be one of
// the model's relations and the path may not be deeper than Config.MaxPreloadDepth.
func (h *Handler) validatePreloads(model interface{}, preloads []common.PreloadOption) error {
	relations := map[string]bool{}
	for _, name := range buildModelInfo("", "", model).relationNames {
		relations[strings.ToLower(name)] = true
	}
	for i := range preloads {
		segments := strings.Split(preloads[i].Relation, ".")
		if len(segments) > h.config.MaxPreloadDepth {
			return NewClientError(CodeLimitExceeded, fmt.Sprintf("preload depth exceeds the maximum of %d", h.config.MaxPreloadDepth))
		}
		for _, seg := range segments {
			if !preloadSegmentRe.MatchString(seg) {
				return invalidArg("invalid preload relation")
			}
		}
		if !relations[strings.ToLower(segments[0])] {
			return invalidArg("unknown relation %q", segments[0])
		}
	}
	return nil
}
