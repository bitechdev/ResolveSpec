package reflection

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

// getColumnStructField resolves the struct field that backs colName (matched by
// json tag, field name or snake_case), following the same rules as
// GetColumnTypeFromModel.
func getColumnStructField(model interface{}, colName string) (reflect.StructField, bool) {
	if model == nil {
		return reflect.StructField{}, false
	}
	sourceColName := ExtractSourceColumn(colName)

	modelType := reflect.TypeOf(model)
	for modelType != nil && modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			if name := jsonTagName(jsonTag); name == sourceColName {
				return field, true
			}
		}
		if equalFold(field.Name, sourceColName) {
			return field, true
		}
		if ToSnakeCase(field.Name) == sourceColName {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// getColumnFieldType resolves the reflect.Type of the struct field that backs
// colName (matched by json tag, field name or snake_case), following the same
// rules as GetColumnTypeFromModel.
func getColumnFieldType(model interface{}, colName string) (reflect.Type, bool) {
	f, ok := getColumnStructField(model, colName)
	if !ok {
		return nil, false
	}
	return f.Type, true
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

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

// IsJSONColumn reports whether colName is backed by a JSON/JSONB column on the
// model. It recognises the spectypes SqlJSONB wrapper, encoding/json.RawMessage,
// map-typed fields, and fields carrying a bun/gorm `type:json` / `type:jsonb`
// tag. colName should be a bare column name (callers pass the parsed base column
// of a JSON path, not the full "col->>'x'" expression).
func IsJSONColumn(model interface{}, colName string) bool {
	f, ok := getColumnStructField(model, colName)
	if !ok {
		return false
	}

	ft := f.Type
	for ft != nil && ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft == nil {
		return false
	}

	if spectypes.IsJSONType(ft) {
		return true
	}
	if ft == rawMessageType {
		return true
	}
	if ft.Kind() == reflect.Map {
		return true
	}
	if tagDeclaresJSON(f.Tag.Get("bun")) || tagDeclaresJSON(f.Tag.Get("gorm")) {
		return true
	}
	return false
}

// tagDeclaresJSON reports whether an ORM struct tag declares a json/jsonb column
// type, e.g. `bun:"meta,type:jsonb"` or `gorm:"column:meta;type:json"`.
func tagDeclaresJSON(tag string) bool {
	if tag == "" {
		return false
	}
	for _, part := range strings.FieldsFunc(tag, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	}) {
		value, found := strings.CutPrefix(strings.TrimSpace(part), "type:")
		if !found {
			continue
		}
		value = strings.ToLower(strings.TrimSpace(value))
		// Match "json" and "jsonb", including parametrised forms just in case.
		if value == "json" || value == "jsonb" ||
			strings.HasPrefix(value, "json(") || strings.HasPrefix(value, "jsonb(") {
			return true
		}
	}
	return false
}
