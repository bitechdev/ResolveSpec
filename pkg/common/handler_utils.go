package common

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// ValidateAndUnwrapModelResult contains the result of model validation
type ValidateAndUnwrapModelResult struct {
	ModelType    reflect.Type
	Model        interface{}
	ModelPtr     interface{}
	OriginalType reflect.Type
}

// ValidateAndUnwrapModel validates that a model is a struct type and unwraps
// pointers, slices, and arrays to get to the base struct type.
// Returns an error if the model is not a valid struct type.
func ValidateAndUnwrapModel(model interface{}) (*ValidateAndUnwrapModelResult, error) {
	modelType := reflect.TypeOf(model)
	originalType := modelType

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("model must be a struct type, got %v. Ensure you register the struct (e.g., ModelCoreAccount{}) not a slice (e.g., []*ModelCoreAccount)", originalType)
	}

	// If the registered model was a pointer or slice, use the unwrapped struct type
	if originalType != modelType {
		model = reflect.New(modelType).Elem().Interface()
	}

	// Create a pointer to the model type for database operations
	modelPtr := reflect.New(reflect.TypeOf(model)).Interface()

	return &ValidateAndUnwrapModelResult{
		ModelType:    modelType,
		Model:        model,
		ModelPtr:     modelPtr,
		OriginalType: originalType,
	}, nil
}

// ExtractTagValue extracts the value for a given key from a struct tag string.
// It handles both semicolon and comma-separated tag formats (e.g., GORM and BUN tags).
// For tags like "json:name;validate:required" it will extract "name" for key "json".
// For tags like "rel:has-many,join:table" it will extract "table" for key "join".
func ExtractTagValue(tag, key string) string {
	// Split by both semicolons and commas to handle different tag formats
	// We need to be smart about this - commas can be part of values
	// So we'll try semicolon first, then comma if needed
	separators := []string{";", ","}

	for _, sep := range separators {
		parts := strings.Split(tag, sep)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, key+":") {
				return strings.TrimPrefix(part, key+":")
			}
		}
	}
	return ""
}

// GetRelationshipInfo analyzes a model type and extracts relationship metadata
// for a specific relation field identified by its JSON name.
// Returns nil if the field is not found or is not a valid relationship.
func GetRelationshipInfo(modelType reflect.Type, relationName string) *RelationshipInfo {
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
			bunTag := field.Tag.Get("bun")
			info := &RelationshipInfo{
				FieldName: field.Name,
				JSONName:  jsonName,
			}

			if strings.Contains(bunTag, "rel:") || strings.Contains(bunTag, "join:") {
				//bun:"rel:has-many,join:rid_hub=rid_hub_division"
				if strings.Contains(bunTag, "has-many") {
					info.RelationType = "hasMany"
				} else if strings.Contains(bunTag, "has-one") {
					info.RelationType = "hasOne"
				} else if strings.Contains(bunTag, "belongs-to") {
					info.RelationType = "belongsTo"
				} else if strings.Contains(bunTag, "many-to-many") {
					info.RelationType = "many2many"
				} else {
					info.RelationType = "hasOne"
				}

				// Extract join info
				joinPart := ExtractTagValue(bunTag, "join")
				if joinPart != "" && info.RelationType == "many2many" {
					// For many2many, the join part is the join table name
					info.JoinTable = joinPart
				} else if joinPart != "" {
					// For other relations, parse foreignKey and references
					joinParts := strings.Split(joinPart, "=")
					if len(joinParts) == 2 {
						info.ForeignKey = joinParts[0]
						info.References = joinParts[1]
					}
				}

				// Get related model type
				if field.Type.Kind() == reflect.Slice {
					elemType := field.Type.Elem()
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.RelatedModel = reflect.New(elemType).Elem().Interface()
					}
				} else if field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Struct {
					elemType := field.Type
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.RelatedModel = reflect.New(elemType).Elem().Interface()
					}
				}

				return info
			}

			// Parse GORM tag to determine relationship type and keys
			if strings.Contains(gormTag, "foreignKey") {
				info.ForeignKey = ExtractTagValue(gormTag, "foreignKey")
				info.References = ExtractTagValue(gormTag, "references")

				// Determine if it's belongsTo or hasMany/hasOne
				if field.Type.Kind() == reflect.Slice {
					info.RelationType = "hasMany"
					// Get the element type for slice
					elemType := field.Type.Elem()
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.RelatedModel = reflect.New(elemType).Elem().Interface()
					}
				} else if field.Type.Kind() == reflect.Ptr || field.Type.Kind() == reflect.Struct {
					info.RelationType = "belongsTo"
					elemType := field.Type
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.RelatedModel = reflect.New(elemType).Elem().Interface()
					}
				}
			} else if strings.Contains(gormTag, "many2many") {
				info.RelationType = "many2many"
				info.JoinTable = ExtractTagValue(gormTag, "many2many")
				// Get the element type for many2many (always slice)
				if field.Type.Kind() == reflect.Slice {
					elemType := field.Type.Elem()
					if elemType.Kind() == reflect.Ptr {
						elemType = elemType.Elem()
					}
					if elemType.Kind() == reflect.Struct {
						info.RelatedModel = reflect.New(elemType).Elem().Interface()
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

// RelationPathToBunAlias converts a relation path (e.g., "Order.Customer") to a Bun alias format.
// It converts to lowercase and replaces dots with double underscores.
// For example: "Order.Customer" -> "order__customer"
func RelationPathToBunAlias(relationPath string) string {
	if relationPath == "" {
		return ""
	}
	// Convert to lowercase and replace dots with double underscores
	alias := strings.ToLower(relationPath)
	alias = strings.ReplaceAll(alias, ".", "__")
	return alias
}

// ReplaceTableReferencesInSQL replaces references to a base table name in a SQL expression
// with the appropriate alias for the current preload level.
// For example, if baseTableName is "mastertaskitem" and targetAlias is "mal__mal",
// it will replace "mastertaskitem.rid_mastertaskitem" with "mal__mal.rid_mastertaskitem"
func ReplaceTableReferencesInSQL(sqlExpr, baseTableName, targetAlias string) string {
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

// GetTableNameFromModel extracts the table name from a model.
// It checks the bun tag first, then falls back to converting the struct name to snake_case.
func GetTableNameFromModel(model interface{}) string {
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
