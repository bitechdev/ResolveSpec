package reflection

import (
	"reflect"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

// getColumnFieldType resolves the reflect.Type of the struct field that backs
// colName (matched by json tag, field name or snake_case), following the same
// rules as GetColumnTypeFromModel.
func getColumnFieldType(model interface{}, colName string) (reflect.Type, bool) {
	if model == nil {
		return nil, false
	}
	sourceColName := ExtractSourceColumn(colName)

	modelType := reflect.TypeOf(model)
	for modelType != nil && modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil, false
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			if name := jsonTagName(jsonTag); name == sourceColName {
				return field.Type, true
			}
		}
		if equalFold(field.Name, sourceColName) {
			return field.Type, true
		}
		if ToSnakeCase(field.Name) == sourceColName {
			return field.Type, true
		}
	}
	return nil, false
}

func jsonTagName(tag string) string {
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			return tag[:i]
		}
	}
	return tag
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// GetColumnSQLTypeName returns the canonical PostgreSQL type name for a column
// backed by a spectypes wrapper (e.g. "geometry", "vector", "jsonb"), or
// ("", false) if the column is not found or not a spectypes type.
func GetColumnSQLTypeName(model interface{}, colName string) (string, bool) {
	t, ok := getColumnFieldType(model, colName)
	if !ok {
		return "", false
	}
	return spectypes.SQLTypeName(t)
}

// IsSpatialColumn reports whether colName is backed by a PostGIS
// geometry/geography wrapper.
func IsSpatialColumn(model interface{}, colName string) bool {
	t, ok := getColumnFieldType(model, colName)
	return ok && spectypes.IsSpatialType(t)
}

// IsVectorColumn reports whether colName is backed by a pgvector wrapper.
func IsVectorColumn(model interface{}, colName string) bool {
	t, ok := getColumnFieldType(model, colName)
	return ok && spectypes.IsVectorType(t)
}
