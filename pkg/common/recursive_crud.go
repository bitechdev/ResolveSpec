package common

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// CRUDRequestProvider interface for models that provide CRUD request strings
type CRUDRequestProvider interface {
	GetRequest() string
}

// RelationshipInfoProvider interface for handlers that can provide relationship info
type RelationshipInfoProvider interface {
	GetRelationshipInfo(modelType reflect.Type, relationName string) *RelationshipInfo
}

// NestedCUDProcessor handles recursive processing of nested object graphs
type NestedCUDProcessor struct {
	db                 Database
	registry           ModelRegistry
	relationshipHelper RelationshipInfoProvider
}

// NewNestedCUDProcessor creates a new nested CUD processor
func NewNestedCUDProcessor(db Database, registry ModelRegistry, relationshipHelper RelationshipInfoProvider) *NestedCUDProcessor {
	return &NestedCUDProcessor{
		db:                 db,
		registry:           registry,
		relationshipHelper: relationshipHelper,
	}
}

// ProcessResult contains the result of processing a CUD operation
type ProcessResult struct {
	ID           interface{}            // The ID of the processed record
	AffectedRows int64                  // Number of rows affected
	Data         map[string]interface{} // The processed data
	RelationData map[string]interface{} // Data from processed relations
}

// ProcessNestedCUD recursively processes nested object graphs for Create, Update, Delete operations
// with automatic foreign key resolution
func (p *NestedCUDProcessor) ProcessNestedCUD(
	ctx context.Context,
	operation string, // "insert", "update", or "delete"
	data map[string]interface{},
	model interface{},
	parentIDs map[string]interface{}, // Parent IDs for foreign key resolution
	tableName string,
) (*ProcessResult, error) {
	logger.Info("Processing nested CUD: operation=%s, table=%s", operation, tableName)

	result := &ProcessResult{
		Data:         make(map[string]interface{}),
		RelationData: make(map[string]interface{}),
	}

	// Check if data has a _request field that overrides the operation
	if requestOp := p.extractCRUDRequest(data); requestOp != "" {
		logger.Debug("Found _request override: %s", requestOp)
		operation = requestOp
	}

	// Get model type for reflection
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		logger.Error("Invalid model type: operation=%s, table=%s, modelType=%v, expected struct", operation, tableName, modelType)
		return nil, fmt.Errorf("model must be a struct type, got %v", modelType)
	}

	// Separate relation fields from regular fields
	relationFields := make(map[string]*RelationshipInfo)
	regularData := make(map[string]interface{})

	for key, value := range data {
		// Skip _request field in actual data processing
		if key == "_request" {
			continue
		}

		// Check if this field is a relation
		relInfo := p.relationshipHelper.GetRelationshipInfo(modelType, key)
		if relInfo != nil {
			relationFields[key] = relInfo
			result.RelationData[key] = value
		} else {
			regularData[key] = value
		}
	}

	// Filter regularData to only include fields that exist in the model
	// Use MapToStruct to validate and filter fields
	regularData = p.filterValidFields(regularData, model)

	// Inject parent IDs for foreign key resolution
	p.injectForeignKeys(regularData, modelType, parentIDs)

	// Get the primary key name for this model
	pkName := reflection.GetPrimaryKeyName(model)

	// Check if we have any data to process (besides _request)
	hasData := len(regularData) > 0

	// Process based on operation
	switch strings.ToLower(operation) {
	case "insert", "create":
		// Only perform insert if we have data to insert
		if hasData {
			id, err := p.processInsert(ctx, regularData, tableName)
			if err != nil {
				logger.Error("Insert failed for table=%s, data=%+v, error=%v", tableName, regularData, err)
				return nil, fmt.Errorf("insert failed: %w", err)
			}
			result.ID = id
			result.AffectedRows = 1
			result.Data = regularData

			// Process child relations after parent insert (to get parent ID)
			if err := p.processChildRelations(ctx, "insert", id, relationFields, result.RelationData, modelType, parentIDs); err != nil {
				logger.Error("Failed to process child relations after insert: table=%s, parentID=%v, relations=%+v, error=%v", tableName, id, relationFields, err)
				return nil, fmt.Errorf("failed to process child relations: %w", err)
			}
		} else {
			logger.Debug("Skipping insert for %s - no data columns besides _request", tableName)
		}

	case "update":
		// Only perform update if we have data to update
		if hasData {
			rows, err := p.processUpdate(ctx, regularData, tableName, data[pkName])
			if err != nil {
				logger.Error("Update failed for table=%s, id=%v, data=%+v, error=%v", tableName, data[pkName], regularData, err)
				return nil, fmt.Errorf("update failed: %w", err)
			}
			result.ID = data[pkName]
			result.AffectedRows = rows
			result.Data = regularData

			// Process child relations for update
			if err := p.processChildRelations(ctx, "update", data[pkName], relationFields, result.RelationData, modelType, parentIDs); err != nil {
				logger.Error("Failed to process child relations after update: table=%s, parentID=%v, relations=%+v, error=%v", tableName, data[pkName], relationFields, err)
				return nil, fmt.Errorf("failed to process child relations: %w", err)
			}
		} else {
			logger.Debug("Skipping update for %s - no data columns besides _request", tableName)
			result.ID = data[pkName]
		}

	case "delete":
		// Process child relations first (for referential integrity)
		if err := p.processChildRelations(ctx, "delete", data[pkName], relationFields, result.RelationData, modelType, parentIDs); err != nil {
			logger.Error("Failed to process child relations before delete: table=%s, id=%v, relations=%+v, error=%v", tableName, data[pkName], relationFields, err)
			return nil, fmt.Errorf("failed to process child relations before delete: %w", err)
		}

		rows, err := p.processDelete(ctx, tableName, data[pkName])
		if err != nil {
			logger.Error("Delete failed for table=%s, id=%v, error=%v", tableName, data[pkName], err)
			return nil, fmt.Errorf("delete failed: %w", err)
		}
		result.ID = data[pkName]
		result.AffectedRows = rows
		result.Data = regularData

	default:
		logger.Error("Unsupported operation: %s for table=%s", operation, tableName)
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}

	logger.Info("Nested CUD completed: operation=%s, id=%v, rows=%d", operation, result.ID, result.AffectedRows)
	return result, nil
}

// extractCRUDRequest extracts the request field from data if present
func (p *NestedCUDProcessor) extractCRUDRequest(data map[string]interface{}) string {
	if request, ok := data["_request"]; ok {
		if requestStr, ok := request.(string); ok {
			return strings.ToLower(strings.TrimSpace(requestStr))
		}
	}
	return ""
}

// filterValidFields filters input data to only include fields that exist in the model
// Uses reflection.MapToStruct to validate fields and extract only those that match the model
func (p *NestedCUDProcessor) filterValidFields(data map[string]interface{}, model interface{}) map[string]interface{} {
	if len(data) == 0 {
		return data
	}

	// Create a new instance of the model to use with MapToStruct
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return data
	}

	// Create a new instance of the model
	tempModel := reflect.New(modelType).Interface()

	// Use MapToStruct to map the data - this will only map valid fields
	err := reflection.MapToStruct(data, tempModel)
	if err != nil {
		logger.Debug("Error mapping data to model: %v", err)
		return data
	}

	// Extract the mapped fields back into a map
	// This effectively filters out any fields that don't exist in the model
	filteredData := make(map[string]interface{})
	tempModelValue := reflect.ValueOf(tempModel).Elem()

	for key, value := range data {
		// Check if the field was successfully mapped
		if fieldWasMapped(tempModelValue, modelType, key) {
			filteredData[key] = value
		} else {
			logger.Debug("Skipping invalid field '%s' - not found in model %v", key, modelType)
		}
	}

	return filteredData
}

// fieldWasMapped checks if a field with the given key was mapped to the model
func fieldWasMapped(modelValue reflect.Value, modelType reflect.Type, key string) bool {
	// Look for the field by JSON tag or field name
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check JSON tag
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if len(parts) > 0 && parts[0] == key {
				return true
			}
		}

		// Check bun tag
		bunTag := field.Tag.Get("bun")
		if bunTag != "" && bunTag != "-" {
			if colName := reflection.ExtractColumnFromBunTag(bunTag); colName == key {
				return true
			}
		}

		// Check gorm tag
		gormTag := field.Tag.Get("gorm")
		if gormTag != "" && gormTag != "-" {
			if colName := reflection.ExtractColumnFromGormTag(gormTag); colName == key {
				return true
			}
		}

		// Check lowercase field name
		if strings.EqualFold(field.Name, key) {
			return true
		}

		// Handle embedded structs recursively
		if field.Anonymous {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				embeddedValue := modelValue.Field(i)
				if embeddedValue.Kind() == reflect.Ptr {
					if embeddedValue.IsNil() {
						continue
					}
					embeddedValue = embeddedValue.Elem()
				}
				if fieldWasMapped(embeddedValue, fieldType, key) {
					return true
				}
			}
		}
	}

	return false
}

// injectForeignKeys injects parent IDs into data for foreign key fields
func (p *NestedCUDProcessor) injectForeignKeys(data map[string]interface{}, modelType reflect.Type, parentIDs map[string]interface{}) {
	if len(parentIDs) == 0 {
		return
	}

	// Iterate through model fields to find foreign key fields
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		jsonTag := field.Tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]

		// Check if this field is a foreign key and we have a parent ID for it
		// Common patterns: DepartmentID, ManagerID, ProjectID, etc.
		for parentKey, parentID := range parentIDs {
			// Match field name patterns like "department_id" with parent key "department"
			if strings.EqualFold(jsonName, parentKey+"_id") ||
				strings.EqualFold(jsonName, parentKey+"id") ||
				strings.EqualFold(field.Name, parentKey+"ID") {
				// Only inject if not already present
				if _, exists := data[jsonName]; !exists {
					logger.Debug("Injecting foreign key: %s = %v", jsonName, parentID)
					data[jsonName] = parentID
				}
			}
		}
	}
}

// processInsert handles insert operation
func (p *NestedCUDProcessor) processInsert(
	ctx context.Context,
	data map[string]interface{},
	tableName string,
) (interface{}, error) {
	logger.Debug("Inserting into %s with data: %+v", tableName, data)

	query := p.db.NewInsert().Table(tableName)

	for key, value := range data {
		query = query.Value(key, value)
	}
	pkName := reflection.GetPrimaryKeyName(tableName)
	// Add RETURNING clause to get the inserted ID
	query = query.Returning(pkName)

	result, err := query.Exec(ctx)
	if err != nil {
		logger.Error("Insert execution failed: table=%s, data=%+v, error=%v", tableName, data, err)
		return nil, fmt.Errorf("insert exec failed: %w", err)
	}

	// Try to get the ID
	var id interface{}
	if lastID, err := result.LastInsertId(); err == nil && lastID > 0 {
		id = lastID
	} else if data[pkName] != nil {
		id = data[pkName]
	}

	logger.Debug("Insert successful, ID: %v, rows affected: %d", id, result.RowsAffected())
	return id, nil
}

// processUpdate handles update operation
func (p *NestedCUDProcessor) processUpdate(
	ctx context.Context,
	data map[string]interface{},
	tableName string,
	id interface{},
) (int64, error) {
	if id == nil {
		logger.Error("Update requires an ID: table=%s, data=%+v", tableName, data)
		return 0, fmt.Errorf("update requires an ID")
	}

	logger.Debug("Updating %s with ID %v, data: %+v", tableName, id, data)

	query := p.db.NewUpdate().Table(tableName).SetMap(data).Where(fmt.Sprintf("%s = ?", QuoteIdent(reflection.GetPrimaryKeyName(tableName))), id)

	result, err := query.Exec(ctx)
	if err != nil {
		logger.Error("Update execution failed: table=%s, id=%v, data=%+v, error=%v", tableName, id, data, err)
		return 0, fmt.Errorf("update exec failed: %w", err)
	}

	rows := result.RowsAffected()
	logger.Debug("Update successful, rows affected: %d", rows)
	return rows, nil
}

// processDelete handles delete operation
func (p *NestedCUDProcessor) processDelete(ctx context.Context, tableName string, id interface{}) (int64, error) {
	if id == nil {
		logger.Error("Delete requires an ID: table=%s", tableName)
		return 0, fmt.Errorf("delete requires an ID")
	}

	logger.Debug("Deleting from %s with ID %v", tableName, id)

	query := p.db.NewDelete().Table(tableName).Where(fmt.Sprintf("%s = ?", QuoteIdent(reflection.GetPrimaryKeyName(tableName))), id)

	result, err := query.Exec(ctx)
	if err != nil {
		logger.Error("Delete execution failed: table=%s, id=%v, error=%v", tableName, id, err)
		return 0, fmt.Errorf("delete exec failed: %w", err)
	}

	rows := result.RowsAffected()
	logger.Debug("Delete successful, rows affected: %d", rows)
	return rows, nil
}

// processChildRelations recursively processes child relations
func (p *NestedCUDProcessor) processChildRelations(
	ctx context.Context,
	operation string,
	parentID interface{},
	relationFields map[string]*RelationshipInfo,
	relationData map[string]interface{},
	parentModelType reflect.Type,
	incomingParentIDs map[string]interface{}, // IDs from all ancestors
) error {
	for relationName, relInfo := range relationFields {
		relationValue, exists := relationData[relationName]
		if !exists || relationValue == nil {
			continue
		}

		logger.Debug("Processing relation: %s, type: %s", relationName, relInfo.RelationType)

		// Get the related model
		field, found := parentModelType.FieldByName(relInfo.FieldName)
		if !found {
			logger.Error("Field %s not found in model type %v for relation %s", relInfo.FieldName, parentModelType, relationName)
			continue
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
		relatedTableName := p.getTableNameForModel(relatedModel, relInfo.JSONName)

		// Prepare parent IDs for foreign key injection
		// Start by copying all incoming parent IDs (from ancestors)
		parentIDs := make(map[string]interface{})
		for k, v := range incomingParentIDs {
			parentIDs[k] = v
		}
		logger.Debug("Inherited %d parent IDs from ancestors: %+v", len(incomingParentIDs), incomingParentIDs)

		// Add the current parent's primary key to the parentIDs map
		// This ensures nested children have access to all ancestor IDs
		if parentID != nil && parentModelType != nil {
			// Get the parent model's primary key field name
			parentPKFieldName := reflection.GetPrimaryKeyName(parentModelType)
			if parentPKFieldName != "" {
				// Get the JSON name for the primary key field
				parentPKJSONName := reflection.GetJSONNameForField(parentModelType, parentPKFieldName)
				baseName := ""
				if len(parentPKJSONName) > 1 {
					baseName = parentPKJSONName
				} else {
					// Add parent's PK to the map using the base model name
					baseName = strings.TrimSuffix(parentPKFieldName, "ID")
					baseName = strings.TrimSuffix(strings.ToLower(baseName), "_id")
					if baseName == "" {
						baseName = "parent"
					}
				}

				parentIDs[baseName] = parentID
				logger.Debug("Added current parent PK to parentIDs map: %s=%v (from field %s)", baseName, parentID, parentPKFieldName)
			}
		}

		// Also add the foreign key reference if specified
		if relInfo.ForeignKey != "" && parentID != nil {
			// Extract the base name from foreign key (e.g., "DepartmentID" -> "Department")
			baseName := strings.TrimSuffix(relInfo.ForeignKey, "ID")
			baseName = strings.TrimSuffix(strings.ToLower(baseName), "_id")
			// Only add if different from what we already added
			if _, exists := parentIDs[baseName]; !exists {
				parentIDs[baseName] = parentID
				logger.Debug("Added foreign key to parentIDs map: %s=%v (from FK %s)", baseName, parentID, relInfo.ForeignKey)
			}
		}

		logger.Debug("Final parentIDs map for relation %s: %+v", relationName, parentIDs)

		// Determine which field name to use for setting parent ID in child data
		// Priority: Use foreign key field name if specified
		var foreignKeyFieldName string
		if relInfo.ForeignKey != "" {
			// Get the JSON name for the foreign key field in the child model
			foreignKeyFieldName = reflection.GetJSONNameForField(relatedModelType, relInfo.ForeignKey)
			if foreignKeyFieldName == "" {
				// Fallback to lowercase field name
				foreignKeyFieldName = strings.ToLower(relInfo.ForeignKey)
			}
			logger.Debug("Using foreign key field for direct assignment: %s (from FK %s)", foreignKeyFieldName, relInfo.ForeignKey)
		}

		// Get the primary key name for the child model to avoid overwriting it in recursive relationships
		childPKName := reflection.GetPrimaryKeyName(relatedModel)
		childPKFieldName := reflection.GetJSONNameForField(relatedModelType, childPKName)
		if childPKFieldName == "" {
			childPKFieldName = strings.ToLower(childPKName)
		}

		logger.Debug("Processing relation with foreignKeyField=%s, childPK=%s", foreignKeyFieldName, childPKFieldName)

		// Process based on relation type and data structure
		switch v := relationValue.(type) {
		case map[string]interface{}:
			// Single related object - directly set foreign key if specified
			// IMPORTANT: In recursive relationships, don't overwrite the primary key
			if parentID != nil && foreignKeyFieldName != "" && foreignKeyFieldName != childPKFieldName {
				v[foreignKeyFieldName] = parentID
				logger.Debug("Set foreign key in single relation: %s=%v", foreignKeyFieldName, parentID)
			} else if foreignKeyFieldName == childPKFieldName {
				logger.Debug("Skipping foreign key assignment - same as primary key (recursive relationship): %s", foreignKeyFieldName)
			}
			_, err := p.ProcessNestedCUD(ctx, operation, v, relatedModel, parentIDs, relatedTableName)
			if err != nil {
				logger.Error("Failed to process single relation: name=%s, table=%s, operation=%s, parentID=%v, data=%+v, error=%v",
					relationName, relatedTableName, operation, parentID, v, err)
				return fmt.Errorf("failed to process relation %s: %w", relationName, err)
			}

		case []interface{}:
			// Multiple related objects
			for i, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					// Directly set foreign key if specified
					// IMPORTANT: In recursive relationships, don't overwrite the primary key
					if parentID != nil && foreignKeyFieldName != "" && foreignKeyFieldName != childPKFieldName {
						itemMap[foreignKeyFieldName] = parentID
						logger.Debug("Set foreign key in relation array[%d]: %s=%v", i, foreignKeyFieldName, parentID)
					} else if foreignKeyFieldName == childPKFieldName {
						logger.Debug("Skipping foreign key assignment in array[%d] - same as primary key (recursive relationship): %s", i, foreignKeyFieldName)
					}
					_, err := p.ProcessNestedCUD(ctx, operation, itemMap, relatedModel, parentIDs, relatedTableName)
					if err != nil {
						logger.Error("Failed to process relation array item: name=%s[%d], table=%s, operation=%s, parentID=%v, data=%+v, error=%v",
							relationName, i, relatedTableName, operation, parentID, itemMap, err)
						return fmt.Errorf("failed to process relation %s[%d]: %w", relationName, i, err)
					}
				} else {
					logger.Warn("Relation array item is not a map: name=%s[%d], type=%T", relationName, i, item)
				}
			}

		case []map[string]interface{}:
			// Multiple related objects (typed slice)
			for i, itemMap := range v {
				// Directly set foreign key if specified
				// IMPORTANT: In recursive relationships, don't overwrite the primary key
				if parentID != nil && foreignKeyFieldName != "" && foreignKeyFieldName != childPKFieldName {
					itemMap[foreignKeyFieldName] = parentID
					logger.Debug("Set foreign key in relation typed array[%d]: %s=%v", i, foreignKeyFieldName, parentID)
				} else if foreignKeyFieldName == childPKFieldName {
					logger.Debug("Skipping foreign key assignment in typed array[%d] - same as primary key (recursive relationship): %s", i, foreignKeyFieldName)
				}
				_, err := p.ProcessNestedCUD(ctx, operation, itemMap, relatedModel, parentIDs, relatedTableName)
				if err != nil {
					logger.Error("Failed to process relation typed array item: name=%s[%d], table=%s, operation=%s, parentID=%v, data=%+v, error=%v",
						relationName, i, relatedTableName, operation, parentID, itemMap, err)
					return fmt.Errorf("failed to process relation %s[%d]: %w", relationName, i, err)
				}
			}

		default:
			logger.Error("Unsupported relation data type: name=%s, type=%T, value=%+v", relationName, relationValue, relationValue)
		}
	}

	return nil
}

// getTableNameForModel gets the table name for a model
func (p *NestedCUDProcessor) getTableNameForModel(model interface{}, defaultName string) string {
	if provider, ok := model.(TableNameProvider); ok {
		tableName := provider.TableName()
		if tableName != "" {
			return tableName
		}
	}
	return defaultName
}

// ShouldUseNestedProcessor determines if we should use nested CUD processing
// It recursively checks if the data contains:
// 1. A _request field at any level, OR
// 2. Nested relations that themselves contain further nested relations or _request fields
// This ensures nested processing is only used when there are deeply nested operations
func ShouldUseNestedProcessor(data map[string]interface{}, model interface{}, relationshipHelper RelationshipInfoProvider) bool {
	return shouldUseNestedProcessorDepth(data, model, relationshipHelper, 0)
}

// shouldUseNestedProcessorDepth is the internal recursive implementation with depth tracking
func shouldUseNestedProcessorDepth(data map[string]interface{}, model interface{}, relationshipHelper RelationshipInfoProvider, depth int) bool {
	// Check for _request field
	if _, hasCRUDRequest := data["_request"]; hasCRUDRequest {
		return true
	}

	// Get model type
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return false
	}

	// Check if data contains any fields that are relations (nested objects or arrays)
	for key, value := range data {
		// Skip _request and regular scalar fields
		if key == "_request" {
			continue
		}

		// Check if this field is a relation in the model
		relInfo := relationshipHelper.GetRelationshipInfo(modelType, key)
		if relInfo != nil {
			// Check if the value is actually nested data (object or array)
			switch v := value.(type) {
			case map[string]interface{}, []interface{}, []map[string]interface{}:
				// If we're already at a nested level (depth > 0) and found a relation,
				// that means we have multi-level nesting, so return true
				if depth > 0 {
					return true
				}
				// At depth 0, recurse to check if the nested data has further nesting
				switch typedValue := v.(type) {
				case map[string]interface{}:
					if shouldUseNestedProcessorDepth(typedValue, relInfo.RelatedModel, relationshipHelper, depth+1) {
						return true
					}
				case []interface{}:
					for _, item := range typedValue {
						if itemMap, ok := item.(map[string]interface{}); ok {
							if shouldUseNestedProcessorDepth(itemMap, relInfo.RelatedModel, relationshipHelper, depth+1) {
								return true
							}
						}
					}
				case []map[string]interface{}:
					for _, itemMap := range typedValue {
						if shouldUseNestedProcessorDepth(itemMap, relInfo.RelatedModel, relationshipHelper, depth+1) {
							return true
						}
					}
				}
			}
		}
	}

	return false
}
