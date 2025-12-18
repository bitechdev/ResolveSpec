package restheadspec

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// FallbackHandler is a function that handles requests when no model is found
// It receives the same parameters as the Handle method
type FallbackHandler func(w common.ResponseWriter, r common.Request, params map[string]string)

// Handler handles API requests using database and model abstractions
// This handler reads filters, columns, and options from HTTP headers
type Handler struct {
	db               common.Database
	registry         common.ModelRegistry
	hooks            *HookRegistry
	nestedProcessor  *common.NestedCUDProcessor
	fallbackHandler  FallbackHandler
	openAPIGenerator func() (string, error)
}

// NewHandler creates a new API handler with database and registry abstractions
func NewHandler(db common.Database, registry common.ModelRegistry) *Handler {
	handler := &Handler{
		db:       db,
		registry: registry,
		hooks:    NewHookRegistry(),
	}
	// Initialize nested processor
	handler.nestedProcessor = common.NewNestedCUDProcessor(db, registry, handler)
	return handler
}

// GetDatabase returns the underlying database connection
// Implements common.SpecHandler interface
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

// Hooks returns the hook registry for this handler
// Use this to register custom hooks for operations
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// SetFallbackHandler sets a fallback handler to be called when no model is found
// If not set, the handler will simply return (pass through to next route)
func (h *Handler) SetFallbackHandler(fallback FallbackHandler) {
	h.fallbackHandler = fallback
}

// handlePanic is a helper function to handle panics with stack traces
func (h *Handler) handlePanic(w common.ResponseWriter, method string, err interface{}) {
	stack := debug.Stack()
	logger.Error("Panic in %s: %v\nStack trace:\n%s", method, err, string(stack))
	h.sendError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("Internal server error in %s", method), fmt.Errorf("%v", err))
}

// Handle processes API requests through router-agnostic interface
// Options are read from HTTP headers instead of request body
func (h *Handler) Handle(w common.ResponseWriter, r common.Request, params map[string]string) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "Handle", err)
		}
	}()

	// Check for ?openapi query parameter
	if r.UnderlyingRequest().URL.Query().Get("openapi") != "" {
		h.HandleOpenAPI(w, r)
		return
	}

	ctx := r.UnderlyingRequest().Context()

	schema := params["schema"]
	entity := params["entity"]
	id := params["id"]

	// Determine operation based on HTTP method
	method := r.Method()

	logger.Info("Handling %s request for %s.%s", method, schema, entity)

	// Get model and populate context with request-scoped data
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		// Model not found - call fallback handler if set, otherwise pass through
		logger.Debug("Model not found for %s.%s", schema, entity)
		if h.fallbackHandler != nil {
			logger.Debug("Calling fallback handler for %s.%s", schema, entity)
			h.fallbackHandler(w, r, params)
		} else {
			logger.Debug("No fallback handler set, passing through to next route")
		}
		return
	}

	// Validate and unwrap model using common utility
	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		logger.Error("Model for %s.%s validation failed: %v", schema, entity, err)
		h.sendError(w, http.StatusInternalServerError, "invalid_model_type", err.Error(), err)
		return
	}

	model = result.Model
	modelPtr := result.ModelPtr
	tableName := h.getTableName(schema, entity, model)

	// Parse options from headers - this now includes relation name resolution
	options := h.parseOptionsFromHeaders(r, model)

	// Validate and filter columns in options (log warnings for invalid columns)
	validator := common.NewColumnValidator(model)
	options = h.filterExtendedOptions(validator, options, model)

	// Add request-scoped data to context (including options)
	ctx = WithRequestData(ctx, schema, entity, tableName, model, modelPtr, options)

	switch method {
	case "GET":
		if id != "" {
			// GET with ID - read single record
			h.handleRead(ctx, w, id, options)
		} else {
			// GET without ID - read multiple records
			h.handleRead(ctx, w, "", options)
		}
	case "POST":
		// Read request body
		body, err := r.Body()
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body", err)
			return
		}

		// Try to detect if this is a meta operation request
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(body, &bodyMap); err == nil {
			if operation, ok := bodyMap["operation"].(string); ok && operation == "meta" {
				logger.Info("Detected meta operation request for %s.%s", schema, entity)
				h.handleMeta(ctx, w, schema, entity, model)
				return
			}
		}

		// Not a meta operation, proceed with normal create/update
		var data interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			logger.Error("Failed to decode request body: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body", err)
			return
		}
		validId, _ := strconv.ParseInt(id, 10, 64)
		if validId > 0 {
			h.handleUpdate(ctx, w, id, nil, data, options)
		} else {
			h.handleCreate(ctx, w, data, options)
		}
	case "PUT", "PATCH":
		// Update operation

		body, err := r.Body()
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body", err)
			return
		}
		var data interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			logger.Error("Failed to decode request body: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body", err)
			return
		}
		h.handleUpdate(ctx, w, id, nil, data, options)
	case "DELETE":
		// Try to read body for batch delete support
		var data interface{}
		body, err := r.Body()
		if err == nil && len(body) > 0 {
			if err := json.Unmarshal(body, &data); err != nil {
				logger.Warn("Failed to decode delete request body (will try single delete): %v", err)
				data = nil
			}
		}
		h.handleDelete(ctx, w, id, data)
	default:
		logger.Error("Invalid HTTP method: %s", method)
		h.sendError(w, http.StatusMethodNotAllowed, "invalid_method", "Invalid HTTP method", nil)
	}
}

// HandleGet processes GET requests for metadata
func (h *Handler) HandleGet(w common.ResponseWriter, r common.Request, params map[string]string) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "HandleGet", err)
		}
	}()

	// Check for ?openapi query parameter
	if r.UnderlyingRequest().URL.Query().Get("openapi") != "" {
		h.HandleOpenAPI(w, r)
		return
	}

	schema := params["schema"]
	entity := params["entity"]

	logger.Info("Getting metadata for %s.%s", schema, entity)

	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		// Model not found - call fallback handler if set, otherwise pass through
		logger.Debug("Model not found for %s.%s", schema, entity)
		if h.fallbackHandler != nil {
			logger.Debug("Calling fallback handler for %s.%s", schema, entity)
			h.fallbackHandler(w, r, params)
		} else {
			logger.Debug("No fallback handler set, passing through to next route")
		}
		return
	}

	// Parse request options from headers to get response format settings
	options := h.parseOptionsFromHeaders(r, model)

	tableMetadata := h.generateMetadata(schema, entity, model)
	// Send with formatted response to respect DetailApi/SimpleApi/Syncfusion format
	// Create empty metadata for response wrapper
	responseMetadata := &common.Metadata{
		Total:    0,
		Filtered: 0,
		Count:    0,
		Limit:    0,
		Offset:   0,
	}
	h.sendFormattedResponse(w, tableMetadata, responseMetadata, options)
}

// handleMeta processes meta operation requests
func (h *Handler) handleMeta(ctx context.Context, w common.ResponseWriter, schema, entity string, model interface{}) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "handleMeta", err)
		}
	}()

	logger.Info("Getting metadata for %s.%s via meta operation", schema, entity)

	metadata := h.generateMetadata(schema, entity, model)
	h.sendResponse(w, metadata, nil)
}

// parseOptionsFromHeaders is now implemented in headers.go

func (h *Handler) handleRead(ctx context.Context, w common.ResponseWriter, id string, options ExtendedRequestOptions) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "handleRead", err)
		}
	}()

	schema := GetSchema(ctx)
	entity := GetEntity(ctx)
	tableName := GetTableName(ctx)
	model := GetModel(ctx)

	if id == "" {
		options.SingleRecordAsObject = false
	}

	// Execute BeforeRead hooks
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		TableName: tableName,
		Model:     model,
		Options:   options,
		ID:        id,
		Writer:    w,
		Tx:        h.db,
	}

	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		logger.Error("BeforeRead hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	// Validate and unwrap model type to get base struct
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Error("Model must be a struct type, got %v for %s.%s", modelType, schema, entity)
		h.sendError(w, http.StatusInternalServerError, "invalid_model", "Model must be a struct type", fmt.Errorf("invalid model type: %v", modelType))
		return
	}

	// Create a pointer to a slice of pointers to the model type for query results
	modelPtr := reflect.New(reflect.SliceOf(reflect.PointerTo(modelType))).Interface()

	logger.Info("Reading records from %s.%s", schema, entity)

	// Start with Model() using the slice pointer to avoid "Model(nil)" errors in Count()
	// Bun's Model() accepts both single pointers and slice pointers
	query := h.db.NewSelect().Model(modelPtr)

	// Only set Table() if the model doesn't provide a table name via the underlying type
	// Create a temporary instance to check for TableNameProvider
	tempInstance := reflect.New(modelType).Interface()
	if provider, ok := tempInstance.(common.TableNameProvider); !ok || provider.TableName() == "" {
		query = query.Table(tableName)
	}

	// If we have computed columns/expressions but options.Columns is empty,
	// populate it with all model columns first since computed columns are additions
	if len(options.Columns) == 0 && (len(options.ComputedQL) > 0 || len(options.ComputedColumns) > 0) {
		logger.Debug("Populating options.Columns with all model columns since computed columns are additions")
		options.Columns = reflection.GetSQLModelColumns(model)
	}

	// Apply ComputedQL fields if any
	if len(options.ComputedQL) > 0 {
		for colName, colExpr := range options.ComputedQL {
			logger.Debug("Applying computed column: %s", colName)
			if strings.Contains(colName, "cql") {
				query = query.ColumnExpr(fmt.Sprintf("(%s)::text AS %s", colExpr, colName))
			} else {
				query = query.ColumnExpr(fmt.Sprintf("(%s)AS %s", colExpr, colName))
			}

			for colIndex := range options.Columns {
				if options.Columns[colIndex] == colName {
					// Remove the computed column from the selected columns to avoid duplication
					options.Columns = append(options.Columns[:colIndex], options.Columns[colIndex+1:]...)
					break
				}
			}
		}
	}

	if len(options.ComputedColumns) > 0 {
		for _, cu := range options.ComputedColumns {
			logger.Debug("Applying computed column: %s", cu.Name)
			if strings.Contains(cu.Name, "cql") {
				query = query.ColumnExpr(fmt.Sprintf("(%s)::text AS %s", cu.Expression, cu.Name))
			} else {
				query = query.ColumnExpr(fmt.Sprintf("(%s) AS %s", cu.Expression, cu.Name))
			}

			for colIndex := range options.Columns {
				if options.Columns[colIndex] == cu.Name {
					// Remove the computed column from the selected columns to avoid duplication
					options.Columns = append(options.Columns[:colIndex], options.Columns[colIndex+1:]...)
					break
				}
			}
		}
	}

	// Apply column selection
	if len(options.Columns) > 0 {
		logger.Debug("Selecting columns: %v", options.Columns)
		for _, col := range options.Columns {
			query = query.Column(reflection.ExtractSourceColumn(col))
		}

	}

	// Apply expand (Just expand to Preload for now)
	for _, expand := range options.Expand {
		logger.Debug("Applying expand: %s", expand.Relation)
		sorts := make([]common.SortOption, 0)
		for _, s := range strings.Split(expand.Sort, ",") {
			if s == "" {
				continue
			}
			dir := "ASC"
			if strings.HasPrefix(s, "-") || strings.HasSuffix(strings.ToUpper(s), " DESC") {
				dir = "DESC"
				s = strings.TrimPrefix(s, "-")
				s = strings.TrimSuffix(strings.ToLower(s), " desc")
			}
			sorts = append(sorts, common.SortOption{
				Column: s, Direction: dir,
			})
		}
		// Note: Expand would require JOIN implementation
		// For now, we'll use Preload as a fallback
		// query = query.Preload(expand.Relation)
		if options.Preload == nil {
			options.Preload = make([]common.PreloadOption, 0)
		}
		skip := false
		for idx := range options.Preload {
			if options.Preload[idx].Relation == expand.Relation {
				skip = true
				continue
			}
		}
		if !skip {
			options.Preload = append(options.Preload, common.PreloadOption{
				Relation: expand.Relation,
				Columns:  expand.Columns,
				Sort:     sorts,
				Where:    expand.Where,
			})
		}
	}

	// Apply preloading
	for idx := range options.Preload {
		preload := options.Preload[idx]
		logger.Debug("Applying preload: %s", preload.Relation)

		// Validate and fix WHERE clause to ensure it contains the relation prefix
		if len(preload.Where) > 0 {
			fixedWhere, err := common.ValidateAndFixPreloadWhere(preload.Where, preload.Relation)
			if err != nil {
				logger.Error("Invalid preload WHERE clause for relation '%s': %v", preload.Relation, err)
				h.sendError(w, http.StatusBadRequest, "invalid_preload_where",
					fmt.Sprintf("Invalid preload WHERE clause for relation '%s'", preload.Relation), err)
				return
			}
			preload.Where = fixedWhere
		}

		// Apply the preload with recursive support
		query = h.applyPreloadWithRecursion(query, preload, options.Preload, model, 0)
	}

	// Apply DISTINCT if requested
	if options.Distinct {
		logger.Debug("Applying DISTINCT")
		// Note: DISTINCT implementation depends on ORM support
		// This may need to be handled differently per database adapter
	}

	// Apply filters - validate and adjust for column types first
	for i := range options.Filters {
		filter := &options.Filters[i]

		// Validate and adjust filter based on column type
		castInfo := h.ValidateAndAdjustFilterForColumnType(filter, model)

		// Default to AND if LogicOperator is not set
		logicOp := filter.LogicOperator
		if logicOp == "" {
			logicOp = "AND"
		}

		logger.Debug("Applying filter: %s %s %v (needsCast=%v, logic=%s)", filter.Column, filter.Operator, filter.Value, castInfo.NeedsCast, logicOp)
		query = h.applyFilter(query, *filter, tableName, castInfo.NeedsCast, logicOp)
	}

	// Apply custom SQL WHERE clause (AND condition)
	if options.CustomSQLWhere != "" {
		logger.Debug("Applying custom SQL WHERE: %s", options.CustomSQLWhere)
		// Sanitize and allow preload table prefixes since custom SQL may reference multiple tables
		sanitizedWhere := common.SanitizeWhereClause(options.CustomSQLWhere, reflection.ExtractTableNameOnly(tableName), &options.RequestOptions)
		if sanitizedWhere != "" {
			query = query.Where(sanitizedWhere)
		}
	}

	// Apply custom SQL WHERE clause (OR condition)
	if options.CustomSQLOr != "" {
		logger.Debug("Applying custom SQL OR: %s", options.CustomSQLOr)
		// Sanitize and allow preload table prefixes since custom SQL may reference multiple tables
		sanitizedOr := common.SanitizeWhereClause(options.CustomSQLOr, reflection.ExtractTableNameOnly(tableName), &options.RequestOptions)
		if sanitizedOr != "" {
			query = query.WhereOr(sanitizedOr)
		}
	}

	// If ID is provided, filter by ID
	if id != "" {
		pkName := reflection.GetPrimaryKeyName(model)
		logger.Debug("Filtering by ID=%s: %s", pkName, id)

		query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
	}

	// Apply sorting
	for _, sort := range options.Sort {
		direction := "ASC"
		if strings.EqualFold(sort.Direction, "desc") {
			direction = "DESC"
		}
		logger.Debug("Applying sort: %s %s", sort.Column, direction)

		// Check if it's an expression (enclosed in brackets) - use directly without quoting
		if strings.HasPrefix(sort.Column, "(") && strings.HasSuffix(sort.Column, ")") {
			// For expressions, pass as raw SQL to prevent auto-quoting
			query = query.OrderExpr(fmt.Sprintf("%s %s", sort.Column, direction))
		} else {
			// Regular column - let Bun handle quoting
			query = query.Order(fmt.Sprintf("%s %s", sort.Column, direction))
		}
	}

	// Get total count before pagination (unless skip count is requested)
	var total int
	if !options.SkipCount {
		// Try to get from cache first (unless SkipCache is true)
		var cachedTotalData *cachedTotal
		var cacheKey string

		if !options.SkipCache {
			// Build cache key from query parameters
			// Convert expand options to interface slice for the cache key builder
			expandOpts := make([]interface{}, len(options.Expand))
			for i, exp := range options.Expand {
				expandOpts[i] = map[string]interface{}{
					"relation": exp.Relation,
					"where":    exp.Where,
				}
			}

			cacheKeyHash := buildExtendedQueryCacheKey(
				tableName,
				options.Filters,
				options.Sort,
				options.CustomSQLWhere,
				options.CustomSQLOr,
				expandOpts,
				options.Distinct,
				options.CursorForward,
				options.CursorBackward,
			)
			cacheKey = getQueryTotalCacheKey(cacheKeyHash)

			// Try to retrieve from cache
			cachedTotalData = &cachedTotal{}
			err := cache.GetDefaultCache().Get(ctx, cacheKey, cachedTotalData)
			if err == nil {
				total = cachedTotalData.Total
				logger.Debug("Total records (from cache): %d", total)
			} else {
				logger.Debug("Cache miss for query total")
				cachedTotalData = nil
			}
		}

		// If not in cache or cache skip, execute count query
		if cachedTotalData == nil {
			count, err := query.Count(ctx)
			if err != nil {
				logger.Error("Error counting records: %v", err)
				h.sendError(w, http.StatusInternalServerError, "query_error", "Error counting records", err)
				return
			}
			total = count
			logger.Debug("Total records (from query): %d", total)

			// Store in cache with schema and table tags (if caching is enabled)
			if !options.SkipCache && cacheKey != "" {
				cacheTTL := time.Minute * 2 // Default 2 minutes TTL
				if err := setQueryTotalCache(ctx, cacheKey, total, schema, tableName, cacheTTL); err != nil {
					logger.Warn("Failed to cache query total: %v", err)
					// Don't fail the request if caching fails
				} else {
					logger.Debug("Cached query total with key: %s", cacheKey)
				}
			}
		}
	} else {
		logger.Debug("Skipping count as requested")
		total = -1 // Indicate count was skipped
	}

	// Apply pagination
	if options.Limit != nil && *options.Limit > 0 {
		logger.Debug("Applying limit: %d", *options.Limit)
		query = query.Limit(*options.Limit)
	}
	if options.Offset != nil && *options.Offset > 0 {
		logger.Debug("Applying offset: %d", *options.Offset)
		query = query.Offset(*options.Offset)
	}

	// Apply cursor-based pagination
	if len(options.CursorForward) > 0 || len(options.CursorBackward) > 0 {
		logger.Debug("Applying cursor pagination")

		// Get primary key name
		pkName := reflection.GetPrimaryKeyName(model)

		// Extract model columns for validation using the generic database function
		modelColumns := reflection.GetModelColumns(model)

		// Build expand joins map (if needed in future)
		var expandJoins map[string]string
		if len(options.Expand) > 0 {
			expandJoins = make(map[string]string)
			// TODO: Build actual JOIN SQL for each expand relation
			// For now, pass empty map as joins are handled via Preload
		}

		// Get cursor filter SQL
		cursorFilter, err := options.GetCursorFilter(tableName, pkName, modelColumns, expandJoins)
		if err != nil {
			logger.Error("Error building cursor filter: %v", err)
			h.sendError(w, http.StatusBadRequest, "cursor_error", "Invalid cursor pagination", err)
			return
		}

		// Apply cursor filter to query
		if cursorFilter != "" {
			logger.Debug("Applying cursor filter: %s", cursorFilter)
			sanitizedCursor := common.SanitizeWhereClause(cursorFilter, reflection.ExtractTableNameOnly(tableName), &options.RequestOptions)
			if sanitizedCursor != "" {
				query = query.Where(sanitizedCursor)
			}
		}
	}

	// Execute BeforeScan hooks - pass query chain so hooks can modify it
	hookCtx.Query = query
	if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
		logger.Error("BeforeScan hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	// Use potentially modified query from hook context
	if modifiedQuery, ok := hookCtx.Query.(common.SelectQuery); ok {
		query = modifiedQuery
	}

	// Execute query - modelPtr was already created earlier
	if err := query.ScanModel(ctx); err != nil {
		logger.Error("Error executing query: %v", err)
		h.sendError(w, http.StatusInternalServerError, "query_error", "Error executing query", err)
		return
	}

	// Check if a specific ID was requested but no record was found
	resultCount := reflection.Len(modelPtr)
	if id != "" && resultCount == 0 {
		logger.Warn("Record not found for ID: %s", id)
		h.sendError(w, http.StatusNotFound, "not_found", "Record not found", nil)
		return
	}

	limit := 0
	if options.Limit != nil {
		limit = *options.Limit
	}
	offset := 0
	if options.Offset != nil {
		offset = *options.Offset
	}

	// Set row numbers on each record if the model has a RowNumber field
	h.setRowNumbersOnRecords(modelPtr, offset)

	metadata := &common.Metadata{
		Total:    int64(total),
		Count:    int64(resultCount),
		Filtered: int64(total),
		Limit:    limit,
		Offset:   offset,
	}

	// Fetch row number for a specific record if requested
	if options.FetchRowNumber != nil && *options.FetchRowNumber != "" {
		pkName := reflection.GetPrimaryKeyName(model)
		pkValue := *options.FetchRowNumber

		logger.Debug("Fetching row number for specific PK %s = %s", pkName, pkValue)

		rowNum, err := h.FetchRowNumber(ctx, tableName, pkName, pkValue, options, model)
		if err != nil {
			logger.Warn("Failed to fetch row number: %v", err)
			// Don't fail the entire request, just log the warning
		} else {
			metadata.RowNumber = &rowNum
			logger.Debug("Row number for PK %s: %d", pkValue, rowNum)
		}
	}

	// Execute AfterRead hooks
	hookCtx.Result = modelPtr
	hookCtx.Error = nil

	if err := h.hooks.Execute(AfterRead, hookCtx); err != nil {
		logger.Error("AfterRead hook failed: %v", err)
		h.sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
		return
	}

	h.sendFormattedResponse(w, modelPtr, metadata, options)
}

// applyPreloadWithRecursion applies a preload with support for ComputedQL and recursive preloading
func (h *Handler) applyPreloadWithRecursion(query common.SelectQuery, preload common.PreloadOption, allPreloads []common.PreloadOption, model interface{}, depth int) common.SelectQuery {
	// Log relationship keys if they're specified (from XFiles)
	if preload.RelatedKey != "" || preload.ForeignKey != "" || preload.PrimaryKey != "" {
		logger.Debug("Preload %s has relationship keys - PK: %s, RelatedKey: %s, ForeignKey: %s",
			preload.Relation, preload.PrimaryKey, preload.RelatedKey, preload.ForeignKey)

		// Build a WHERE clause using the relationship keys if needed
		// Note: Bun's PreloadRelation typically handles the relationship join automatically via struct tags
		// However, if the relationship keys are explicitly provided from XFiles, we can use them
		// to add additional filtering or validation
		if preload.RelatedKey != "" && preload.Where == "" {
			// For child tables: ensure the child's relatedkey column will be matched
			// The actual parent value is dynamic and handled by Bun's preload mechanism
			// We just log this for visibility
			logger.Debug("Child table %s will be filtered by %s matching parent's primary key",
				preload.Relation, preload.RelatedKey)
		}
		if preload.ForeignKey != "" && preload.Where == "" {
			// For parent tables: ensure the parent's primary key matches the current table's foreign key
			logger.Debug("Parent table %s will be filtered by primary key matching current table's %s",
				preload.Relation, preload.ForeignKey)
		}
	}

	// Apply the preload
	query = query.PreloadRelation(preload.Relation, func(sq common.SelectQuery) common.SelectQuery {
		// Get the related model for column operations
		relatedModel := reflection.GetRelationModel(model, preload.Relation)
		if relatedModel == nil {
			logger.Warn("Could not get related model for preload: %s", preload.Relation)
			// relatedModel = model // fallback to parent model
		} else {

			// If we have computed columns but no explicit columns, populate with all model columns first
			// since computed columns are additions
			if len(preload.Columns) == 0 && (len(preload.ComputedQL) > 0 || len(preload.OmitColumns) > 0) {
				logger.Debug("Populating preload columns with all model columns since computed columns are additions")
				preload.Columns = reflection.GetSQLModelColumns(relatedModel)
			}

			// Apply ComputedQL fields if any
			if len(preload.ComputedQL) > 0 {
				// Get the base table name from the related model
				baseTableName := getTableNameFromModel(relatedModel)

				// Convert the preload relation path to the appropriate alias format
				// This is ORM-specific. Currently we only support Bun's format.
				// TODO: Add support for other ORMs if needed
				preloadAlias := ""
				if h.db.GetUnderlyingDB() != nil {
					// Check if we're using Bun by checking the type name
					underlyingType := fmt.Sprintf("%T", h.db.GetUnderlyingDB())
					if strings.Contains(underlyingType, "bun.DB") {
						// Use Bun's alias format: lowercase with double underscores
						preloadAlias = relationPathToBunAlias(preload.Relation)
					}
					// For GORM: GORM doesn't use the same alias format, and this fix
					// may not be needed since GORM handles preloads differently
				}

				logger.Debug("Applying computed columns to preload %s (alias: %s, base table: %s)",
					preload.Relation, preloadAlias, baseTableName)

				for colName, colExpr := range preload.ComputedQL {
					// Replace table references in the expression with the preload alias
					// This fixes the ambiguous column reference issue when there are multiple
					// levels of recursive/nested preloads
					adjustedExpr := colExpr
					if baseTableName != "" && preloadAlias != "" {
						adjustedExpr = replaceTableReferencesInSQL(colExpr, baseTableName, preloadAlias)
						if adjustedExpr != colExpr {
							logger.Debug("Adjusted computed column expression for %s: '%s' -> '%s'",
								colName, colExpr, adjustedExpr)
						}
					}

					logger.Debug("Applying computed column to preload %s: %s", preload.Relation, colName)
					sq = sq.ColumnExpr(fmt.Sprintf("(%s) AS %s", adjustedExpr, colName))
					// Remove the computed column from selected columns to avoid duplication
					for colIndex := range preload.Columns {
						if preload.Columns[colIndex] == colName {
							preload.Columns = append(preload.Columns[:colIndex], preload.Columns[colIndex+1:]...)
							break
						}
					}
				}
			}

			// Handle OmitColumns
			if len(preload.OmitColumns) > 0 {
				allCols := preload.Columns
				// Remove omitted columns
				preload.Columns = []string{}
				for _, col := range allCols {
					addCols := true
					for _, omitCol := range preload.OmitColumns {
						if col == omitCol {
							addCols = false
							break
						}
					}
					if addCols {
						preload.Columns = append(preload.Columns, col)
					}
				}
			}

			// Apply column selection
			if len(preload.Columns) > 0 {
				sq = sq.Column(preload.Columns...)
			}
		}

		// Apply filters
		if len(preload.Filters) > 0 {
			for _, filter := range preload.Filters {
				sq = h.applyFilter(sq, filter, "", false, "AND")
			}
		}

		// Apply sorting
		if len(preload.Sort) > 0 {
			for _, sort := range preload.Sort {
				// Check if it's an expression (enclosed in brackets) - use directly without quoting
				if strings.HasPrefix(sort.Column, "(") && strings.HasSuffix(sort.Column, ")") {
					// For expressions, pass as raw SQL to prevent auto-quoting
					sq = sq.OrderExpr(fmt.Sprintf("%s %s", sort.Column, sort.Direction))
				} else {
					// Regular column - let ORM handle quoting
					sq = sq.Order(fmt.Sprintf("%s %s", sort.Column, sort.Direction))
				}
			}
		}

		// Apply WHERE clause
		if len(preload.Where) > 0 {
			// Build RequestOptions with all preloads to allow references to sibling relations
			preloadOpts := &common.RequestOptions{Preload: allPreloads}
			sanitizedWhere := common.SanitizeWhereClause(preload.Where, reflection.ExtractTableNameOnly(preload.Relation), preloadOpts)
			if len(sanitizedWhere) > 0 {
				sq = sq.Where(sanitizedWhere)
			}
		}

		// Apply limit
		if preload.Limit != nil && *preload.Limit > 0 {
			sq = sq.Limit(*preload.Limit)
		}

		if preload.Offset != nil && *preload.Offset > 0 {
			sq = sq.Offset(*preload.Offset)
		}

		return sq
	})

	// Handle recursive preloading
	if preload.Recursive && depth < 5 {
		logger.Debug("Applying recursive preload for %s at depth %d", preload.Relation, depth+1)

		// For recursive relationships, we need to get the last part of the relation path
		// e.g., "MastertaskItems" -> "MastertaskItems.MastertaskItems"
		relationParts := strings.Split(preload.Relation, ".")
		lastRelationName := relationParts[len(relationParts)-1]

		// Create a recursive preload with the same configuration
		// but with the relation path extended
		recursivePreload := preload
		recursivePreload.Relation = preload.Relation + "." + lastRelationName

		// Recursively apply preload until we reach depth 5
		query = h.applyPreloadWithRecursion(query, recursivePreload, allPreloads, model, depth+1)
	}

	return query
}

// relationPathToBunAlias converts a relation path like "MAL.MAL.DEF" to the Bun alias format "mal__mal__def"
// Bun generates aliases for nested relations by lowercasing and replacing dots with double underscores
func relationPathToBunAlias(relationPath string) string {
	if relationPath == "" {
		return ""
	}
	// Convert to lowercase and replace dots with double underscores
	alias := strings.ToLower(relationPath)
	alias = strings.ReplaceAll(alias, ".", "__")
	return alias
}

// replaceTableReferencesInSQL replaces references to a base table name in a SQL expression
// with the appropriate alias for the current preload level
// For example, if baseTableName is "mastertaskitem" and targetAlias is "mal__mal",
// it will replace "mastertaskitem.rid_mastertaskitem" with "mal__mal.rid_mastertaskitem"
func replaceTableReferencesInSQL(sqlExpr, baseTableName, targetAlias string) string {
	if sqlExpr == "" || baseTableName == "" || targetAlias == "" {
		return sqlExpr
	}

	// Replace both quoted and unquoted table references
	// Handle patterns like: tablename.column, "tablename".column, tablename."column", "tablename"."column"

	// Pattern 1: tablename.column (unquoted)
	result := strings.ReplaceAll(sqlExpr, baseTableName+".", targetAlias+".")

	// Pattern 2: "tablename".column or "tablename"."column" (quoted table name)
	result = strings.ReplaceAll(result, "\""+baseTableName+"\".", "\""+targetAlias+"\".")

	return result
}

// getTableNameFromModel extracts the table name from a model
// It checks the bun tag first, then falls back to converting the struct name to snake_case
func getTableNameFromModel(model interface{}) string {
	if model == nil {
		return ""
	}

	modelType := reflect.TypeOf(model)

	// Unwrap pointers
	for modelType != nil && modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return ""
	}

	// Look for bun tag on embedded BaseModel
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if field.Anonymous {
			bunTag := field.Tag.Get("bun")
			if strings.HasPrefix(bunTag, "table:") {
				return strings.TrimPrefix(bunTag, "table:")
			}
		}
	}

	// Fallback: convert struct name to lowercase (simple heuristic)
	// This handles cases like "MasterTaskItem" -> "mastertaskitem"
	return strings.ToLower(modelType.Name())
}

func (h *Handler) handleCreate(ctx context.Context, w common.ResponseWriter, data interface{}, options ExtendedRequestOptions) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "handleCreate", err)
		}
	}()

	schema := GetSchema(ctx)
	entity := GetEntity(ctx)
	tableName := GetTableName(ctx)
	model := GetModel(ctx)

	logger.Info("Creating record in %s.%s", schema, entity)

	// Execute BeforeCreate hooks
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		TableName: tableName,
		Model:     model,
		Options:   options,
		Data:      data,
		Writer:    w,
		Tx:        h.db,
	}

	if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
		logger.Error("BeforeCreate hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	// Use potentially modified data from hook context
	data = hookCtx.Data

	// Normalize data to slice for unified processing
	dataSlice := h.normalizeToSlice(data)
	logger.Debug("Processing %d item(s) for creation", len(dataSlice))

	// Store original data maps for merging later
	originalDataMaps := make([]map[string]interface{}, 0, len(dataSlice))

	// Process all items in a transaction
	results := make([]interface{}, 0, len(dataSlice))
	err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
		// Create temporary nested processor with transaction
		txNestedProcessor := common.NewNestedCUDProcessor(tx, h.registry, h)

		for i, item := range dataSlice {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				// Convert to map if needed
				jsonData, err := json.Marshal(item)
				if err != nil {
					return fmt.Errorf("failed to marshal item %d: %w", i, err)
				}
				itemMap = make(map[string]interface{})
				if err := json.Unmarshal(jsonData, &itemMap); err != nil {
					return fmt.Errorf("failed to unmarshal item %d: %w", i, err)
				}
			}

			// Store a copy of the original data map for merging later
			originalMap := make(map[string]interface{})
			for k, v := range itemMap {
				originalMap[k] = v
			}
			originalDataMaps = append(originalDataMaps, originalMap)

			// Extract nested relations if present (but don't process them yet)
			var nestedRelations map[string]interface{}
			if h.shouldUseNestedProcessor(itemMap, model) {
				logger.Debug("Extracting nested relations for item %d", i)
				cleanedData, relations, err := h.extractNestedRelations(itemMap, model)
				if err != nil {
					return fmt.Errorf("failed to extract nested relations for item %d: %w", i, err)
				}
				itemMap = cleanedData
				nestedRelations = relations
			}

			// Convert item to model type - create a pointer to the model
			modelValue := reflect.New(reflect.TypeOf(model)).Interface()
			jsonData, err := json.Marshal(itemMap)
			if err != nil {
				return fmt.Errorf("failed to marshal item %d: %w", i, err)
			}
			if err := json.Unmarshal(jsonData, modelValue); err != nil {
				return fmt.Errorf("failed to unmarshal item %d: %w", i, err)
			}

			// Create insert query
			query := tx.NewInsert().Model(modelValue)

			// Only set Table() if the model doesn't provide a table name via TableNameProvider
			if provider, ok := modelValue.(common.TableNameProvider); !ok || provider.TableName() == "" {
				query = query.Table(tableName)
			}

			query = query.Returning("*")

			// Execute BeforeScan hooks - pass query chain so hooks can modify it
			itemHookCtx := &HookContext{
				Context:   ctx,
				Handler:   h,
				Schema:    schema,
				Entity:    entity,
				TableName: tableName,
				Model:     model,
				Options:   options,
				Data:      modelValue,
				Writer:    w,
				Query:     query,
				Tx:        tx,
			}
			if err := h.hooks.Execute(BeforeScan, itemHookCtx); err != nil {
				return fmt.Errorf("BeforeScan hook failed for item %d: %w", i, err)
			}

			// Use potentially modified query from hook context
			if modifiedQuery, ok := itemHookCtx.Query.(common.InsertQuery); ok {
				query = modifiedQuery
			}

			// Execute insert and get the ID
			if _, err := query.Exec(ctx); err != nil {
				return fmt.Errorf("failed to insert item %d: %w", i, err)
			}

			// Get the inserted ID
			insertedID := reflection.GetPrimaryKeyValue(modelValue)

			// Now process nested relations with the parent ID
			if len(nestedRelations) > 0 {
				logger.Debug("Processing nested relations for item %d with parent ID: %v", i, insertedID)
				if err := h.processChildRelationsWithParentID(ctx, txNestedProcessor, "insert", nestedRelations, model, insertedID); err != nil {
					return fmt.Errorf("failed to process nested relations for item %d: %w", i, err)
				}
			}

			results = append(results, modelValue)
		}
		return nil
	})

	if err != nil {
		logger.Error("Error creating records: %v", err)
		h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating records", err)
		return
	}

	// Merge created records with original request data
	// This preserves extra keys from the request
	mergedResults := make([]interface{}, 0, len(results))
	for i, result := range results {
		if i < len(originalDataMaps) {
			merged := h.mergeRecordWithRequest(result, originalDataMaps[i])
			mergedResults = append(mergedResults, merged)
		} else {
			mergedResults = append(mergedResults, result)
		}
	}

	// Execute AfterCreate hooks
	var responseData interface{}
	if len(mergedResults) == 1 {
		responseData = mergedResults[0]
		hookCtx.Result = mergedResults[0]
	} else {
		responseData = mergedResults
		hookCtx.Result = map[string]interface{}{"created": len(mergedResults), "data": mergedResults}
	}
	hookCtx.Error = nil

	if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
		logger.Error("AfterCreate hook failed: %v", err)
		h.sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
		return
	}

	logger.Info("Successfully created %d record(s)", len(mergedResults))
	// Invalidate cache for this table
	cacheTags := buildCacheTags(schema, tableName)
	if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
		logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
	}
	h.sendResponseWithOptions(w, responseData, nil, &options)
}

func (h *Handler) handleUpdate(ctx context.Context, w common.ResponseWriter, id string, idPtr *int64, data interface{}, options ExtendedRequestOptions) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "handleUpdate", err)
		}
	}()

	schema := GetSchema(ctx)
	entity := GetEntity(ctx)
	tableName := GetTableName(ctx)
	model := GetModel(ctx)

	logger.Info("Updating record in %s.%s", schema, entity)

	// Execute BeforeUpdate hooks
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		TableName: tableName,
		Tx:        h.db,
		Model:     model,
		Options:   options,
		ID:        id,
		Data:      data,
		Writer:    w,
	}

	if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
		logger.Error("BeforeUpdate hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	// Use potentially modified data from hook context
	data = hookCtx.Data

	// Convert data to map
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		jsonData, err := json.Marshal(data)
		if err != nil {
			logger.Error("Error marshaling data: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_data", "Invalid data format", err)
			return
		}
		if err := json.Unmarshal(jsonData, &dataMap); err != nil {
			logger.Error("Error unmarshaling data: %v", err)
			h.sendError(w, http.StatusBadRequest, "invalid_data", "Invalid data format", err)
			return
		}
	}

	// Determine target ID
	var targetID interface{}
	if id != "" {
		targetID = id
	} else if idPtr != nil {
		targetID = *idPtr
	} else {
		h.sendError(w, http.StatusBadRequest, "missing_id", "ID is required for update", nil)
		return
	}

	// Get the primary key name for the model
	pkName := reflection.GetPrimaryKeyName(model)

	// Variable to store the updated record
	var updatedRecord interface{}

	// Process nested relations if present
	err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
		// Create temporary nested processor with transaction
		txNestedProcessor := common.NewNestedCUDProcessor(tx, h.registry, h)

		// Extract nested relations if present (but don't process them yet)
		var nestedRelations map[string]interface{}
		if h.shouldUseNestedProcessor(dataMap, model) {
			logger.Debug("Extracting nested relations for update")
			cleanedData, relations, err := h.extractNestedRelations(dataMap, model)
			if err != nil {
				return fmt.Errorf("failed to extract nested relations: %w", err)
			}
			dataMap = cleanedData
			nestedRelations = relations
		}

		// Ensure ID is in the data map for the update
		dataMap[pkName] = targetID

		// Populate model instance from dataMap to preserve custom types (like SqlJSONB)
		// Get the type of the model, handling both pointer and non-pointer types
		modelType := reflect.TypeOf(model)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}
		modelInstance := reflect.New(modelType).Interface()
		if err := reflection.MapToStruct(dataMap, modelInstance); err != nil {
			return fmt.Errorf("failed to populate model from data: %w", err)
		}

		// Create update query using Model() to preserve custom types and driver.Valuer interfaces
		query := tx.NewUpdate().Model(modelInstance)
		query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), targetID)

		// Execute BeforeScan hooks - pass query chain so hooks can modify it
		hookCtx.Query = query
		hookCtx.Tx = tx
		if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
			return fmt.Errorf("BeforeScan hook failed: %w", err)
		}

		// Use potentially modified query from hook context
		if modifiedQuery, ok := hookCtx.Query.(common.UpdateQuery); ok {
			query = modifiedQuery
		}

		// Execute update
		result, err := query.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update record: %w", err)
		}

		// Now process nested relations with the parent ID
		if len(nestedRelations) > 0 {
			logger.Debug("Processing nested relations for update with parent ID: %v", targetID)
			if err := h.processChildRelationsWithParentID(ctx, txNestedProcessor, "update", nestedRelations, model, targetID); err != nil {
				return fmt.Errorf("failed to process nested relations: %w", err)
			}
		}

		// Fetch the updated record to return the new values
		modelValue := reflect.New(reflect.TypeOf(model)).Interface()
		selectQuery := tx.NewSelect().Model(modelValue).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), targetID)
		if err := selectQuery.ScanModel(ctx); err != nil {
			return fmt.Errorf("failed to fetch updated record: %w", err)
		}

		updatedRecord = modelValue

		// Store result for hooks
		hookCtx.Result = updatedRecord
		_ = result // Keep result variable for potential future use
		return nil
	})

	if err != nil {
		logger.Error("Error updating record: %v", err)
		h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating record", err)
		return
	}

	// Merge the updated record with the original request data
	// This preserves extra keys from the request and updates values from the database
	mergedData := h.mergeRecordWithRequest(updatedRecord, dataMap)

	// Execute AfterUpdate hooks
	hookCtx.Result = mergedData
	hookCtx.Error = nil
	if err := h.hooks.Execute(AfterUpdate, hookCtx); err != nil {
		logger.Error("AfterUpdate hook failed: %v", err)
		h.sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
		return
	}

	logger.Info("Successfully updated record with ID: %v", targetID)
	// Invalidate cache for this table
	cacheTags := buildCacheTags(schema, tableName)
	if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
		logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
	}
	h.sendResponseWithOptions(w, mergedData, nil, &options)
}

func (h *Handler) handleDelete(ctx context.Context, w common.ResponseWriter, id string, data interface{}) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "handleDelete", err)
		}
	}()

	schema := GetSchema(ctx)
	entity := GetEntity(ctx)
	tableName := GetTableName(ctx)
	model := GetModel(ctx)

	logger.Info("Deleting record(s) from %s.%s", schema, entity)

	// Handle batch delete from request data
	if data != nil {
		switch v := data.(type) {
		case []string:
			// Array of IDs as strings
			logger.Info("Batch delete with %d IDs ([]string)", len(v))
			deletedCount := 0
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, itemID := range v {
					// Execute hooks for each item
					hookCtx := &HookContext{
						Context:   ctx,
						Handler:   h,
						Schema:    schema,
						Entity:    entity,
						TableName: tableName,
						Model:     model,
						ID:        itemID,
						Writer:    w,
						Tx:        tx,
					}

					if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
						logger.Warn("BeforeDelete hook failed for ID %s: %v", itemID, err)
						continue
					}

					query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)

					result, err := query.Exec(ctx)
					if err != nil {
						return fmt.Errorf("failed to delete record %s: %w", itemID, err)
					}
					deletedCount += int(result.RowsAffected())

					// Execute AfterDelete hook
					hookCtx.Result = map[string]interface{}{"deleted": result.RowsAffected()}
					hookCtx.Error = nil
					if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
						logger.Warn("AfterDelete hook failed for ID %s: %v", itemID, err)
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error in batch delete: %v", err)
				h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting records", err)
				return
			}
			logger.Info("Successfully deleted %d records", deletedCount)
			// Invalidate cache for this table
			cacheTags := buildCacheTags(schema, tableName)
			if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
				logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
			}
			h.sendResponse(w, map[string]interface{}{"deleted": deletedCount}, nil)
			return

		case []interface{}:
			// Array of IDs or objects with ID field
			logger.Info("Batch delete with %d items ([]interface{})", len(v))
			deletedCount := 0
			pkName := reflection.GetPrimaryKeyName(model)
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, item := range v {
					var itemID interface{}

					// Check if item is a string ID or object with id field
					switch v := item.(type) {
					case string:
						itemID = v
					case map[string]interface{}:
						itemID = v[pkName]
					default:
						itemID = item
					}

					if itemID == nil {
						continue
					}

					itemIDStr := fmt.Sprintf("%v", itemID)

					// Execute hooks for each item
					hookCtx := &HookContext{
						Context:   ctx,
						Handler:   h,
						Schema:    schema,
						Entity:    entity,
						TableName: tableName,
						Model:     model,
						ID:        itemIDStr,
						Writer:    w,
						Tx:        tx,
					}

					if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
						logger.Warn("BeforeDelete hook failed for ID %v: %v", itemID, err)
						continue
					}

					query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
					result, err := query.Exec(ctx)
					if err != nil {
						return fmt.Errorf("failed to delete record %v: %w", itemID, err)
					}
					deletedCount += int(result.RowsAffected())

					// Execute AfterDelete hook
					hookCtx.Result = map[string]interface{}{"deleted": result.RowsAffected()}
					hookCtx.Error = nil
					if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
						logger.Warn("AfterDelete hook failed for ID %v: %v", itemID, err)
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error in batch delete: %v", err)
				h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting records", err)
				return
			}
			logger.Info("Successfully deleted %d records", deletedCount)
			// Invalidate cache for this table
			cacheTags := buildCacheTags(schema, tableName)
			if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
				logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
			}
			h.sendResponse(w, map[string]interface{}{"deleted": deletedCount}, nil)
			return

		case []map[string]interface{}:
			// Array of objects with id field
			logger.Info("Batch delete with %d items ([]map[string]interface{})", len(v))
			deletedCount := 0
			pkName := reflection.GetPrimaryKeyName(model)
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, item := range v {
					if itemID, ok := item[pkName]; ok && itemID != nil {
						itemIDStr := fmt.Sprintf("%v", itemID)

						// Execute hooks for each item
						hookCtx := &HookContext{
							Context:   ctx,
							Handler:   h,
							Schema:    schema,
							Entity:    entity,
							TableName: tableName,
							Model:     model,
							ID:        itemIDStr,
							Writer:    w,
							Tx:        tx,
						}

						if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
							logger.Warn("BeforeDelete hook failed for ID %v: %v", itemID, err)
							continue
						}

						query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
						result, err := query.Exec(ctx)
						if err != nil {
							return fmt.Errorf("failed to delete record %v: %w", itemID, err)
						}
						deletedCount += int(result.RowsAffected())

						// Execute AfterDelete hook
						hookCtx.Result = map[string]interface{}{"deleted": result.RowsAffected()}
						hookCtx.Error = nil
						if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
							logger.Warn("AfterDelete hook failed for ID %v: %v", itemID, err)
						}
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error in batch delete: %v", err)
				h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting records", err)
				return
			}
			logger.Info("Successfully deleted %d records", deletedCount)
			// Invalidate cache for this table
			cacheTags := buildCacheTags(schema, tableName)
			if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
				logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
			}
			h.sendResponse(w, map[string]interface{}{"deleted": deletedCount}, nil)
			return

		case map[string]interface{}:
			// Single object with id field
			pkName := reflection.GetPrimaryKeyName(model)
			if itemID, ok := v[pkName]; ok && itemID != nil {
				id = fmt.Sprintf("%v", itemID)
			}
		}
	}

	// Single delete with URL ID
	if id == "" {
		h.sendError(w, http.StatusBadRequest, "missing_id", "ID is required for delete", nil)
		return
	}

	// Get primary key name
	pkName := reflection.GetPrimaryKeyName(model)

	// First, fetch the record that will be deleted
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	recordToDelete := reflect.New(modelType).Interface()

	selectQuery := h.db.NewSelect().Model(recordToDelete).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)
	if err := selectQuery.ScanModel(ctx); err != nil {
		if err == sql.ErrNoRows {
			logger.Warn("Record not found for delete: %s = %s", pkName, id)
			h.sendError(w, http.StatusNotFound, "not_found", "Record not found", err)
			return
		}
		logger.Error("Error fetching record for delete: %v", err)
		h.sendError(w, http.StatusInternalServerError, "fetch_error", "Error fetching record", err)
		return
	}

	// Execute BeforeDelete hooks with the record data
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    schema,
		Entity:    entity,
		TableName: tableName,
		Model:     model,
		ID:        id,
		Writer:    w,
		Tx:        h.db,
		Data:      recordToDelete,
	}

	if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
		logger.Error("BeforeDelete hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	query := h.db.NewDelete().Table(tableName)
	query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(pkName)), id)

	// Execute BeforeScan hooks - pass query chain so hooks can modify it
	hookCtx.Query = query
	if err := h.hooks.Execute(BeforeScan, hookCtx); err != nil {
		logger.Error("BeforeScan hook failed: %v", err)
		h.sendError(w, http.StatusBadRequest, "hook_error", "Hook execution failed", err)
		return
	}

	// Use potentially modified query from hook context
	if modifiedQuery, ok := hookCtx.Query.(common.DeleteQuery); ok {
		query = modifiedQuery
	}

	result, err := query.Exec(ctx)
	if err != nil {
		logger.Error("Error deleting record: %v", err)
		h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting record", err)
		return
	}

	// Check if the record was actually deleted
	if result.RowsAffected() == 0 {
		logger.Warn("No rows deleted for ID: %s", id)
		h.sendError(w, http.StatusNotFound, "not_found", "Record not found or already deleted", nil)
		return
	}

	// Execute AfterDelete hooks with the deleted record data
	hookCtx.Result = recordToDelete
	hookCtx.Error = nil

	if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
		logger.Error("AfterDelete hook failed: %v", err)
		h.sendError(w, http.StatusInternalServerError, "hook_error", "Hook execution failed", err)
		return
	}

	// Return the deleted record data
	// Invalidate cache for this table
	cacheTags := buildCacheTags(schema, tableName)
	if err := invalidateCacheForTags(ctx, cacheTags); err != nil {
		logger.Warn("Failed to invalidate cache for table %s: %v", tableName, err)
	}
	h.sendResponse(w, recordToDelete, nil)
}

// mergeRecordWithRequest merges a database record with the original request data
// This preserves extra keys from the request that aren't in the database model
// and updates values from the database (e.g., from SQL triggers or defaults)
func (h *Handler) mergeRecordWithRequest(dbRecord interface{}, requestData map[string]interface{}) map[string]interface{} {
	// Convert the database record to a map
	dbMap := make(map[string]interface{})

	// Marshal and unmarshal to convert struct to map
	jsonData, err := json.Marshal(dbRecord)
	if err != nil {
		logger.Warn("Failed to marshal database record for merging: %v", err)
		return requestData
	}

	if err := json.Unmarshal(jsonData, &dbMap); err != nil {
		logger.Warn("Failed to unmarshal database record for merging: %v", err)
		return requestData
	}

	// Start with the request data (preserves extra keys)
	result := make(map[string]interface{})
	for k, v := range requestData {
		result[k] = v
	}

	// Update with values from database (overwrites with DB values, including trigger changes)
	for k, v := range dbMap {
		result[k] = v
	}

	return result
}

// normalizeToSlice converts data to a slice. Single items become a 1-item slice.
func (h *Handler) normalizeToSlice(data interface{}) []interface{} {
	if data == nil {
		return []interface{}{}
	}

	dataValue := reflect.ValueOf(data)
	if dataValue.Kind() == reflect.Slice || dataValue.Kind() == reflect.Array {
		result := make([]interface{}, dataValue.Len())
		for i := 0; i < dataValue.Len(); i++ {
			result[i] = dataValue.Index(i).Interface()
		}
		return result
	}

	// Single item - return as 1-item slice
	return []interface{}{data}
}

// extractNestedRelations extracts nested relations from data, returning cleaned data and relations
// This does NOT process the relations, just separates them for later processing
func (h *Handler) extractNestedRelations(
	data map[string]interface{},
	model interface{},
) (_cleanedData map[string]interface{}, _relations map[string]interface{}, _err error) {
	// Get model type for reflection
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return data, nil, fmt.Errorf("model must be a struct type, got %v", modelType)
	}

	// Separate relation fields from regular fields
	cleanedData := make(map[string]interface{})
	relations := make(map[string]interface{})

	for key, value := range data {
		// Skip _request field
		if key == "_request" {
			continue
		}

		// Check if this field is a relation
		relInfo := h.GetRelationshipInfo(modelType, key)
		if relInfo != nil {
			logger.Debug("Found nested relation field: %s (type: %s)", key, relInfo.RelationType)
			relations[key] = value
		} else {
			cleanedData[key] = value
		}
	}

	return cleanedData, relations, nil
}

// processChildRelationsWithParentID processes nested relations with a parent ID
func (h *Handler) processChildRelationsWithParentID(
	ctx context.Context,
	processor *common.NestedCUDProcessor,
	operation string,
	relations map[string]interface{},
	parentModel interface{},
	parentID interface{},
) error {
	// Get model type for reflection
	modelType := reflect.TypeOf(parentModel)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return fmt.Errorf("model must be a struct type, got %v", modelType)
	}

	// Process each relation
	for relationName, relationValue := range relations {
		if relationValue == nil {
			continue
		}

		// Get relationship info
		relInfo := h.GetRelationshipInfo(modelType, relationName)
		if relInfo == nil {
			logger.Warn("No relationship info found for %s, skipping", relationName)
			continue
		}

		// Process this relation with parent ID
		if err := h.processChildRelationsForField(ctx, processor, operation, relationName, relationValue, relInfo, modelType, parentID); err != nil {
			return fmt.Errorf("failed to process relation %s: %w", relationName, err)
		}
	}

	return nil
}

// processChildRelationsForField processes a single nested relation field
func (h *Handler) processChildRelationsForField(
	ctx context.Context,
	processor *common.NestedCUDProcessor,
	operation string,
	relationName string,
	relationValue interface{},
	relInfo *common.RelationshipInfo,
	parentModelType reflect.Type,
	parentID interface{},
) error {
	if relationValue == nil {
		return nil
	}

	// Get the related model
	field, found := parentModelType.FieldByName(relInfo.FieldName)
	if !found {
		return fmt.Errorf("field %s not found in model", relInfo.FieldName)
	}

	// Get the model type for the relation
	relatedModelType := field.Type
	if relatedModelType.Kind() == reflect.Slice {
		relatedModelType = relatedModelType.Elem()
	}
	if relatedModelType.Kind() == reflect.Ptr {
		relatedModelType = relatedModelType.Elem()
	}

	// Create an instance of the related model
	relatedModel := reflect.New(relatedModelType).Elem().Interface()

	// Get table name for related model
	relatedTableName := h.getTableNameForRelatedModel(relatedModel, relInfo.JSONName)

	// Prepare parent IDs for foreign key injection
	parentIDs := make(map[string]interface{})
	if relInfo.ForeignKey != "" && parentID != nil {
		baseName := strings.TrimSuffix(relInfo.ForeignKey, "ID")
		baseName = strings.TrimSuffix(strings.ToLower(baseName), "_id")
		parentIDs[baseName] = parentID
	}

	// Process based on relation type and data structure
	switch v := relationValue.(type) {
	case map[string]interface{}:
		// Single related object
		_, err := processor.ProcessNestedCUD(ctx, operation, v, relatedModel, parentIDs, relatedTableName)
		if err != nil {
			return fmt.Errorf("failed to process single relation: %w", err)
		}

	case []interface{}:
		// Multiple related objects
		for i, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				_, err := processor.ProcessNestedCUD(ctx, operation, itemMap, relatedModel, parentIDs, relatedTableName)
				if err != nil {
					return fmt.Errorf("failed to process relation item %d: %w", i, err)
				}
			}
		}

	case []map[string]interface{}:
		// Multiple related objects (typed slice)
		for i, itemMap := range v {
			_, err := processor.ProcessNestedCUD(ctx, operation, itemMap, relatedModel, parentIDs, relatedTableName)
			if err != nil {
				return fmt.Errorf("failed to process relation item %d: %w", i, err)
			}
		}

	default:
		return fmt.Errorf("unsupported relation data type: %T", relationValue)
	}

	return nil
}

// getTableNameForRelatedModel gets the table name for a related model
func (h *Handler) getTableNameForRelatedModel(model interface{}, defaultName string) string {
	if provider, ok := model.(common.TableNameProvider); ok {
		tableName := provider.TableName()
		if tableName != "" {
			return tableName
		}
	}
	return defaultName
}

// qualifyColumnName ensures column name is fully qualified with table name if not already
func (h *Handler) qualifyColumnName(columnName, fullTableName string) string {
	// Check if column already has a table/schema prefix (contains a dot)
	if strings.Contains(columnName, ".") {
		return columnName
	}

	// If no table name provided, return column as-is
	if fullTableName == "" {
		return columnName
	}

	// Extract just the table name from "schema.table" format
	// Only use the table name part, not the schema
	tableOnly := fullTableName
	if idx := strings.LastIndex(fullTableName, "."); idx != -1 {
		tableOnly = fullTableName[idx+1:]
	}

	// Return column qualified with just the table name
	return fmt.Sprintf("%s.%s", tableOnly, columnName)
}

func (h *Handler) applyFilter(query common.SelectQuery, filter common.FilterOption, tableName string, needsCast bool, logicOp string) common.SelectQuery {
	// Qualify the column name with table name if not already qualified
	qualifiedColumn := h.qualifyColumnName(filter.Column, tableName)

	// Apply casting to text if needed for non-numeric columns or non-numeric values
	if needsCast {
		qualifiedColumn = fmt.Sprintf("CAST(%s AS TEXT)", qualifiedColumn)
	}

	// Helper function to apply the correct Where method based on logic operator
	applyWhere := func(condition string, args ...interface{}) common.SelectQuery {
		if logicOp == "OR" {
			return query.WhereOr(condition, args...)
		}
		return query.Where(condition, args...)
	}

	switch strings.ToLower(filter.Operator) {
	case "eq", "equals":
		return applyWhere(fmt.Sprintf("%s = ?", qualifiedColumn), filter.Value)
	case "neq", "not_equals", "ne":
		return applyWhere(fmt.Sprintf("%s != ?", qualifiedColumn), filter.Value)
	case "gt", "greater_than":
		return applyWhere(fmt.Sprintf("%s > ?", qualifiedColumn), filter.Value)
	case "gte", "greater_than_equals", "ge":
		return applyWhere(fmt.Sprintf("%s >= ?", qualifiedColumn), filter.Value)
	case "lt", "less_than":
		return applyWhere(fmt.Sprintf("%s < ?", qualifiedColumn), filter.Value)
	case "lte", "less_than_equals", "le":
		return applyWhere(fmt.Sprintf("%s <= ?", qualifiedColumn), filter.Value)
	case "like":
		return applyWhere(fmt.Sprintf("%s LIKE ?", qualifiedColumn), filter.Value)
	case "ilike":
		// Use ILIKE for case-insensitive search (PostgreSQL)
		// Column is already cast to TEXT if needed
		return applyWhere(fmt.Sprintf("%s ILIKE ?", qualifiedColumn), filter.Value)
	case "in":
		return applyWhere(fmt.Sprintf("%s IN (?)", qualifiedColumn), filter.Value)
	case "between":
		// Handle between operator - exclusive (> val1 AND < val2)
		if values, ok := filter.Value.([]interface{}); ok && len(values) == 2 {
			return applyWhere(fmt.Sprintf("%s > ? AND %s < ?", qualifiedColumn, qualifiedColumn), values[0], values[1])
		} else if values, ok := filter.Value.([]string); ok && len(values) == 2 {
			return applyWhere(fmt.Sprintf("%s > ? AND %s < ?", qualifiedColumn, qualifiedColumn), values[0], values[1])
		}
		logger.Warn("Invalid BETWEEN filter value format")
		return query
	case "between_inclusive":
		// Handle between inclusive operator - inclusive (>= val1 AND <= val2)
		if values, ok := filter.Value.([]interface{}); ok && len(values) == 2 {
			return applyWhere(fmt.Sprintf("%s >= ? AND %s <= ?", qualifiedColumn, qualifiedColumn), values[0], values[1])
		} else if values, ok := filter.Value.([]string); ok && len(values) == 2 {
			return applyWhere(fmt.Sprintf("%s >= ? AND %s <= ?", qualifiedColumn, qualifiedColumn), values[0], values[1])
		}
		logger.Warn("Invalid BETWEEN INCLUSIVE filter value format")
		return query
	case "is_null", "isnull":
		// Check for NULL values - don't use cast for NULL checks
		colName := h.qualifyColumnName(filter.Column, tableName)
		return applyWhere(fmt.Sprintf("(%s IS NULL OR %s = '')", colName, colName))
	case "is_not_null", "isnotnull":
		// Check for NOT NULL values - don't use cast for NULL checks
		colName := h.qualifyColumnName(filter.Column, tableName)
		return applyWhere(fmt.Sprintf("(%s IS NOT NULL AND %s != '')", colName, colName))
	default:
		logger.Warn("Unknown filter operator: %s, defaulting to equals", filter.Operator)
		return applyWhere(fmt.Sprintf("%s = ?", qualifiedColumn), filter.Value)
	}
}

// parseTableName splits a table name that may contain schema into separate schema and table
func (h *Handler) parseTableName(fullTableName string) (schema, table string) {
	if idx := strings.LastIndex(fullTableName, "."); idx != -1 {
		return fullTableName[:idx], fullTableName[idx+1:]
	}
	return "", fullTableName
}

// getSchemaAndTable returns the schema and table name separately
// It checks SchemaProvider and TableNameProvider interfaces and handles cases where
// the table name may already include the schema (e.g., "public.users")
//
// Priority order:
// 1. If TableName() contains a schema (e.g., "myschema.mytable"), that schema takes precedence
// 2. If model implements SchemaProvider, use that schema
// 3. Otherwise, use the defaultSchema parameter
func (h *Handler) getSchemaAndTable(defaultSchema, entity string, model interface{}) (schema, table string) {
	// First check if model provides a table name
	// We check this FIRST because the table name might already contain the schema
	if tableProvider, ok := model.(common.TableNameProvider); ok {
		tableName := tableProvider.TableName()

		// IMPORTANT: Check if the table name already contains a schema (e.g., "schema.table")
		// This is common when models need to specify a different schema than the default
		if tableSchema, tableOnly := h.parseTableName(tableName); tableSchema != "" {
			// Table name includes schema - use it and ignore any other schema providers
			logger.Debug("TableName() includes schema: %s.%s", tableSchema, tableOnly)
			return tableSchema, tableOnly
		}

		// Table name is just the table name without schema
		// Now determine which schema to use
		if schemaProvider, ok := model.(common.SchemaProvider); ok {
			schema = schemaProvider.SchemaName()
		} else {
			schema = defaultSchema
		}

		return schema, tableName
	}

	// No TableNameProvider, so check for schema and use entity as table name
	if schemaProvider, ok := model.(common.SchemaProvider); ok {
		schema = schemaProvider.SchemaName()
	} else {
		schema = defaultSchema
	}

	// Default to entity name as table
	return schema, entity
}

// getTableName returns the full table name including schema (schema.table)
func (h *Handler) getTableName(schema, entity string, model interface{}) string {
	schemaName, tableName := h.getSchemaAndTable(schema, entity, model)
	if schemaName != "" {
		return fmt.Sprintf("%s.%s", schemaName, tableName)
	}
	return tableName
}

func (h *Handler) generateMetadata(schema, entity string, model interface{}) *common.TableMetadata {
	modelType := reflect.TypeOf(model)

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType.Kind() != reflect.Struct {
		logger.Error("Model type must be a struct, got %s for %s.%s", modelType.Kind(), schema, entity)
		return &common.TableMetadata{
			Schema:    schema,
			Table:     h.getTableName(schema, entity, model),
			Columns:   []common.Column{},
			Relations: []string{},
		}
	}

	tableName := h.getTableName(schema, entity, model)

	metadata := &common.TableMetadata{
		Schema:    schema,
		Table:     tableName,
		Columns:   []common.Column{},
		Relations: []string{},
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		gormTag := field.Tag.Get("gorm")
		jsonTag := field.Tag.Get("json")

		// Skip fields with json:"-"
		if jsonTag == "-" {
			continue
		}

		// Get JSON name
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}

		// Check if this is a relation field (slice or struct, but not time.Time)
		if field.Type.Kind() == reflect.Slice ||
			(field.Type.Kind() == reflect.Struct && field.Type.Name() != "Time") ||
			(field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct && field.Type.Elem().Name() != "Time") {
			metadata.Relations = append(metadata.Relations, jsonName)
			continue
		}

		// Get column name from gorm tag or json tag
		columnName := field.Tag.Get("gorm")
		if strings.Contains(columnName, "column:") {
			parts := strings.Split(columnName, ";")
			for _, part := range parts {
				if strings.HasPrefix(part, "column:") {
					columnName = strings.TrimPrefix(part, "column:")
					break
				}
			}
		} else {
			columnName = jsonName
		}

		column := common.Column{
			Name:       columnName,
			Type:       h.getColumnType(field.Type),
			IsNullable: h.isNullable(field),
			IsPrimary:  strings.Contains(gormTag, "primaryKey") || strings.Contains(gormTag, "primary_key"),
			IsUnique:   strings.Contains(gormTag, "unique"),
			HasIndex:   strings.Contains(gormTag, "index"),
		}

		metadata.Columns = append(metadata.Columns, column)
	}

	return metadata
}

func (h *Handler) getColumnType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Bool:
		return "boolean"
	case reflect.Ptr:
		return h.getColumnType(t.Elem())
	default:
		return "unknown"
	}
}

func (h *Handler) isNullable(field reflect.StructField) bool {
	return field.Type.Kind() == reflect.Ptr
}

func (h *Handler) sendResponse(w common.ResponseWriter, data interface{}, metadata *common.Metadata) {
	h.sendResponseWithOptions(w, data, metadata, nil)
}

// sendResponseWithOptions sends a response with optional formatting
func (h *Handler) sendResponseWithOptions(w common.ResponseWriter, data interface{}, metadata *common.Metadata, options *ExtendedRequestOptions) {
	w.SetHeader("Content-Type", "application/json")
	if data == nil {
		data = map[string]interface{}{}
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	// Normalize single-record arrays to objects if requested
	if options != nil && options.SingleRecordAsObject {
		data = h.normalizeResultArray(data)
	}

	// Return data as-is without wrapping in common.Response

	if err := w.WriteJSON(data); err != nil {
		logger.Error("Failed to write JSON response: %v", err)
	}
}

// normalizeResultArray converts a single-element array to an object if requested
// Returns the single element if data is a slice/array with exactly one element, otherwise returns data unchanged
func (h *Handler) normalizeResultArray(data interface{}) interface{} {
	if data == nil {
		return map[string]interface{}{}
	}

	// Use reflection to check if data is a slice or array
	dataValue := reflect.ValueOf(data)
	if dataValue.Kind() == reflect.Ptr {
		dataValue = dataValue.Elem()
	}

	// Check if it's a slice or array
	if dataValue.Kind() == reflect.Slice || dataValue.Kind() == reflect.Array {
		if dataValue.Len() == 1 {
			// Return the single element
			return dataValue.Index(0).Interface()
		} else if dataValue.Len() == 0 {
			// Return empty object instead of empty array
			return map[string]interface{}{}
		}
	}

	if dataValue.Kind() == reflect.String {
		str := dataValue.String()
		if str == "" || str == "null" {
			return map[string]interface{}{}
		}

	}
	return data
}

// sendFormattedResponse sends response with formatting options
func (h *Handler) sendFormattedResponse(w common.ResponseWriter, data interface{}, metadata *common.Metadata, options ExtendedRequestOptions) {
	// Normalize single-record arrays to objects if requested
	httpStatus := http.StatusOK
	if data == nil {
		data = map[string]interface{}{}
		httpStatus = http.StatusPartialContent
	} else {
		dataLen := reflection.Len(data)
		if dataLen == 0 {
			httpStatus = http.StatusPartialContent
		}
	}

	if options.SingleRecordAsObject {
		data = h.normalizeResultArray(data)
	}

	// Clean JSON if requested (remove null/empty fields)
	if options.CleanJSON {
		data = h.cleanJSON(data)
	}

	w.SetHeader("Content-Type", "application/json")
	w.SetHeader("Content-Range", fmt.Sprintf("%d-%d/%d", metadata.Offset, int64(metadata.Offset)+metadata.Count, metadata.Filtered))
	w.SetHeader("X-Api-Range-Total", fmt.Sprintf("%d", metadata.Filtered))
	w.SetHeader("X-Api-Range-Size", fmt.Sprintf("%d", metadata.Count))

	// Format response based on response format option
	switch options.ResponseFormat {
	case "simple":
		// Simple format: just return the data array
		w.WriteHeader(httpStatus)
		if err := w.WriteJSON(data); err != nil {
			logger.Error("Failed to write JSON response: %v", err)
		}
	case "syncfusion":
		// Syncfusion format: { result: data, count: total }
		response := map[string]interface{}{
			"result": data,
		}
		if metadata != nil {
			response["count"] = metadata.Total
		}
		w.WriteHeader(httpStatus)
		if err := w.WriteJSON(response); err != nil {
			logger.Error("Failed to write JSON response: %v", err)
		}
	default:
		// Default/detail format: standard response with metadata
		response := common.Response{
			Success:  true,
			Data:     data,
			Metadata: metadata,
		}
		w.WriteHeader(httpStatus)
		if err := w.WriteJSON(response); err != nil {
			logger.Error("Failed to write JSON response: %v", err)
		}
	}
}

// cleanJSON removes null and empty fields from the response
func (h *Handler) cleanJSON(data interface{}) interface{} {
	// This is a simplified implementation
	// A full implementation would recursively clean nested structures
	// For now, we'll return the data as-is
	// TODO: Implement recursive cleaning
	return data
}

func (h *Handler) sendError(w common.ResponseWriter, statusCode int, code, message string, err error) {
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	} else if message != "" {
		errorMsg = message
	} else {
		errorMsg = code
	}

	response := map[string]interface{}{
		"_error":  errorMsg,
		"_retval": 1,
	}
	w.SetHeader("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if jsonErr := w.WriteJSON(response); jsonErr != nil {
		logger.Error("Failed to write JSON error response: %v", jsonErr)
	}
}

// FetchRowNumber calculates the row number of a specific record based on sorting and filtering
// Returns the 1-based row number of the record with the given primary key value
func (h *Handler) FetchRowNumber(ctx context.Context, tableName string, pkName string, pkValue string, options ExtendedRequestOptions, model any) (int64, error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic during FetchRowNumber: %v", r)
		}
	}()

	// Build the sort order SQL
	sortSQL := ""
	if len(options.Sort) > 0 {
		sortParts := make([]string, 0, len(options.Sort))
		for _, sort := range options.Sort {
			if sort.Column == "" {
				continue
			}
			direction := "ASC"
			if strings.EqualFold(sort.Direction, "desc") {
				direction = "DESC"
			}

			// Check if it's an expression (enclosed in brackets) - use directly without table prefix
			if strings.HasPrefix(sort.Column, "(") && strings.HasSuffix(sort.Column, ")") {
				sortParts = append(sortParts, fmt.Sprintf("%s %s", sort.Column, direction))
			} else {
				// Regular column - add table prefix
				sortParts = append(sortParts, fmt.Sprintf("%s.%s %s", tableName, sort.Column, direction))
			}
		}
		sortSQL = strings.Join(sortParts, ", ")
	} else {
		// Default sort by primary key
		sortSQL = fmt.Sprintf("%s.%s ASC", tableName, pkName)
	}

	// Build WHERE clauses from filters
	whereClauses := make([]string, 0)
	for i := range options.Filters {
		filter := &options.Filters[i]
		whereClause := h.buildFilterSQL(filter, tableName)
		if whereClause != "" {
			whereClauses = append(whereClauses, fmt.Sprintf("(%s)", whereClause))
		}
	}

	// Combine WHERE clauses
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Add custom SQL WHERE if provided
	if options.CustomSQLWhere != "" {
		if whereSQL == "" {
			whereSQL = "WHERE " + options.CustomSQLWhere
		} else {
			whereSQL += " AND (" + options.CustomSQLWhere + ")"
		}
	}

	// Build JOIN clauses from Expand options
	joinSQL := ""
	if len(options.Expand) > 0 {
		joinParts := make([]string, 0, len(options.Expand))
		for _, expand := range options.Expand {
			// Note: This is a simplified join - in production you'd need proper FK mapping
			joinParts = append(joinParts, fmt.Sprintf("LEFT JOIN %s ON %s.%s_id = %s.id",
				expand.Relation, tableName, expand.Relation, expand.Relation))
		}
		joinSQL = strings.Join(joinParts, "\n")
	}

	// Build the final query with parameterized PK value
	queryStr := fmt.Sprintf(`
		SELECT search.rn
		FROM (
			SELECT %[1]s.%[2]s,
				ROW_NUMBER() OVER(ORDER BY %[3]s) AS rn
			FROM %[1]s
			%[5]s
			%[4]s
		) search
		WHERE search.%[2]s = ?
	`,
		tableName, // [1] - table name
		pkName,    // [2] - primary key column name
		sortSQL,   // [3] - sort order SQL
		whereSQL,  // [4] - WHERE clause
		joinSQL,   // [5] - JOIN clauses
	)

	logger.Debug("FetchRowNumber query: %s, pkValue: %s", queryStr, pkValue)

	// Execute the raw query with parameterized PK value
	var result []struct {
		RN int64 `bun:"rn"`
	}
	err := h.db.Query(ctx, &result, queryStr, pkValue)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch row number: %w", err)
	}

	if len(result) == 0 {
		return 0, fmt.Errorf("no row found for primary key %s", pkValue)
	}

	return result[0].RN, nil
}

// buildFilterSQL converts a filter to SQL WHERE clause string
func (h *Handler) buildFilterSQL(filter *common.FilterOption, tableName string) string {
	qualifiedColumn := h.qualifyColumnName(filter.Column, tableName)

	switch strings.ToLower(filter.Operator) {
	case "eq", "equals":
		return fmt.Sprintf("%s = '%v'", qualifiedColumn, filter.Value)
	case "neq", "not_equals", "ne":
		return fmt.Sprintf("%s != '%v'", qualifiedColumn, filter.Value)
	case "gt", "greater_than":
		return fmt.Sprintf("%s > '%v'", qualifiedColumn, filter.Value)
	case "gte", "greater_than_equals", "ge":
		return fmt.Sprintf("%s >= '%v'", qualifiedColumn, filter.Value)
	case "lt", "less_than":
		return fmt.Sprintf("%s < '%v'", qualifiedColumn, filter.Value)
	case "lte", "less_than_equals", "le":
		return fmt.Sprintf("%s <= '%v'", qualifiedColumn, filter.Value)
	case "like":
		return fmt.Sprintf("%s LIKE '%v'", qualifiedColumn, filter.Value)
	case "ilike":
		return fmt.Sprintf("%s ILIKE '%v'", qualifiedColumn, filter.Value)
	case "in":
		if values, ok := filter.Value.([]any); ok {
			valueStrs := make([]string, len(values))
			for i, v := range values {
				valueStrs[i] = fmt.Sprintf("'%v'", v)
			}
			return fmt.Sprintf("%s IN (%s)", qualifiedColumn, strings.Join(valueStrs, ", "))
		}
		return ""
	case "is_null", "isnull":
		return fmt.Sprintf("(%s IS NULL OR %s = '')", qualifiedColumn, qualifiedColumn)
	case "is_not_null", "isnotnull":
		return fmt.Sprintf("(%s IS NOT NULL AND %s != '')", qualifiedColumn, qualifiedColumn)
	default:
		logger.Warn("Unknown filter operator in buildFilterSQL: %s", filter.Operator)
		return ""
	}
}

// setRowNumbersOnRecords sets the RowNumber field on each record if it exists
// The row number is calculated as offset + index + 1 (1-based)
func (h *Handler) setRowNumbersOnRecords(records any, offset int) {
	// Get the reflect value of the records
	recordsValue := reflect.ValueOf(records)
	if recordsValue.Kind() == reflect.Ptr {
		recordsValue = recordsValue.Elem()
	}

	// Ensure it's a slice
	if recordsValue.Kind() != reflect.Slice {
		logger.Debug("setRowNumbersOnRecords: records is not a slice, skipping")
		return
	}

	// Iterate through each record
	for i := 0; i < recordsValue.Len(); i++ {
		record := recordsValue.Index(i)

		// Dereference if it's a pointer
		if record.Kind() == reflect.Ptr {
			if record.IsNil() {
				continue
			}
			record = record.Elem()
		}

		// Ensure it's a struct
		if record.Kind() != reflect.Struct {
			continue
		}

		// Try to find and set the RowNumber field
		rowNumberField := record.FieldByName("RowNumber")
		if rowNumberField.IsValid() && rowNumberField.CanSet() {
			// Check if the field is of type int64
			if rowNumberField.Kind() == reflect.Int64 {
				rowNum := int64(offset + i + 1)
				rowNumberField.SetInt(rowNum)

			}
		}
	}
}

// filterExtendedOptions filters all column references, removing invalid ones and logging warnings
func (h *Handler) filterExtendedOptions(validator *common.ColumnValidator, options ExtendedRequestOptions, model interface{}) ExtendedRequestOptions {
	filtered := options

	// Filter base RequestOptions
	filtered.RequestOptions = validator.FilterRequestOptions(options.RequestOptions)

	// Filter SearchColumns
	filtered.SearchColumns = validator.FilterValidColumns(options.SearchColumns)

	// Filter AdvancedSQL column keys
	filteredAdvSQL := make(map[string]string)
	for colName, sqlExpr := range options.AdvancedSQL {
		if validator.IsValidColumn(colName) {
			filteredAdvSQL[colName] = sqlExpr
		} else {
			logger.Warn("Invalid column in advanced SQL removed: %s", colName)
		}
	}
	filtered.AdvancedSQL = filteredAdvSQL

	// ComputedQL columns are allowed to be any name since they're computed
	// No filtering needed for ComputedQL keys
	filtered.ComputedQL = options.ComputedQL

	// Filter Expand columns using the expand relation's model
	filteredExpands := make([]ExpandOption, 0, len(options.Expand))
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	for _, expand := range options.Expand {
		filteredExpand := expand

		// Get the relationship info for this expand relation
		relInfo := h.getRelationshipInfo(modelType, expand.Relation)
		if relInfo != nil && relInfo.relatedModel != nil {
			// Create a validator for the related model
			expandValidator := common.NewColumnValidator(relInfo.relatedModel)
			// Filter columns using the related model's validator
			filteredExpand.Columns = expandValidator.FilterValidColumns(expand.Columns)

			// Filter sort columns in the expand Sort string
			if expand.Sort != "" {
				sortFields := strings.Split(expand.Sort, ",")
				validSortFields := make([]string, 0, len(sortFields))
				for _, sortField := range sortFields {
					sortField = strings.TrimSpace(sortField)
					if sortField == "" {
						continue
					}

					// Extract column name (remove direction prefixes/suffixes)
					colName := sortField
					direction := ""

					if strings.HasPrefix(sortField, "-") {
						direction = "-"
						colName = strings.TrimPrefix(sortField, "-")
					} else if strings.HasPrefix(sortField, "+") {
						direction = "+"
						colName = strings.TrimPrefix(sortField, "+")
					}

					if strings.HasSuffix(strings.ToLower(colName), " desc") {
						direction = " desc"
						colName = strings.TrimSuffix(strings.ToLower(colName), " desc")
					} else if strings.HasSuffix(strings.ToLower(colName), " asc") {
						direction = " asc"
						colName = strings.TrimSuffix(strings.ToLower(colName), " asc")
					}

					colName = strings.TrimSpace(colName)

					// Validate the column name
					if expandValidator.IsValidColumn(colName) {
						validSortFields = append(validSortFields, direction+colName)
					} else if strings.HasPrefix(colName, "(") && strings.HasSuffix(colName, ")") {
						// Allow sort by expression/subquery, but validate for security
						if common.IsSafeSortExpression(colName) {
							validSortFields = append(validSortFields, direction+colName)
						} else {
							logger.Warn("Unsafe sort expression in expand '%s' removed: '%s'", expand.Relation, colName)
						}
					} else {
						logger.Warn("Invalid column in expand '%s' sort '%s' removed", expand.Relation, colName)
					}
				}
				filteredExpand.Sort = strings.Join(validSortFields, ",")
			}
		} else {
			// If we can't find the relationship, log a warning and skip column filtering
			logger.Warn("Cannot validate columns for unknown relation: %s", expand.Relation)
			// Keep the columns as-is if we can't validate them
			filteredExpand.Columns = expand.Columns
		}

		filteredExpands = append(filteredExpands, filteredExpand)
	}
	filtered.Expand = filteredExpands

	return filtered
}

// shouldUseNestedProcessor determines if we should use nested CUD processing
// It recursively checks if the data contains deeply nested relations or _request fields
// Simple one-level relations without further nesting don't require the nested processor
func (h *Handler) shouldUseNestedProcessor(data map[string]interface{}, model interface{}) bool {
	return common.ShouldUseNestedProcessor(data, model, h)
}

// Relationship support functions for nested CUD processing

// GetRelationshipInfo implements common.RelationshipInfoProvider interface
func (h *Handler) GetRelationshipInfo(modelType reflect.Type, relationName string) *common.RelationshipInfo {
	info := h.getRelationshipInfo(modelType, relationName)
	if info == nil {
		return nil
	}
	// Convert internal type to common type
	return &common.RelationshipInfo{
		FieldName:    info.fieldName,
		JSONName:     info.jsonName,
		RelationType: info.relationType,
		ForeignKey:   info.foreignKey,
		References:   info.references,
		JoinTable:    info.joinTable,
		RelatedModel: info.relatedModel,
	}
}

type relationshipInfo struct {
	fieldName    string
	jsonName     string
	relationType string // "belongsTo", "hasMany", "hasOne", "many2many"
	foreignKey   string
	references   string
	joinTable    string
	relatedModel interface{}
}

func (h *Handler) getRelationshipInfo(modelType reflect.Type, relationName string) *relationshipInfo {
	// Ensure we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Warn("Cannot get relationship info from non-struct type: %v", modelType)
		return nil
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		jsonTag := field.Tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]

		if jsonName == relationName {
			gormTag := field.Tag.Get("gorm")
			info := &relationshipInfo{
				fieldName: field.Name,
				jsonName:  jsonName,
			}

			// Parse GORM tag to determine relationship type and keys
			if strings.Contains(gormTag, "foreignKey") {
				info.foreignKey = h.extractTagValue(gormTag, "foreignKey")
				info.references = h.extractTagValue(gormTag, "references")

				// Determine if it's belongsTo or hasMany/hasOne
				if field.Type.Kind() == reflect.Slice {
					info.relationType = "hasMany"
					// Get the element type for slice
					elemType := field.Type.Elem()
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.relatedModel = reflect.New(elemType).Elem().Interface()
					}
				} else if field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Struct {
					info.relationType = "belongsTo"
					elemType := field.Type
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.relatedModel = reflect.New(elemType).Elem().Interface()
					}
				}
			} else if strings.Contains(gormTag, "many2many") {
				info.relationType = "many2many"
				info.joinTable = h.extractTagValue(gormTag, "many2many")
				// Get the element type for many2many (always slice)
				if field.Type.Kind() == reflect.Slice {
					elemType := field.Type.Elem()
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.relatedModel = reflect.New(elemType).Elem().Interface()
					}
				}
			} else {
				// Field has no GORM relationship tags, so it's not a relation
				return nil
			}

			return info
		}
	}
	return nil
}

func (h *Handler) extractTagValue(tag, key string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+":") {
			return strings.TrimPrefix(part, key+":")
		}
	}
	return ""
}

// HandleOpenAPI generates and returns the OpenAPI specification
func (h *Handler) HandleOpenAPI(w common.ResponseWriter, r common.Request) {
	// Import needed here to avoid circular dependency
	// The import is done inline
	// We'll use a factory function approach instead
	if h.openAPIGenerator == nil {
		logger.Error("OpenAPI generator not configured")
		h.sendError(w, http.StatusInternalServerError, "openapi_not_configured", "OpenAPI generation not configured", nil)
		return
	}

	spec, err := h.openAPIGenerator()
	if err != nil {
		logger.Error("Failed to generate OpenAPI spec: %v", err)
		h.sendError(w, http.StatusInternalServerError, "openapi_generation_error", "Failed to generate OpenAPI specification", err)
		return
	}

	w.SetHeader("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(spec))
	if err != nil {
		logger.Error("Error sending OpenAPI spec response: %v", err)
	}
}

// SetOpenAPIGenerator sets the OpenAPI generator function
// This allows avoiding circular dependencies
func (h *Handler) SetOpenAPIGenerator(generator func() (string, error)) {
	h.openAPIGenerator = generator
}
