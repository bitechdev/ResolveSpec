package resolvemcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/server"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Handler exposes registered database models as MCP tools and resources.
type Handler struct {
	db        common.Database
	registry  common.ModelRegistry
	hooks     *HookRegistry
	mcpServer *server.MCPServer
	config    Config
	name      string
	version   string
}

// NewHandler creates a Handler with the given database, model registry, and config.
func NewHandler(db common.Database, registry common.ModelRegistry, cfg Config) *Handler {
	h := &Handler{
		db:        db,
		registry:  registry,
		hooks:     NewHookRegistry(),
		mcpServer: server.NewMCPServer("resolvemcp", "1.0.0"),
		config:    cfg,
		name:      "resolvemcp",
		version:   "1.0.0",
	}
	registerAnnotationTool(h)
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

func (d *dynamicSSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	baseURL := requestBaseURL(r)

	d.mu.Lock()
	if d.pool == nil {
		d.pool = make(map[string]*server.SSEServer)
	}
	s, ok := d.pool[baseURL]
	if !ok {
		s = d.h.newSSEServer(baseURL, d.h.config.BasePath)
		d.pool[baseURL] = s
	}
	d.mu.Unlock()

	s.ServeHTTP(w, r)
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

// RegisterModel registers a model and immediately exposes it as MCP tools and a resource.
func (h *Handler) RegisterModel(schema, entity string, model interface{}) error {
	fullName := buildModelName(schema, entity)
	if err := h.registry.RegisterModel(fullName, model); err != nil {
		return err
	}
	registerModelTools(h, schema, entity, model)
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
	registerModelTools(h, schema, entity, model)
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

// executeRead reads records from the database and returns raw data + metadata.
func (h *Handler) executeRead(ctx context.Context, schema, entity, id string, options common.RequestOptions) (interface{}, *common.Metadata, error) {
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, nil, fmt.Errorf("model not found: %w", err)
	}

	unwrapped, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid model: %w", err)
	}

	model = unwrapped.Model
	modelType := unwrapped.ModelType
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(ctx, schema, entity, tableName, model, unwrapped.ModelPtr)

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

	sliceType := reflect.SliceOf(reflect.PointerTo(modelType))
	modelPtr := reflect.New(sliceType).Interface()

	query := h.db.NewSelect().Model(modelPtr)

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

	// Preloads
	if len(options.Preload) > 0 {
		var err error
		query, err = h.applyPreloads(model, query, options.Preload)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to apply preloads: %w", err)
		}
	}

	// Filters
	query = h.applyFilters(query, options.Filters)

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
			return nil, nil, fmt.Errorf("cursor error: %w", err)
		}

		if cursorFilter != "" {
			sanitized := common.SanitizeWhereClause(cursorFilter, reflection.ExtractTableNameOnly(tableName), &options)
			sanitized = common.EnsureOuterParentheses(sanitized)
			if sanitized != "" {
				query = query.Where(sanitized)
			}
		}
	}

	// Count
	total, err := query.Count(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error counting records: %w", err)
	}

	// Pagination
	if options.Limit != nil && *options.Limit > 0 {
		query = query.Limit(*options.Limit)
	}
	if options.Offset != nil && *options.Offset > 0 {
		query = query.Offset(*options.Offset)
	}

	// BeforeRead hook
	hookCtx.Query = query
	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		return nil, nil, err
	}

	var data interface{}
	if id != "" {
		singleResult := reflect.New(modelType).Interface()
		pkName := reflection.GetPrimaryKeyName(singleResult)
		query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		if err := query.Scan(ctx, singleResult); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil, fmt.Errorf("record not found")
			}
			return nil, nil, fmt.Errorf("query error: %w", err)
		}
		data = singleResult
	} else {
		if err := query.Scan(ctx, modelPtr); err != nil {
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
func (h *Handler) executeCreate(ctx context.Context, schema, entity string, data interface{}) (interface{}, error) {
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(ctx, schema, entity, tableName, model, result.ModelPtr)

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
	if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
		return nil, err
	}

	// Use potentially modified data
	data = hookCtx.Data

	switch v := data.(type) {
	case map[string]interface{}:
		query := h.db.NewInsert().Table(tableName)
		for key, value := range v {
			query = query.Value(key, value)
		}
		if _, err := query.Exec(ctx); err != nil {
			return nil, fmt.Errorf("create error: %w", err)
		}
		hookCtx.Result = v
		if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
			return nil, fmt.Errorf("AfterCreate hook failed: %w", err)
		}
		return v, nil

	case []interface{}:
		results := make([]interface{}, 0, len(v))
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			for _, item := range v {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					return fmt.Errorf("each item must be an object")
				}
				q := tx.NewInsert().Table(tableName)
				for key, value := range itemMap {
					q = q.Value(key, value)
				}
				if _, err := q.Exec(ctx); err != nil {
					return err
				}
				results = append(results, item)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("batch create error: %w", err)
		}
		hookCtx.Result = results
		if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
			return nil, fmt.Errorf("AfterCreate hook failed: %w", err)
		}
		return results, nil

	default:
		return nil, fmt.Errorf("data must be an object or array of objects")
	}
}

// executeUpdate updates a record by ID.
func (h *Handler) executeUpdate(ctx context.Context, schema, entity, id string, data interface{}) (interface{}, error) {
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(ctx, schema, entity, tableName, model, result.ModelPtr)

	updates, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data must be an object")
	}

	if id == "" {
		if idVal, exists := updates["id"]; exists {
			id = fmt.Sprintf("%v", idVal)
		}
	}
	if id == "" {
		return nil, fmt.Errorf("update requires an ID")
	}

	pkName := reflection.GetPrimaryKeyName(model)

	var updateResult interface{}
	err = h.db.RunInTransaction(ctx, func(tx common.Database) error {
		// Read existing record
		modelType := reflect.TypeOf(model)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}
		existingRecord := reflect.New(modelType).Interface()
		selectQuery := tx.NewSelect().Model(existingRecord).Column("*").
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)

		if err := selectQuery.ScanModel(ctx); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("no records found to update")
			}
			return fmt.Errorf("error fetching existing record: %w", err)
		}

		// Convert to map
		existingMap := make(map[string]interface{})
		jsonData, err := json.Marshal(existingRecord)
		if err != nil {
			return fmt.Errorf("error marshaling existing record: %w", err)
		}
		if err := json.Unmarshal(jsonData, &existingMap); err != nil {
			return fmt.Errorf("error unmarshaling existing record: %w", err)
		}

		hookCtx := &HookContext{
			Context:   ctx,
			Handler:   h,
			Schema:    schema,
			Entity:    entity,
			Model:     model,
			Operation: "update",
			ID:        id,
			Data:      updates,
			Tx:        tx,
		}
		if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
			return err
		}
		if modifiedData, ok := hookCtx.Data.(map[string]interface{}); ok {
			updates = modifiedData
		}

		// Merge non-nil, non-empty values
		for key, newValue := range updates {
			if newValue == nil {
				continue
			}
			if strVal, ok := newValue.(string); ok && strVal == "" {
				continue
			}
			existingMap[key] = newValue
		}

		q := tx.NewUpdate().Table(tableName).SetMap(existingMap).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		res, err := q.Exec(ctx)
		if err != nil {
			return fmt.Errorf("error updating record: %w", err)
		}
		if res.RowsAffected() == 0 {
			return fmt.Errorf("no records found to update")
		}

		updateResult = existingMap
		hookCtx.Result = updateResult
		return h.hooks.Execute(AfterUpdate, hookCtx)
	})

	if err != nil {
		return nil, err
	}
	return updateResult, nil
}

// executeDelete deletes a record by ID.
func (h *Handler) executeDelete(ctx context.Context, schema, entity, id string) (interface{}, error) {
	if id == "" {
		return nil, fmt.Errorf("delete requires an ID")
	}

	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return nil, fmt.Errorf("model not found: %w", err)
	}

	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		return nil, fmt.Errorf("invalid model: %w", err)
	}

	model = result.Model
	tableName := h.getTableName(schema, entity, model)
	ctx = withRequestData(ctx, schema, entity, tableName, model, result.ModelPtr)

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
	if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
		return nil, err
	}

	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	var recordToDelete interface{}

	err = h.db.RunInTransaction(ctx, func(tx common.Database) error {
		record := reflect.New(modelType).Interface()
		selectQuery := tx.NewSelect().Model(record).
			Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
		if err := selectQuery.ScanModel(ctx); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("record not found")
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
			return fmt.Errorf("record not found or already deleted")
		}

		recordToDelete = record
		hookCtx.Tx = tx
		hookCtx.Result = record
		return h.hooks.Execute(AfterDelete, hookCtx)
	})
	if err != nil {
		return nil, err
	}

	logger.Info("[resolvemcp] Deleted record %s from %s.%s", id, schema, entity)
	return recordToDelete, nil
}

// applyFilters applies all filters with OR grouping logic.
func (h *Handler) applyFilters(query common.SelectQuery, filters []common.FilterOption) common.SelectQuery {
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
			query = h.applyFilterGroup(query, orGroup)
			i = j
		} else {
			condition, args := h.buildFilterCondition(filters[i])
			if condition != "" {
				query = query.Where(condition, args...)
			}
			i++
		}
	}

	return query
}

func (h *Handler) applyFilterGroup(query common.SelectQuery, filters []common.FilterOption) common.SelectQuery {
	var conditions []string
	var args []interface{}

	for _, filter := range filters {
		condition, filterArgs := h.buildFilterCondition(filter)
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

func (h *Handler) buildFilterCondition(filter common.FilterOption) (string, []interface{}) {
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
		return fmt.Sprintf("%s LIKE ?", filter.Column), []interface{}{filter.Value}
	case "ilike":
		return fmt.Sprintf("%s ILIKE ?", filter.Column), []interface{}{filter.Value}
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

func (h *Handler) applyPreloads(model interface{}, query common.SelectQuery, preloads []common.PreloadOption) (common.SelectQuery, error) {
	for _, preload := range preloads {
		if preload.Relation == "" {
			continue
		}
		query = query.PreloadRelation(preload.Relation)
	}
	return query, nil
}
