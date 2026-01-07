package reflection

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

type PrimaryKeyNameProvider interface {
	GetIDName() string
}

// GetPrimaryKeyName extracts the primary key column name from a model
// It first checks if the model implements PrimaryKeyNameProvider (GetIDName method)
// Falls back to reflection to find bun:",pk" tag, then gorm:"primaryKey" tag
func GetPrimaryKeyName(model any) string {
	if reflect.TypeOf(model) == nil {
		return ""
	}
	// If we are given a string model name, look up the model
	if reflect.TypeOf(model).Kind() == reflect.String {
		name := model.(string)
		m, err := modelregistry.GetModelByName(name)
		if err == nil {
			model = m
		}
	}

	// Check if model implements PrimaryKeyNameProvider
	if provider, ok := model.(PrimaryKeyNameProvider); ok {
		return provider.GetIDName()
	}

	// Try Bun tag first
	if pkName := getPrimaryKeyFromReflection(model, "bun"); pkName != "" {
		return pkName
	}

	// Fall back to GORM tag
	if pkName := getPrimaryKeyFromReflection(model, "gorm"); pkName != "" {
		return pkName
	}

	return ""
}

// GetPrimaryKeyValue extracts the primary key value from a model instance
// Returns the value of the primary key field
func GetPrimaryKeyValue(model any) any {
	if model == nil || reflect.TypeOf(model) == nil {
		return nil
	}

	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil
	}

	// Try Bun tag first
	if pkValue := findPrimaryKeyValue(val, "bun"); pkValue != nil {
		return pkValue
	}

	// Fall back to GORM tag
	if pkValue := findPrimaryKeyValue(val, "gorm"); pkValue != nil {
		return pkValue
	}

	// Last resort: look for field named "ID" or "Id"
	if pkValue := findFieldByName(val, "id"); pkValue != nil {
		return pkValue
	}

	return nil
}

// findPrimaryKeyValue recursively searches for a primary key field in the struct
func findPrimaryKeyValue(val reflect.Value, ormType string) any {
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			// Recursively search in embedded struct
			if pkValue := findPrimaryKeyValue(fieldValue, ormType); pkValue != nil {
				return pkValue
			}
			continue
		}

		// Check for primary key tag
		switch ormType {
		case "bun":
			bunTag := field.Tag.Get("bun")
			if strings.Contains(bunTag, "pk") && fieldValue.CanInterface() {
				return fieldValue.Interface()
			}
		case "gorm":
			gormTag := field.Tag.Get("gorm")
			if strings.Contains(gormTag, "primaryKey") && fieldValue.CanInterface() {
				return fieldValue.Interface()
			}
		}
	}

	return nil
}

// findFieldByName recursively searches for a field by name in the struct
func findFieldByName(val reflect.Value, name string) any {
	typ := val.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			// Recursively search in embedded struct
			if result := findFieldByName(fieldValue, name); result != nil {
				return result
			}
			continue
		}

		// Check if field name matches
		if strings.EqualFold(field.Name, name) && fieldValue.CanInterface() {
			return fieldValue.Interface()
		}
	}

	return nil
}

// GetModelColumns extracts all column names from a model using reflection
// It checks bun tags first, then gorm tags, then json tags, and finally falls back to lowercase field names
// This function recursively processes embedded structs to include their fields
func GetModelColumns(model any) []string {
	var columns []string

	modelType := reflect.TypeOf(model)

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType != nil && (modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return columns
	}

	collectColumnsFromType(modelType, &columns)

	return columns
}

// collectColumnsFromType recursively collects column names from a struct type and its embedded fields
func collectColumnsFromType(typ reflect.Type, columns *[]string) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous {
			// Unwrap pointer type if necessary
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}

			// Recursively process embedded struct
			if fieldType.Kind() == reflect.Struct {
				collectColumnsFromType(fieldType, columns)
				continue
			}
		}

		// Get column name using the same logic as primary key extraction
		columnName := getColumnNameFromField(field)

		if columnName != "" {
			*columns = append(*columns, columnName)
		}
	}
}

// getColumnNameFromField extracts the column name from a struct field
// Priority: bun tag -> gorm tag -> json tag -> lowercase field name
func getColumnNameFromField(field reflect.StructField) string {
	// Try bun tag first
	bunTag := field.Tag.Get("bun")
	if bunTag != "" && bunTag != "-" {
		if colName := ExtractColumnFromBunTag(bunTag); colName != "" {
			return colName
		}
	}

	// Try gorm tag
	gormTag := field.Tag.Get("gorm")
	if gormTag != "" && gormTag != "-" {
		if colName := ExtractColumnFromGormTag(gormTag); colName != "" {
			return colName
		}
	}

	// Fall back to json tag
	jsonTag := field.Tag.Get("json")
	if jsonTag != "" && jsonTag != "-" {
		// Extract just the field name before any options
		parts := strings.Split(jsonTag, ",")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}

	// Last resort: use field name in lowercase
	return strings.ToLower(field.Name)
}

// getPrimaryKeyFromReflection uses reflection to find the primary key field
// This function recursively searches embedded structs
func getPrimaryKeyFromReflection(model any, ormType string) string {
	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return ""
	}

	typ := val.Type()
	return findPrimaryKeyNameFromType(typ, ormType)
}

// findPrimaryKeyNameFromType recursively searches for the primary key field name in a struct type
func findPrimaryKeyNameFromType(typ reflect.Type, ormType string) string {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous {
			// Unwrap pointer type if necessary
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}

			// Recursively search in embedded struct
			if fieldType.Kind() == reflect.Struct {
				if pkName := findPrimaryKeyNameFromType(fieldType, ormType); pkName != "" {
					return pkName
				}
			}
			continue
		}

		switch ormType {
		case "gorm":
			// Check for gorm tag with primaryKey
			gormTag := field.Tag.Get("gorm")
			if strings.Contains(gormTag, "primaryKey") {
				// Try to extract column name from gorm tag
				if colName := ExtractColumnFromGormTag(gormTag); colName != "" {
					return colName
				}
				// Fall back to json tag
				if jsonTag := field.Tag.Get("json"); jsonTag != "" {
					return strings.Split(jsonTag, ",")[0]
				}
			}
		case "bun":
			// Check for bun tag with pk flag
			bunTag := field.Tag.Get("bun")
			if strings.Contains(bunTag, "pk") {
				// Extract column name from bun tag
				if colName := ExtractColumnFromBunTag(bunTag); colName != "" {
					return colName
				}
				// Fall back to json tag
				if jsonTag := field.Tag.Get("json"); jsonTag != "" {
					return strings.Split(jsonTag, ",")[0]
				}
			}
		}
	}

	return ""
}

// ExtractColumnFromGormTag extracts the column name from a gorm tag
// Example: "column:id;primaryKey" -> "id"
func ExtractColumnFromGormTag(tag string) string {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if colName, found := strings.CutPrefix(part, "column:"); found {
			return colName
		}
	}
	return ""
}

// ExtractColumnFromBunTag extracts the column name from a bun tag
// Example: "id,pk" -> "id"
// Example: ",pk" -> "" (will fall back to json tag)
func ExtractColumnFromBunTag(tag string) string {
	parts := strings.Split(tag, ",")
	if strings.HasPrefix(strings.ToLower(tag), "table:") || strings.HasPrefix(strings.ToLower(tag), "rel:") || strings.HasPrefix(strings.ToLower(tag), "join:") {
		return ""
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// GetSQLModelColumns extracts column names that have valid SQL field mappings
// This function only returns columns that:
// 1. Have bun or gorm tags (not just json tags)
// 2. Are not relations (no rel:, join:, foreignKey, references, many2many tags)
// 3. Are not scan-only embedded fields
func GetSQLModelColumns(model any) []string {
	var columns []string

	modelType := reflect.TypeOf(model)

	// Unwrap pointers, slices, and arrays to get to the base struct type
	for modelType != nil && (modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return columns
	}

	collectSQLColumnsFromType(modelType, &columns, false)

	return columns
}

// collectSQLColumnsFromType recursively collects SQL column names from a struct type
// scanOnlyEmbedded indicates if we're inside a scan-only embedded struct
func collectSQLColumnsFromType(typ reflect.Type, columns *[]string, scanOnlyEmbedded bool) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous {
			// Unwrap pointer type if necessary
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}

			// Check if the embedded struct itself is scan-only
			isScanOnly := scanOnlyEmbedded
			bunTag := field.Tag.Get("bun")
			if bunTag != "" && isBunFieldScanOnly(bunTag) {
				isScanOnly = true
			}

			// Recursively process embedded struct
			if fieldType.Kind() == reflect.Struct {
				collectSQLColumnsFromType(fieldType, columns, isScanOnly)
				continue
			}
		}

		// Skip fields in scan-only embedded structs
		if scanOnlyEmbedded {
			continue
		}

		// Get bun and gorm tags
		bunTag := field.Tag.Get("bun")
		gormTag := field.Tag.Get("gorm")

		// Skip if neither bun nor gorm tag exists
		if bunTag == "" && gormTag == "" {
			continue
		}

		// Skip if explicitly marked with "-"
		if bunTag == "-" || gormTag == "-" {
			continue
		}

		// Skip if field itself is scan-only (bun)
		if bunTag != "" && isBunFieldScanOnly(bunTag) {
			continue
		}

		// Skip if field itself is read-only (gorm)
		if gormTag != "" && isGormFieldReadOnly(gormTag) {
			continue
		}

		// Skip relation fields (bun)
		if bunTag != "" {
			// Skip if it's a bun relation (rel:, join:, or m2m:)
			if strings.Contains(bunTag, "rel:") ||
				strings.Contains(bunTag, "join:") ||
				strings.Contains(bunTag, "m2m:") {
				continue
			}
		}

		// Skip relation fields (gorm)
		if gormTag != "" {
			// Skip if it has gorm relationship tags
			if strings.Contains(gormTag, "foreignKey:") ||
				strings.Contains(gormTag, "references:") ||
				strings.Contains(gormTag, "many2many:") ||
				strings.Contains(gormTag, "constraint:") {
				continue
			}
		}

		// Get column name
		columnName := ""
		if bunTag != "" {
			columnName = ExtractColumnFromBunTag(bunTag)
		}
		if columnName == "" && gormTag != "" {
			columnName = ExtractColumnFromGormTag(gormTag)
		}

		// Skip if we couldn't extract a column name
		if columnName == "" {
			continue
		}

		*columns = append(*columns, columnName)
	}
}

// IsColumnWritable checks if a column can be written to in the database
// For bun: returns false if the field has "scanonly" tag
// For gorm: returns false if the field has "<-:false" or "->" (read-only) tag
// This function recursively searches embedded structs
func IsColumnWritable(model any, columnName string) bool {
	modelType := reflect.TypeOf(model)

	// Unwrap pointers to get to the base struct type
	for modelType != nil && modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}

	// Validate that we have a struct type
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return false
	}

	found, writable := isColumnWritableInType(modelType, columnName)
	if found {
		return writable
	}

	// Column not found in model, allow it (might be a dynamic column)
	return true
}

// isColumnWritableInType recursively searches for a column and checks if it's writable
// Returns (found, writable) where found indicates if the column was found
func isColumnWritableInType(typ reflect.Type, columnName string) (found bool, writable bool) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)

		// Check if this is an embedded struct
		if field.Anonymous {
			// Unwrap pointer type if necessary
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}

			// Recursively search in embedded struct
			if fieldType.Kind() == reflect.Struct {
				if found, writable := isColumnWritableInType(fieldType, columnName); found {
					return true, writable
				}
			}
			continue
		}

		// Check if this field matches the column name
		fieldColumnName := getColumnNameFromField(field)
		if fieldColumnName != columnName {
			continue
		}

		// Found the field, now check if it's writable
		// Check bun tag for scanonly
		bunTag := field.Tag.Get("bun")
		if bunTag != "" {
			if isBunFieldScanOnly(bunTag) {
				return true, false
			}
		}

		// Check gorm tag for write restrictions
		gormTag := field.Tag.Get("gorm")
		if gormTag != "" {
			if isGormFieldReadOnly(gormTag) {
				return true, false
			}
		}

		// Column is writable
		return true, true
	}

	// Column not found
	return false, false
}

// isBunFieldScanOnly checks if a bun tag indicates the field is scan-only
// Example: "column_name,scanonly" -> true
func isBunFieldScanOnly(tag string) bool {
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == "scanonly" {
			return true
		}
	}
	return false
}

// isGormFieldReadOnly checks if a gorm tag indicates the field is read-only
// Examples:
//   - "<-:false" -> true (no writes allowed)
//   - "->" -> true (read-only, common pattern)
//   - "column:name;->" -> true
//   - "<-:create" -> false (writes allowed on create)
func isGormFieldReadOnly(tag string) bool {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		// Check for read-only marker
		if part == "->" {
			return true
		}

		// Check for write restrictions
		if value, found := strings.CutPrefix(part, "<-:"); found {
			if value == "false" {
				return true
			}
		}
	}
	return false
}

// ExtractSourceColumn extracts the base column name from PostgreSQL JSON operators
// Examples:
//   - "columna->>'val'" returns "columna"
//   - "columna->'key'" returns "columna"
//   - "columna" returns "columna"
//   - "table.columna->>'val'" returns "table.columna"
func ExtractSourceColumn(colName string) string {
	// Check for PostgreSQL JSON operators: -> and ->>
	if idx := strings.Index(colName, "->>"); idx != -1 {
		return strings.TrimSpace(colName[:idx])
	}
	if idx := strings.Index(colName, "->"); idx != -1 {
		return strings.TrimSpace(colName[:idx])
	}
	return colName
}

// ToSnakeCase converts a string from CamelCase to snake_case
// Handles consecutive uppercase letters (acronyms) correctly:
// "HTTPServer" -> "http_server", "UserID" -> "user_id", "MyHTTPServer" -> "my_http_server"
func ToSnakeCase(s string) string {
	var result strings.Builder
	runes := []rune(s)

	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Add underscore if:
			// 1. Previous character is lowercase, OR
			// 2. Next character is lowercase (transition from acronym to word)
			prevIsLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if prevIsLower || nextIsLower {
				result.WriteRune('_')
			}
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// GetColumnTypeFromModel uses reflection to determine the Go type of a column in a model
func GetColumnTypeFromModel(model interface{}, colName string) reflect.Kind {
	if model == nil {
		return reflect.Invalid
	}

	// Extract the source column name (remove JSON operators like ->> or ->)
	sourceColName := ExtractSourceColumn(colName)

	modelType := reflect.TypeOf(model)
	// Dereference pointer if needed
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// Ensure it's a struct
	if modelType.Kind() != reflect.Struct {
		return reflect.Invalid
	}

	// Find the field by JSON tag or field name
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// Check JSON tag
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" {
			// Parse JSON tag (format: "name,omitempty")
			parts := strings.Split(jsonTag, ",")
			if parts[0] == sourceColName {
				return field.Type.Kind()
			}
		}

		// Check field name (case-insensitive)
		if strings.EqualFold(field.Name, sourceColName) {
			return field.Type.Kind()
		}

		// Check snake_case conversion
		snakeCaseName := ToSnakeCase(field.Name)
		if snakeCaseName == sourceColName {
			return field.Type.Kind()
		}
	}

	return reflect.Invalid
}

// IsNumericType checks if a reflect.Kind is a numeric type
func IsNumericType(kind reflect.Kind) bool {
	return kind == reflect.Int || kind == reflect.Int8 || kind == reflect.Int16 ||
		kind == reflect.Int32 || kind == reflect.Int64 || kind == reflect.Uint ||
		kind == reflect.Uint8 || kind == reflect.Uint16 || kind == reflect.Uint32 ||
		kind == reflect.Uint64 || kind == reflect.Float32 || kind == reflect.Float64
}

// IsStringType checks if a reflect.Kind is a string type
func IsStringType(kind reflect.Kind) bool {
	return kind == reflect.String
}

// IsNumericValue checks if a string value can be parsed as a number
func IsNumericValue(value string) bool {
	value = strings.TrimSpace(value)
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

// ConvertToNumericType converts a string value to the appropriate numeric type
func ConvertToNumericType(value string, kind reflect.Kind) (interface{}, error) {
	value = strings.TrimSpace(value)

	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Parse as integer
		bitSize := 64
		switch kind {
		case reflect.Int8:
			bitSize = 8
		case reflect.Int16:
			bitSize = 16
		case reflect.Int32:
			bitSize = 32
		}

		intVal, err := strconv.ParseInt(value, 10, bitSize)
		if err != nil {
			return nil, fmt.Errorf("invalid integer value: %w", err)
		}

		// Return the appropriate type
		switch kind {
		case reflect.Int:
			return int(intVal), nil
		case reflect.Int8:
			return int8(intVal), nil
		case reflect.Int16:
			return int16(intVal), nil
		case reflect.Int32:
			return int32(intVal), nil
		case reflect.Int64:
			return intVal, nil
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Parse as unsigned integer
		bitSize := 64
		switch kind {
		case reflect.Uint8:
			bitSize = 8
		case reflect.Uint16:
			bitSize = 16
		case reflect.Uint32:
			bitSize = 32
		}

		uintVal, err := strconv.ParseUint(value, 10, bitSize)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned integer value: %w", err)
		}

		// Return the appropriate type
		switch kind {
		case reflect.Uint:
			return uint(uintVal), nil
		case reflect.Uint8:
			return uint8(uintVal), nil
		case reflect.Uint16:
			return uint16(uintVal), nil
		case reflect.Uint32:
			return uint32(uintVal), nil
		case reflect.Uint64:
			return uintVal, nil
		}

	case reflect.Float32, reflect.Float64:
		// Parse as float
		bitSize := 64
		if kind == reflect.Float32 {
			bitSize = 32
		}

		floatVal, err := strconv.ParseFloat(value, bitSize)
		if err != nil {
			return nil, fmt.Errorf("invalid float value: %w", err)
		}

		if kind == reflect.Float32 {
			return float32(floatVal), nil
		}
		return floatVal, nil
	}

	return nil, fmt.Errorf("unsupported numeric type: %v", kind)
}

// RelationType represents the type of database relationship
type RelationType string

const (
	RelationHasMany    RelationType = "has-many"     // 1:N - use separate query
	RelationBelongsTo  RelationType = "belongs-to"   // N:1 - use JOIN
	RelationHasOne     RelationType = "has-one"      // 1:1 - use JOIN
	RelationManyToMany RelationType = "many-to-many" // M:N - use separate query
	RelationUnknown    RelationType = "unknown"
)

// ShouldUseJoin returns true if the relation type should use a JOIN instead of separate query
func (rt RelationType) ShouldUseJoin() bool {
	return rt == RelationBelongsTo || rt == RelationHasOne
}

// GetRelationType inspects the model's struct tags to determine the relationship type
// It checks both Bun and GORM tags to identify the relationship cardinality
func GetRelationType(model interface{}, fieldName string) RelationType {
	if model == nil || fieldName == "" {
		return RelationUnknown
	}

	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return RelationUnknown
	}

	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return RelationUnknown
	}

	// Find the field
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// Check if field name matches (case-insensitive)
		if !strings.EqualFold(field.Name, fieldName) {
			continue
		}

		// Check Bun tags first
		bunTag := field.Tag.Get("bun")
		if bunTag != "" && strings.Contains(bunTag, "rel:") {
			// Parse bun relation tag: rel:has-many, rel:belongs-to, rel:has-one, rel:many-to-many
			parts := strings.Split(bunTag, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "rel:") {
					relType := strings.TrimPrefix(part, "rel:")
					switch relType {
					case "has-many":
						return RelationHasMany
					case "belongs-to":
						return RelationBelongsTo
					case "has-one":
						return RelationHasOne
					case "many-to-many", "m2m":
						return RelationManyToMany
					}
				}
			}
		}

		// Check GORM tags
		gormTag := field.Tag.Get("gorm")
		if gormTag != "" {
			// GORM uses different patterns:
			// - foreignKey: usually indicates belongs-to or has-one
			// - many2many: indicates many-to-many
			// - Field type (slice vs pointer) helps determine cardinality

			if strings.Contains(gormTag, "many2many:") {
				return RelationManyToMany
			}

			// Check field type for cardinality hints
			fieldType := field.Type
			if fieldType.Kind() == reflect.Slice {
				// Slice indicates has-many or many-to-many
				return RelationHasMany
			}
			if fieldType.Kind() == reflect.Ptr {
				// Pointer to single struct usually indicates belongs-to or has-one
				// Check if it has foreignKey (belongs-to) or references (has-one)
				if strings.Contains(gormTag, "foreignKey:") {
					return RelationBelongsTo
				}
				return RelationHasOne
			}
		}

		// Fall back to field type inference
		fieldType := field.Type
		if fieldType.Kind() == reflect.Slice {
			// Slice of structs → has-many
			return RelationHasMany
		}
		if fieldType.Kind() == reflect.Ptr || fieldType.Kind() == reflect.Struct {
			// Single struct → belongs-to (default assumption for safety)
			// Using belongs-to as default ensures we use JOIN, which is safer
			return RelationBelongsTo
		}
	}

	return RelationUnknown
}

// GetRelationModel gets the model type for a relation field
// It searches for the field by name in the following order (case-insensitive):
// 1. Actual field name
// 2. Bun tag name (if exists)
// 3. Gorm tag name (if exists)
// 4. JSON tag name (if exists)
//
// Supports recursive field paths using dot notation (e.g., "MAL.MAL.DEF")
// For nested fields, it traverses through each level of the struct hierarchy
func GetRelationModel(model interface{}, fieldName string) interface{} {
	if model == nil || fieldName == "" {
		return nil
	}

	// Split the field name by "." to handle nested/recursive relations
	fieldParts := strings.Split(fieldName, ".")

	// Start with the current model
	currentModel := model

	// Traverse through each level of the field path
	for _, part := range fieldParts {
		if part == "" {
			continue
		}

		currentModel = getRelationModelSingleLevel(currentModel, part)
		if currentModel == nil {
			return nil
		}
	}

	return currentModel
}

// MapToStruct populates a struct from a map while preserving custom types
// It uses reflection to set struct fields based on map keys, matching by:
// 1. Bun tag column name
// 2. Gorm tag column name
// 3. JSON tag name
// 4. Field name (case-insensitive)
// This preserves custom types that implement driver.Valuer like SqlJSONB
func MapToStruct(dataMap map[string]interface{}, target interface{}) error {
	if dataMap == nil || target == nil {
		return fmt.Errorf("dataMap and target cannot be nil")
	}

	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return fmt.Errorf("target must be a pointer to a struct")
	}

	targetValue = targetValue.Elem()
	if targetValue.Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to a struct")
	}

	targetType := targetValue.Type()

	// Create a map of column names to field indices for faster lookup
	columnToField := make(map[string]int)
	for i := 0; i < targetType.NumField(); i++ {
		field := targetType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Build list of possible column names for this field
		var columnNames []string

		// 1. Bun tag
		if bunTag := field.Tag.Get("bun"); bunTag != "" && bunTag != "-" {
			if colName := ExtractColumnFromBunTag(bunTag); colName != "" {
				columnNames = append(columnNames, colName)
			}
		}

		// 2. Gorm tag
		if gormTag := field.Tag.Get("gorm"); gormTag != "" && gormTag != "-" {
			if colName := ExtractColumnFromGormTag(gormTag); colName != "" {
				columnNames = append(columnNames, colName)
			}
		}

		// 3. JSON tag
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if len(parts) > 0 && parts[0] != "" {
				columnNames = append(columnNames, parts[0])
			}
		}

		// 4. Field name variations
		columnNames = append(columnNames, field.Name)
		columnNames = append(columnNames, strings.ToLower(field.Name))
		//columnNames = append(columnNames, ToSnakeCase(field.Name))

		// Map all column name variations to this field index
		for _, colName := range columnNames {
			columnToField[strings.ToLower(colName)] = i
		}
	}

	// Iterate through the map and set struct fields
	for key, value := range dataMap {
		// Find the field index for this key
		fieldIndex, found := columnToField[strings.ToLower(key)]
		if !found {
			// Skip keys that don't map to any field
			continue
		}

		field := targetValue.Field(fieldIndex)
		if !field.CanSet() {
			continue
		}

		// Set the value, preserving custom types
		if err := setFieldValue(field, value); err != nil {
			return fmt.Errorf("failed to set field %s: %w", targetType.Field(fieldIndex).Name, err)
		}
	}

	return nil
}

// setFieldValue sets a reflect.Value from an interface{} value, handling type conversions
func setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		// Set zero value for nil
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	valueReflect := reflect.ValueOf(value)

	// If types match exactly, just set it
	if valueReflect.Type().AssignableTo(field.Type()) {
		field.Set(valueReflect)
		return nil
	}

	// Handle pointer fields
	if field.Kind() == reflect.Ptr {
		if valueReflect.Kind() != reflect.Ptr {
			// Create a new pointer and set its value
			newPtr := reflect.New(field.Type().Elem())
			if err := setFieldValue(newPtr.Elem(), value); err != nil {
				return err
			}
			field.Set(newPtr)
			return nil
		}
	}

	// Handle conversions for basic types
	switch field.Kind() {
	case reflect.String:
		if str, ok := value.(string); ok {
			field.SetString(str)
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if num, ok := convertToInt64(value); ok {
			if field.OverflowInt(num) {
				return fmt.Errorf("integer overflow")
			}
			field.SetInt(num)
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if num, ok := convertToUint64(value); ok {
			if field.OverflowUint(num) {
				return fmt.Errorf("unsigned integer overflow")
			}
			field.SetUint(num)
			return nil
		}
	case reflect.Float32, reflect.Float64:
		if num, ok := convertToFloat64(value); ok {
			if field.OverflowFloat(num) {
				return fmt.Errorf("float overflow")
			}
			field.SetFloat(num)
			return nil
		}
	case reflect.Bool:
		if b, ok := value.(bool); ok {
			field.SetBool(b)
			return nil
		}
	case reflect.Slice:
		// Handle []byte specially (for types like SqlJSONB)
		if field.Type().Elem().Kind() == reflect.Uint8 {
			switch v := value.(type) {
			case []byte:
				field.SetBytes(v)
				return nil
			case string:
				field.SetBytes([]byte(v))
				return nil
			case map[string]interface{}, []interface{}, []*any, map[string]*any:
				// Marshal complex types to JSON for SqlJSONB fields
				jsonBytes, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("failed to marshal value to JSON: %w", err)
				}
				field.SetBytes(jsonBytes)
				return nil
			}
		}

		// Handle slice-to-slice conversions (e.g., []interface{} to []*SomeModel)
		if valueReflect.Kind() == reflect.Slice {
			return convertSlice(field, valueReflect)
		}
	}

	// Handle struct types (like SqlTimeStamp, SqlDate, SqlTime which wrap SqlNull[time.Time])
	if field.Kind() == reflect.Struct {

		// Handle datatypes.SqlNull[T] and wrapped types (SqlTimeStamp, SqlDate, SqlTime)
		// Check if the type has a Scan method (sql.Scanner interface)
		if field.CanAddr() {
			scanMethod := field.Addr().MethodByName("Scan")
			if scanMethod.IsValid() {
				// Call the Scan method with the value
				results := scanMethod.Call([]reflect.Value{reflect.ValueOf(value)})
				if len(results) > 0 {
					// Check if there was an error
					if err, ok := results[0].Interface().(error); ok && err != nil {
						return err
					}
					return nil
				}
			}
		}

		// Handle time.Time with ISO string fallback
		if field.Type() == reflect.TypeOf(time.Time{}) {
			switch v := value.(type) {
			case time.Time:
				field.Set(reflect.ValueOf(v))
				return nil
			case string:
				// Try parsing as ISO 8601 / RFC3339
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					field.Set(reflect.ValueOf(t))
					return nil
				}
				// Try other common formats
				formats := []string{
					"2006-01-02T15:04:05.000-0700",
					"2006-01-02T15:04:05.000",
					"2006-01-02T15:04:05",
					"2006-01-02 15:04:05",
					"2006-01-02",
				}
				for _, format := range formats {
					if t, err := time.Parse(format, v); err == nil {
						field.Set(reflect.ValueOf(t))
						return nil
					}
				}
				return fmt.Errorf("cannot parse time string: %s", v)
			}
		}

		// Fallback: Try to find a "Val" field (for SqlNull types) and set it directly
		valField := field.FieldByName("Val")
		if valField.IsValid() && valField.CanSet() {
			// Also set Valid field to true
			validField := field.FieldByName("Valid")
			if validField.IsValid() && validField.CanSet() && validField.Kind() == reflect.Bool {
				// Set the Val field
				if err := setFieldValue(valField, value); err != nil {
					return err
				}
				// Set Valid to true
				validField.SetBool(true)
				return nil
			}
		}

	}

	// If we can convert the type, do it
	if valueReflect.Type().ConvertibleTo(field.Type()) {
		field.Set(valueReflect.Convert(field.Type()))
		return nil
	}

	return fmt.Errorf("cannot convert %v to %v", valueReflect.Type(), field.Type())
}

// convertSlice converts a source slice to a target slice type, handling element-wise conversions
// Supports converting []interface{} to slices of structs or pointers to structs
func convertSlice(targetSlice reflect.Value, sourceSlice reflect.Value) error {
	if sourceSlice.Kind() != reflect.Slice || targetSlice.Kind() != reflect.Slice {
		return fmt.Errorf("both source and target must be slices")
	}

	// Get the element type of the target slice
	targetElemType := targetSlice.Type().Elem()
	sourceLen := sourceSlice.Len()

	// Create a new slice with the same length as the source
	newSlice := reflect.MakeSlice(targetSlice.Type(), sourceLen, sourceLen)

	// Convert each element
	for i := 0; i < sourceLen; i++ {
		sourceElem := sourceSlice.Index(i)
		targetElem := newSlice.Index(i)

		// Get the actual value from the source element
		var sourceValue interface{}
		if sourceElem.CanInterface() {
			sourceValue = sourceElem.Interface()
		} else {
			continue
		}

		// Handle nil elements
		if sourceValue == nil {
			// For pointer types, nil is valid
			if targetElemType.Kind() == reflect.Ptr {
				targetElem.Set(reflect.Zero(targetElemType))
			}
			continue
		}

		// If target element type is a pointer to struct, we need to create new instances
		if targetElemType.Kind() == reflect.Ptr {
			// Create a new instance of the pointed-to type
			newElemPtr := reflect.New(targetElemType.Elem())

			// Convert the source value to the struct
			switch sv := sourceValue.(type) {
			case map[string]interface{}:
				// Source is a map, use MapToStruct to populate the new instance
				if err := MapToStruct(sv, newElemPtr.Interface()); err != nil {
					return fmt.Errorf("failed to convert element %d: %w", i, err)
				}
			default:
				// Try direct conversion or setFieldValue
				if err := setFieldValue(newElemPtr.Elem(), sourceValue); err != nil {
					return fmt.Errorf("failed to convert element %d: %w", i, err)
				}
			}

			targetElem.Set(newElemPtr)
		} else if targetElemType.Kind() == reflect.Struct {
			// Target element is a struct (not a pointer)
			switch sv := sourceValue.(type) {
			case map[string]interface{}:
				// Use MapToStruct to populate the element
				elemPtr := targetElem.Addr()
				if elemPtr.CanInterface() {
					if err := MapToStruct(sv, elemPtr.Interface()); err != nil {
						return fmt.Errorf("failed to convert element %d: %w", i, err)
					}
				}
			default:
				// Try direct conversion
				if err := setFieldValue(targetElem, sourceValue); err != nil {
					return fmt.Errorf("failed to convert element %d: %w", i, err)
				}
			}
		} else {
			// For other types, use setFieldValue
			if err := setFieldValue(targetElem, sourceValue); err != nil {
				return fmt.Errorf("failed to convert element %d: %w", i, err)
			}
		}
	}

	// Set the converted slice to the target field
	targetSlice.Set(newSlice)
	return nil
}

// convertToInt64 attempts to convert various types to int64
func convertToInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case float32:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			return num, true
		}
	}
	return 0, false
}

// convertToUint64 attempts to convert various types to uint64
func convertToUint64(value interface{}) (uint64, bool) {
	switch v := value.(type) {
	case int:
		return uint64(v), true
	case int8:
		return uint64(v), true
	case int16:
		return uint64(v), true
	case int32:
		return uint64(v), true
	case int64:
		return uint64(v), true
	case uint:
		return uint64(v), true
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case float32:
		return uint64(v), true
	case float64:
		return uint64(v), true
	case string:
		if num, err := strconv.ParseUint(v, 10, 64); err == nil {
			return num, true
		}
	}
	return 0, false
}

// convertToFloat64 attempts to convert various types to float64
func convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		if num, err := strconv.ParseFloat(v, 64); err == nil {
			return num, true
		}
	}
	return 0, false
}

// getRelationModelSingleLevel gets the model type for a single level field (non-recursive)
// This is a helper function used by GetRelationModel to handle one level at a time
func getRelationModelSingleLevel(model interface{}, fieldName string) interface{} {
	if model == nil || fieldName == "" {
		return nil
	}

	modelType := reflect.TypeOf(model)
	if modelType == nil {
		return nil
	}

	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil
	}

	// Find the field by checking in priority order (case-insensitive)
	var field *reflect.StructField
	normalizedFieldName := strings.ToLower(fieldName)

	for i := 0; i < modelType.NumField(); i++ {
		f := modelType.Field(i)

		// 1. Check actual field name (case-insensitive)
		if strings.EqualFold(f.Name, fieldName) {
			field = &f
			break
		}

		// 2. Check bun tag name
		bunTag := f.Tag.Get("bun")
		if bunTag != "" {
			bunColName := ExtractColumnFromBunTag(bunTag)
			if bunColName != "" && strings.EqualFold(bunColName, normalizedFieldName) {
				field = &f
				break
			}
		}

		// 3. Check gorm tag name
		gormTag := f.Tag.Get("gorm")
		if gormTag != "" {
			gormColName := ExtractColumnFromGormTag(gormTag)
			if gormColName != "" && strings.EqualFold(gormColName, normalizedFieldName) {
				field = &f
				break
			}
		}

		// 4. Check JSON tag name
		jsonTag := f.Tag.Get("json")
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if len(parts) > 0 && parts[0] != "" && parts[0] != "-" {
				if strings.EqualFold(parts[0], normalizedFieldName) {
					field = &f
					break
				}
			}
		}
	}

	if field == nil {
		return nil
	}

	// Get the target type
	targetType := field.Type
	if targetType == nil {
		return nil
	}

	if targetType.Kind() == reflect.Slice {
		targetType = targetType.Elem()
		if targetType == nil {
			return nil
		}
	}
	if targetType.Kind() == reflect.Ptr {
		targetType = targetType.Elem()
		if targetType == nil {
			return nil
		}
	}

	if targetType.Kind() != reflect.Struct {
		return nil
	}

	// Create a zero value of the target type
	return reflect.New(targetType).Elem().Interface()
}
