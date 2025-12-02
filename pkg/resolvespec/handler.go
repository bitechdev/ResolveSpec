package resolvespec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Handler handles API requests using database and model abstractions
type Handler struct {
	db              common.Database
	registry        common.ModelRegistry
	nestedProcessor *common.NestedCUDProcessor
	hooks           *HookRegistry
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

// Hooks returns the hook registry for this handler
// Use this to register custom hooks for operations
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// GetDatabase returns the underlying database connection
// Implements common.SpecHandler interface
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

// handlePanic is a helper function to handle panics with stack traces
func (h *Handler) handlePanic(w common.ResponseWriter, method string, err interface{}) {
	stack := debug.Stack()
	logger.Error("Panic in %s: %v\nStack trace:\n%s", method, err, string(stack))
	h.sendError(w, http.StatusInternalServerError, "internal_error", fmt.Sprintf("Internal server error in %s", method), fmt.Errorf("%v", err))
}

// Handle processes API requests through router-agnostic interface
func (h *Handler) Handle(w common.ResponseWriter, r common.Request, params map[string]string) {
	// Capture panics and return error response
	defer func() {
		if err := recover(); err != nil {
			h.handlePanic(w, "Handle", err)
		}
	}()

	ctx := context.Background()

	body, err := r.Body()
	if err != nil {
		logger.Error("Failed to read request body: %v", err)
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Failed to read request body", err)
		return
	}

	var req common.RequestBody
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Error("Failed to decode request body: %v", err)
		h.sendError(w, http.StatusBadRequest, "invalid_request", "Invalid request body", err)
		return
	}

	schema := params["schema"]
	entity := params["entity"]
	id := params["id"]

	logger.Info("Handling %s operation for %s.%s", req.Operation, schema, entity)

	// Get model and populate context with request-scoped data
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		// Model not found - pass through to next route without writing response
		logger.Debug("Model not found for %s.%s, passing through to next route", schema, entity)
		return
	}

	// Validate that the model is a struct type (not a slice or pointer to slice)
	modelType := reflect.TypeOf(model)
	originalType := modelType
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Error("Model for %s.%s must be a struct type, got %v. Please register models as struct types, not slices or pointers to slices.", schema, entity, originalType)
		h.sendError(w, http.StatusInternalServerError, "invalid_model_type",
			fmt.Sprintf("Model must be a struct type, got %v. Ensure you register the struct (e.g., ModelCoreAccount{}) not a slice (e.g., []*ModelCoreAccount)", originalType),
			fmt.Errorf("invalid model type: %v", originalType))
		return
	}

	// If the registered model was a pointer or slice, use the unwrapped struct type
	if originalType != modelType {
		model = reflect.New(modelType).Elem().Interface()
	}

	// Create a pointer to the model type for database operations
	modelPtr := reflect.New(reflect.TypeOf(model)).Interface()
	tableName := h.getTableName(schema, entity, model)

	// Add request-scoped data to context
	ctx = WithRequestData(ctx, schema, entity, tableName, model, modelPtr)

	// Validate and filter columns in options (log warnings for invalid columns)
	validator := common.NewColumnValidator(model)
	req.Options = validator.FilterRequestOptions(req.Options)

	switch req.Operation {
	case "read":
		h.handleRead(ctx, w, id, req.Options)
	case "create":
		h.handleCreate(ctx, w, req.Data, req.Options)
	case "update":
		h.handleUpdate(ctx, w, id, req.ID, req.Data, req.Options)
	case "delete":
		h.handleDelete(ctx, w, id, req.Data)
	default:
		logger.Error("Invalid operation: %s", req.Operation)
		h.sendError(w, http.StatusBadRequest, "invalid_operation", "Invalid operation", nil)
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

	schema := params["schema"]
	entity := params["entity"]

	logger.Info("Getting metadata for %s.%s", schema, entity)

	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		// Model not found - pass through to next route without writing response
		logger.Debug("Model not found for %s.%s, passing through to next route", schema, entity)
		return
	}

	metadata := h.generateMetadata(schema, entity, model)
	h.sendResponse(w, metadata, nil)
}

func (h *Handler) handleRead(ctx context.Context, w common.ResponseWriter, id string, options common.RequestOptions) {
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

	logger.Info("Reading records from %s.%s", schema, entity)

	// Create the model pointer for Scan() operations
	sliceType := reflect.SliceOf(reflect.PointerTo(modelType))
	modelPtr := reflect.New(sliceType).Interface()

	// Start with Model() using the slice pointer to avoid "Model(nil)" errors in Count()
	// Bun's Model() accepts both single pointers and slice pointers
	query := h.db.NewSelect().Model(modelPtr)

	// Only set Table() if the model doesn't provide a table name via the underlying type
	// Create a temporary instance to check for TableNameProvider
	tempInstance := reflect.New(modelType).Interface()
	if provider, ok := tempInstance.(common.TableNameProvider); !ok || provider.TableName() == "" {
		query = query.Table(tableName)
	}

	if len(options.Columns) == 0 && (len(options.ComputedColumns) > 0) {
		logger.Debug("Populating options.Columns with all model columns since computed columns are additions")
		options.Columns = reflection.GetSQLModelColumns(model)
	}

	// Apply column selection
	if len(options.Columns) > 0 {
		logger.Debug("Selecting columns: %v", options.Columns)
		for _, col := range options.Columns {
			query = query.Column(reflection.ExtractSourceColumn(col))
		}
	}

	if len(options.ComputedColumns) > 0 {
		for _, cu := range options.ComputedColumns {
			logger.Debug("Applying computed column: %s", cu.Name)
			query = query.ColumnExpr(fmt.Sprintf("(%s) AS %s", cu.Expression, cu.Name))
		}
	}

	// Apply preloading
	if len(options.Preload) > 0 {
		query = h.applyPreloads(model, query, options.Preload)
	}

	// Apply filters
	for _, filter := range options.Filters {
		logger.Debug("Applying filter: %s %s %v", filter.Column, filter.Operator, filter.Value)
		query = h.applyFilter(query, filter)
	}

	// Apply sorting
	for _, sort := range options.Sort {
		direction := "ASC"
		if strings.EqualFold(sort.Direction, "desc") {
			direction = "DESC"
		}
		logger.Debug("Applying sort: %s %s", sort.Column, direction)
		query = query.Order(fmt.Sprintf("%s %s", sort.Column, direction))
	}

	// Get total count before pagination
	var total int

	// Try to get from cache first
	cacheKeyHash := cache.BuildQueryCacheKey(
		tableName,
		options.Filters,
		options.Sort,
		"", // No custom SQL WHERE in resolvespec
		"", // No custom SQL OR in resolvespec
	)
	cacheKey := cache.GetQueryTotalCacheKey(cacheKeyHash)

	// Try to retrieve from cache
	var cachedTotal cache.CachedTotal
	err := cache.GetDefaultCache().Get(ctx, cacheKey, &cachedTotal)
	if err == nil {
		total = cachedTotal.Total
		logger.Debug("Total records (from cache): %d", total)
	} else {
		// Cache miss - execute count query
		logger.Debug("Cache miss for query total")
		count, err := query.Count(ctx)
		if err != nil {
			logger.Error("Error counting records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "query_error", "Error counting records", err)
			return
		}
		total = count
		logger.Debug("Total records (from query): %d", total)

		// Store in cache
		cacheTTL := time.Minute * 2 // Default 2 minutes TTL
		cacheData := cache.CachedTotal{Total: total}
		if err := cache.GetDefaultCache().Set(ctx, cacheKey, cacheData, cacheTTL); err != nil {
			logger.Warn("Failed to cache query total: %v", err)
			// Don't fail the request if caching fails
		} else {
			logger.Debug("Cached query total with key: %s", cacheKey)
		}
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

	// Execute query
	var result interface{}
	if id != "" {
		logger.Debug("Querying single record with ID: %s", id)
		// For single record, create a new pointer to the struct type
		singleResult := reflect.New(modelType).Interface()

		query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(singleResult))), id)
		if err := query.Scan(ctx, singleResult); err != nil {
			logger.Error("Error querying record: %v", err)
			h.sendError(w, http.StatusInternalServerError, "query_error", "Error executing query", err)
			return
		}
		result = singleResult
	} else {
		logger.Debug("Querying multiple records")
		// Use the modelPtr already created and set on the query
		if err := query.Scan(ctx, modelPtr); err != nil {
			logger.Error("Error querying records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "query_error", "Error executing query", err)
			return
		}
		result = reflect.ValueOf(modelPtr).Elem().Interface()
	}

	logger.Info("Successfully retrieved records")

	limit := 0
	if options.Limit != nil {
		limit = *options.Limit
	}
	offset := 0
	if options.Offset != nil {
		offset = *options.Offset
	}

	h.sendResponse(w, result, &common.Metadata{
		Total:    int64(total),
		Filtered: int64(total),
		Limit:    limit,
		Offset:   offset,
	})
}

func (h *Handler) handleCreate(ctx context.Context, w common.ResponseWriter, data interface{}, options common.RequestOptions) {
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

	logger.Info("Creating records for %s.%s", schema, entity)

	// Check if data contains nested relations or _request field
	switch v := data.(type) {
	case map[string]interface{}:
		// Check if we should use nested processing
		if h.shouldUseNestedProcessor(v, model) {
			logger.Info("Using nested CUD processor for create operation")
			result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "insert", v, model, make(map[string]interface{}), tableName)
			if err != nil {
				logger.Error("Error in nested create: %v", err)
				h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating record with nested data", err)
				return
			}
			logger.Info("Successfully created record with nested data, ID: %v", result.ID)
			h.sendResponse(w, result.Data, nil)
			return
		}

		// Standard processing without nested relations
		query := h.db.NewInsert().Table(tableName)
		for key, value := range v {
			query = query.Value(key, value)
		}
		result, err := query.Exec(ctx)
		if err != nil {
			logger.Error("Error creating record: %v", err)
			h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating record", err)
			return
		}
		logger.Info("Successfully created record, rows affected: %d", result.RowsAffected())
		h.sendResponse(w, v, nil)

	case []map[string]interface{}:
		// Check if any item needs nested processing
		hasNestedData := false
		for _, item := range v {
			if h.shouldUseNestedProcessor(item, model) {
				hasNestedData = true
				break
			}
		}

		if hasNestedData {
			logger.Info("Using nested CUD processor for batch create with nested data")
			results := make([]map[string]interface{}, 0, len(v))
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				// Temporarily swap the database to use transaction
				originalDB := h.nestedProcessor
				h.nestedProcessor = common.NewNestedCUDProcessor(tx, h.registry, h)
				defer func() {
					h.nestedProcessor = originalDB
				}()

				for _, item := range v {
					result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "insert", item, model, make(map[string]interface{}), tableName)
					if err != nil {
						return fmt.Errorf("failed to process item: %w", err)
					}
					results = append(results, result.Data)
				}
				return nil
			})
			if err != nil {
				logger.Error("Error creating records with nested data: %v", err)
				h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating records with nested data", err)
				return
			}
			logger.Info("Successfully created %d records with nested data", len(results))
			h.sendResponse(w, results, nil)
			return
		}

		// Standard batch insert without nested relations
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			for _, item := range v {
				txQuery := tx.NewInsert().Table(tableName)
				for key, value := range item {
					txQuery = txQuery.Value(key, value)
				}
				if _, err := txQuery.Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("Error creating records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating records", err)
			return
		}
		logger.Info("Successfully created %d records", len(v))
		h.sendResponse(w, v, nil)

	case []interface{}:
		// Handle []interface{} type from JSON unmarshaling
		// Check if any item needs nested processing
		hasNestedData := false
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if h.shouldUseNestedProcessor(itemMap, model) {
					hasNestedData = true
					break
				}
			}
		}

		if hasNestedData {
			logger.Info("Using nested CUD processor for batch create with nested data ([]interface{})")
			results := make([]interface{}, 0, len(v))
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				// Temporarily swap the database to use transaction
				originalDB := h.nestedProcessor
				h.nestedProcessor = common.NewNestedCUDProcessor(tx, h.registry, h)
				defer func() {
					h.nestedProcessor = originalDB
				}()

				for _, item := range v {
					if itemMap, ok := item.(map[string]interface{}); ok {
						result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "insert", itemMap, model, make(map[string]interface{}), tableName)
						if err != nil {
							return fmt.Errorf("failed to process item: %w", err)
						}
						results = append(results, result.Data)
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error creating records with nested data: %v", err)
				h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating records with nested data", err)
				return
			}
			logger.Info("Successfully created %d records with nested data", len(results))
			h.sendResponse(w, results, nil)
			return
		}

		// Standard batch insert without nested relations
		list := make([]interface{}, 0)
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			for _, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					txQuery := tx.NewInsert().Table(tableName)
					for key, value := range itemMap {
						txQuery = txQuery.Value(key, value)
					}
					if _, err := txQuery.Exec(ctx); err != nil {
						return err
					}
					list = append(list, item)
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("Error creating records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "create_error", "Error creating records", err)
			return
		}
		logger.Info("Successfully created %d records", len(v))
		h.sendResponse(w, list, nil)

	default:
		logger.Error("Invalid data type for create operation: %T", data)
		h.sendError(w, http.StatusBadRequest, "invalid_data", "Invalid data type for create operation", nil)
	}
}

func (h *Handler) handleUpdate(ctx context.Context, w common.ResponseWriter, urlID string, reqID interface{}, data interface{}, options common.RequestOptions) {
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

	logger.Info("Updating records for %s.%s", schema, entity)

	switch updates := data.(type) {
	case map[string]interface{}:
		// Determine the ID to use
		var targetID interface{}
		switch {
		case urlID != "":
			targetID = urlID
		case reqID != nil:
			targetID = reqID
		case updates["id"] != nil:
			targetID = updates["id"]
		}

		// Check if we should use nested processing
		if h.shouldUseNestedProcessor(updates, model) {
			logger.Info("Using nested CUD processor for update operation")
			// Ensure ID is in the data map
			if targetID != nil {
				updates["id"] = targetID
			}
			result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "update", updates, model, make(map[string]interface{}), tableName)
			if err != nil {
				logger.Error("Error in nested update: %v", err)
				h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating record with nested data", err)
				return
			}
			logger.Info("Successfully updated record with nested data, rows: %d", result.AffectedRows)
			h.sendResponse(w, result.Data, nil)
			return
		}

		// Standard processing without nested relations
		query := h.db.NewUpdate().Table(tableName).SetMap(updates)

		// Apply conditions
		if urlID != "" {
			logger.Debug("Updating by URL ID: %s", urlID)
			query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), urlID)
		} else if reqID != nil {
			switch id := reqID.(type) {
			case string:
				logger.Debug("Updating by request ID: %s", id)
				query = query.Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), id)
			case []string:
				logger.Debug("Updating by multiple IDs: %v", id)
				query = query.Where(fmt.Sprintf("%s IN (?)", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), id)
			}
		}

		result, err := query.Exec(ctx)
		if err != nil {
			logger.Error("Update error: %v", err)
			h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating record(s)", err)
			return
		}

		if result.RowsAffected() == 0 {
			logger.Warn("No records found to update")
			h.sendError(w, http.StatusNotFound, "not_found", "No records found to update", nil)
			return
		}

		logger.Info("Successfully updated %d records", result.RowsAffected())
		h.sendResponse(w, data, nil)

	case []map[string]interface{}:
		// Batch update with array of objects
		hasNestedData := false
		for _, item := range updates {
			if h.shouldUseNestedProcessor(item, model) {
				hasNestedData = true
				break
			}
		}

		if hasNestedData {
			logger.Info("Using nested CUD processor for batch update with nested data")
			results := make([]map[string]interface{}, 0, len(updates))
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				// Temporarily swap the database to use transaction
				originalDB := h.nestedProcessor
				h.nestedProcessor = common.NewNestedCUDProcessor(tx, h.registry, h)
				defer func() {
					h.nestedProcessor = originalDB
				}()

				for _, item := range updates {
					result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "update", item, model, make(map[string]interface{}), tableName)
					if err != nil {
						return fmt.Errorf("failed to process item: %w", err)
					}
					results = append(results, result.Data)
				}
				return nil
			})
			if err != nil {
				logger.Error("Error updating records with nested data: %v", err)
				h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating records with nested data", err)
				return
			}
			logger.Info("Successfully updated %d records with nested data", len(results))
			h.sendResponse(w, results, nil)
			return
		}

		// Standard batch update without nested relations
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			for _, item := range updates {
				if itemID, ok := item["id"]; ok {

					txQuery := tx.NewUpdate().Table(tableName).SetMap(item).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
					if _, err := txQuery.Exec(ctx); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("Error updating records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating records", err)
			return
		}
		logger.Info("Successfully updated %d records", len(updates))
		h.sendResponse(w, updates, nil)

	case []interface{}:
		// Batch update with []interface{}
		hasNestedData := false
		for _, item := range updates {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if h.shouldUseNestedProcessor(itemMap, model) {
					hasNestedData = true
					break
				}
			}
		}

		if hasNestedData {
			logger.Info("Using nested CUD processor for batch update with nested data ([]interface{})")
			results := make([]interface{}, 0, len(updates))
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				// Temporarily swap the database to use transaction
				originalDB := h.nestedProcessor
				h.nestedProcessor = common.NewNestedCUDProcessor(tx, h.registry, h)
				defer func() {
					h.nestedProcessor = originalDB
				}()

				for _, item := range updates {
					if itemMap, ok := item.(map[string]interface{}); ok {
						result, err := h.nestedProcessor.ProcessNestedCUD(ctx, "update", itemMap, model, make(map[string]interface{}), tableName)
						if err != nil {
							return fmt.Errorf("failed to process item: %w", err)
						}
						results = append(results, result.Data)
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error updating records with nested data: %v", err)
				h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating records with nested data", err)
				return
			}
			logger.Info("Successfully updated %d records with nested data", len(results))
			h.sendResponse(w, results, nil)
			return
		}

		// Standard batch update without nested relations
		list := make([]interface{}, 0)
		err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
			for _, item := range updates {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemID, ok := itemMap["id"]; ok {

						txQuery := tx.NewUpdate().Table(tableName).SetMap(itemMap).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
						if _, err := txQuery.Exec(ctx); err != nil {
							return err
						}
						list = append(list, item)
					}
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("Error updating records: %v", err)
			h.sendError(w, http.StatusInternalServerError, "update_error", "Error updating records", err)
			return
		}
		logger.Info("Successfully updated %d records", len(list))
		h.sendResponse(w, list, nil)

	default:
		logger.Error("Invalid data type for update operation: %T", data)
		h.sendError(w, http.StatusBadRequest, "invalid_data", "Invalid data type for update operation", nil)
		return
	}
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

	logger.Info("Deleting records from %s.%s", schema, entity)

	// Handle batch delete from request data
	if data != nil {
		switch v := data.(type) {
		case []string:
			// Array of IDs as strings
			logger.Info("Batch delete with %d IDs ([]string)", len(v))
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, itemID := range v {

					query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
					if _, err := query.Exec(ctx); err != nil {
						return fmt.Errorf("failed to delete record %s: %w", itemID, err)
					}
				}
				return nil
			})
			if err != nil {
				logger.Error("Error in batch delete: %v", err)
				h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting records", err)
				return
			}
			logger.Info("Successfully deleted %d records", len(v))
			h.sendResponse(w, map[string]interface{}{"deleted": len(v)}, nil)
			return

		case []interface{}:
			// Array of IDs or objects with ID field
			logger.Info("Batch delete with %d items ([]interface{})", len(v))
			deletedCount := 0
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, item := range v {
					var itemID interface{}

					// Check if item is a string ID or object with id field
					switch v := item.(type) {
					case string:
						itemID = v
					case map[string]interface{}:
						itemID = v["id"]
					default:
						// Try to use the item directly as ID
						itemID = item
					}

					if itemID == nil {
						continue // Skip items without ID
					}

					query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
					result, err := query.Exec(ctx)
					if err != nil {
						return fmt.Errorf("failed to delete record %v: %w", itemID, err)
					}
					deletedCount += int(result.RowsAffected())
				}
				return nil
			})
			if err != nil {
				logger.Error("Error in batch delete: %v", err)
				h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting records", err)
				return
			}
			logger.Info("Successfully deleted %d records", deletedCount)
			h.sendResponse(w, map[string]interface{}{"deleted": deletedCount}, nil)
			return

		case []map[string]interface{}:
			// Array of objects with id field
			logger.Info("Batch delete with %d items ([]map[string]interface{})", len(v))
			deletedCount := 0
			err := h.db.RunInTransaction(ctx, func(tx common.Database) error {
				for _, item := range v {
					if itemID, ok := item["id"]; ok && itemID != nil {
						query := tx.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), itemID)
						result, err := query.Exec(ctx)
						if err != nil {
							return fmt.Errorf("failed to delete record %v: %w", itemID, err)
						}
						deletedCount += int(result.RowsAffected())
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
			h.sendResponse(w, map[string]interface{}{"deleted": deletedCount}, nil)
			return

		case map[string]interface{}:
			// Single object with id field
			if itemID, ok := v["id"]; ok && itemID != nil {
				id = fmt.Sprintf("%v", itemID)
			}
		}
	}

	// Single delete with URL ID
	if id == "" {
		logger.Error("Delete operation requires an ID")
		h.sendError(w, http.StatusBadRequest, "missing_id", "Delete operation requires an ID", nil)
		return
	}

	query := h.db.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", common.QuoteIdent(reflection.GetPrimaryKeyName(model))), id)

	result, err := query.Exec(ctx)
	if err != nil {
		logger.Error("Error deleting record: %v", err)
		h.sendError(w, http.StatusInternalServerError, "delete_error", "Error deleting record", err)
		return
	}

	if result.RowsAffected() == 0 {
		logger.Warn("No record found to delete with ID: %s", id)
		h.sendError(w, http.StatusNotFound, "not_found", "Record not found", nil)
		return
	}

	logger.Info("Successfully deleted record with ID: %s", id)
	h.sendResponse(w, nil, nil)
}

func (h *Handler) applyFilter(query common.SelectQuery, filter common.FilterOption) common.SelectQuery {
	switch filter.Operator {
	case "eq":
		return query.Where(fmt.Sprintf("%s = ?", filter.Column), filter.Value)
	case "neq":
		return query.Where(fmt.Sprintf("%s != ?", filter.Column), filter.Value)
	case "gt":
		return query.Where(fmt.Sprintf("%s > ?", filter.Column), filter.Value)
	case "gte":
		return query.Where(fmt.Sprintf("%s >= ?", filter.Column), filter.Value)
	case "lt":
		return query.Where(fmt.Sprintf("%s < ?", filter.Column), filter.Value)
	case "lte":
		return query.Where(fmt.Sprintf("%s <= ?", filter.Column), filter.Value)
	case "like":
		return query.Where(fmt.Sprintf("%s LIKE ?", filter.Column), filter.Value)
	case "ilike":
		return query.Where(fmt.Sprintf("%s ILIKE ?", filter.Column), filter.Value)
	case "in":
		return query.Where(fmt.Sprintf("%s IN (?)", filter.Column), filter.Value)
	default:
		return query
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
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Error("Model type must be a struct, got %v for %s.%s", modelType, schema, entity)
		return &common.TableMetadata{
			Schema:    schema,
			Table:     entity,
			Columns:   make([]common.Column, 0),
			Relations: make([]string, 0),
		}
	}

	metadata := &common.TableMetadata{
		Schema:    schema,
		Table:     entity,
		Columns:   make([]common.Column, 0),
		Relations: make([]string, 0),
	}

	// Generate metadata using reflection (same logic as before)
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		if !field.IsExported() {
			continue
		}

		gormTag := field.Tag.Get("gorm")
		jsonTag := field.Tag.Get("json")

		if jsonTag == "-" {
			continue
		}

		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}

		if field.Type.Kind() == reflect.Slice ||
			(field.Type.Kind() == reflect.Struct && field.Type.Name() != "Time") {
			metadata.Relations = append(metadata.Relations, jsonName)
			continue
		}

		column := common.Column{
			Name:       jsonName,
			Type:       getColumnType(field),
			IsNullable: isNullable(field),
			IsPrimary:  strings.Contains(gormTag, "primaryKey"),
			IsUnique:   strings.Contains(gormTag, "unique") || strings.Contains(gormTag, "uniqueIndex"),
			HasIndex:   strings.Contains(gormTag, "index") || strings.Contains(gormTag, "uniqueIndex"),
		}

		metadata.Columns = append(metadata.Columns, column)
	}

	return metadata
}

func (h *Handler) sendResponse(w common.ResponseWriter, data interface{}, metadata *common.Metadata) {
	w.SetHeader("Content-Type", "application/json")
	err := w.WriteJSON(common.Response{
		Success:  true,
		Data:     data,
		Metadata: metadata,
	})
	if err != nil {
		logger.Error("Error sending response: %v", err)
	}
}

func (h *Handler) sendError(w common.ResponseWriter, status int, code, message string, details interface{}) {
	w.SetHeader("Content-Type", "application/json")
	w.WriteHeader(status)
	err := w.WriteJSON(common.Response{
		Success: false,
		Error: &common.APIError{
			Code:    code,
			Message: message,
			Details: details,
			Detail:  fmt.Sprintf("%v", details),
		},
	})
	if err != nil {
		logger.Error("Error sending response: %v", err)
	}
}

// RegisterModel allows registering models at runtime
func (h *Handler) RegisterModel(schema, name string, model interface{}) error {
	fullname := fmt.Sprintf("%s.%s", schema, name)
	return h.registry.RegisterModel(fullname, model)
}

// shouldUseNestedProcessor determines if we should use nested CUD processing
// It checks if the data contains nested relations or a _request field
func (h *Handler) shouldUseNestedProcessor(data map[string]interface{}, model interface{}) bool {
	return common.ShouldUseNestedProcessor(data, model, h)
}

// Helper functions

func getColumnType(field reflect.StructField) string {
	// Check GORM type tag first
	gormTag := field.Tag.Get("gorm")
	if strings.Contains(gormTag, "type:") {
		parts := strings.Split(gormTag, "type:")
		if len(parts) > 1 {
			typePart := strings.Split(parts[1], ";")[0]
			return typePart
		}
	}

	// Map Go types to SQL types
	switch field.Type.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int32:
		return "integer"
	case reflect.Int64:
		return "bigint"
	case reflect.Float32:
		return "float"
	case reflect.Float64:
		return "double"
	case reflect.Bool:
		return "boolean"
	default:
		if field.Type.Name() == "Time" {
			return "timestamp"
		}
		return "unknown"
	}
}

func isNullable(field reflect.StructField) bool {
	// Check if it's a pointer type
	if field.Type.Kind() == reflect.Ptr {
		return true
	}

	// Check if it's a null type from sql package
	typeName := field.Type.Name()
	if strings.HasPrefix(typeName, "Null") {
		return true
	}

	// Check GORM tags
	gormTag := field.Tag.Get("gorm")
	return !strings.Contains(gormTag, "not null")
}

// Preload support functions

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

func (h *Handler) applyPreloads(model interface{}, query common.SelectQuery, preloads []common.PreloadOption) common.SelectQuery {
	modelType := reflect.TypeOf(model)

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Warn("Cannot apply preloads to non-struct type: %v", modelType)
		return query
	}

	for idx := range preloads {
		preload := preloads[idx]
		logger.Debug("Processing preload for relation: %s", preload.Relation)
		relInfo := h.getRelationshipInfo(modelType, preload.Relation)
		if relInfo == nil {
			logger.Warn("Relation %s not found in model", preload.Relation)
			continue
		}

		// Use the field name (capitalized) for ORM preloading
		// ORMs like GORM and Bun expect the struct field name, not the JSON name
		relationFieldName := relInfo.fieldName

		// Validate and fix WHERE clause to ensure it contains the relation prefix
		if len(preload.Where) > 0 {
			fixedWhere, err := common.ValidateAndFixPreloadWhere(preload.Where, relationFieldName)
			if err != nil {
				logger.Error("Invalid preload WHERE clause for relation '%s': %v", relationFieldName, err)
				panic(fmt.Errorf("invalid preload WHERE clause for relation '%s': %w", relationFieldName, err))
			}
			preload.Where = fixedWhere
		}

		logger.Debug("Applying preload: %s", relationFieldName)
		query = query.PreloadRelation(relationFieldName, func(sq common.SelectQuery) common.SelectQuery {
			if len(preload.Columns) == 0 && (len(preload.ComputedQL) > 0 || len(preload.OmitColumns) > 0) {
				preload.Columns = reflection.GetSQLModelColumns(model)
			}

			// Handle column selection and omission
			if len(preload.OmitColumns) > 0 {
				allCols := reflection.GetSQLModelColumns(model)
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

			if len(preload.Columns) > 0 {
				// Ensure foreign key is included in column selection for GORM to establish the relationship
				columns := make([]string, len(preload.Columns))
				copy(columns, preload.Columns)

				// Add foreign key if not already present
				if relInfo.foreignKey != "" {
					// Convert struct field name (e.g., DepartmentID) to snake_case (e.g., department_id)
					foreignKeyColumn := toSnakeCase(relInfo.foreignKey)

					hasForeignKey := false
					for _, col := range columns {
						if col == foreignKeyColumn || col == relInfo.foreignKey {
							hasForeignKey = true
							break
						}
					}
					if !hasForeignKey {
						columns = append(columns, foreignKeyColumn)
					}
				}

				sq = sq.Column(columns...)
			}

			if len(preload.Filters) > 0 {
				for _, filter := range preload.Filters {
					sq = h.applyFilter(sq, filter)
				}
			}
			if len(preload.Sort) > 0 {
				for _, sort := range preload.Sort {
					sq = sq.Order(fmt.Sprintf("%s %s", sort.Column, sort.Direction))
				}
			}

			if len(preload.Where) > 0 {
				sanitizedWhere := common.SanitizeWhereClause(preload.Where, reflection.ExtractTableNameOnly(preload.Relation))
				if len(sanitizedWhere) > 0 {
					sq = sq.Where(sanitizedWhere)
				}
			}

			if preload.Limit != nil && *preload.Limit > 0 {
				sq = sq.Limit(*preload.Limit)
			}

			return sq
		})

		logger.Debug("Applied Preload for relation: %s (field: %s)", preload.Relation, relationFieldName)
	}

	return query
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
				} else if field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Struct {
					info.relationType = "belongsTo"
				}
			} else if strings.Contains(gormTag, "many2many") {
				info.relationType = "many2many"
				info.joinTable = h.extractTagValue(gormTag, "many2many")
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

// toSnakeCase converts a PascalCase or camelCase string to snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if i > 0 && r >= 'A' && r <= 'Z' {
			// Check if previous character is lowercase or if next character is lowercase
			prevIsLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			// Add underscore if this is the start of a new word
			// (previous was lowercase OR this is followed by lowercase)
			if prevIsLower || nextIsLower {
				result.WriteByte('_')
			}
		}

		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
