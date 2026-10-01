package resolvemcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// modelInfo holds pre-computed metadata for a model used in tool descriptions.
type modelInfo struct {
	fullName      string // e.g. "public.users"
	pkName        string // e.g. "id"
	columns       []columnInfo
	relationNames []string
	schemaDoc     string // formatted multi-line schema listing
}

type columnInfo struct {
	jsonName  string
	sqlName   string
	goType    string
	sqlType   string
	isPrimary bool
	isUnique  bool
	isFK      bool
	nullable  bool
}

// buildModelInfo extracts column metadata and pre-builds the schema documentation string.
func buildModelInfo(schema, entity string, model interface{}) modelInfo {
	info := modelInfo{
		fullName: buildModelName(schema, entity),
		pkName:   reflection.GetPrimaryKeyName(model),
	}

	// Unwrap to base struct type
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice) {
		modelType = modelType.Elem()
	}
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return info
	}

	details := reflection.GetModelColumnDetail(reflect.New(modelType).Elem())

	for _, d := range details {
		// Derive the JSON name from the struct field
		jsonName := fieldJSONName(modelType, d.Name)
		if jsonName == "" || jsonName == "-" {
			continue
		}

		// Skip relation fields (slice or user-defined struct that isn't time.Time).
		fieldType, found := modelType.FieldByName(d.Name)
		var unwrappedType reflect.Type
		isSQLType := false
		if found {
			ft := fieldType.Type
			if sqlType, ok := unwrapSQLType(ft); ok {
				unwrappedType = sqlType
				ft = sqlType
				isSQLType = true
			} else if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			isUserStruct := ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.PkgPath() != ""
			if !isSQLType && (ft.Kind() == reflect.Slice || isUserStruct) {
				info.relationNames = append(info.relationNames, jsonName)
				continue
			}
		}

		sqlName := d.SQLName
		if sqlName == "" {
			sqlName = jsonName
		}

		// Derive Go type name, unwrapping pointer if needed.
		goType := d.DataType
		if isSQLType {
			goType = unwrappedType.Name()
		}
		if goType == "" && found {
			ft := fieldType.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			goType = ft.Name()
		}

		// isPrimary: use both the GORM-tag detection and a name comparison against
		// the known primary key (handles camelCase "primaryKey" tags correctly).
		isPrimary := d.SQLKey == "primary_key" ||
			(info.pkName != "" && (sqlName == info.pkName || jsonName == info.pkName))

		ci := columnInfo{
			jsonName:  jsonName,
			sqlName:   sqlName,
			goType:    goType,
			sqlType:   d.SQLDataType,
			isPrimary: isPrimary,
			isUnique:  d.SQLKey == "unique" || d.SQLKey == "uniqueindex",
			isFK:      d.SQLKey == "foreign_key",
			nullable:  isSQLType || d.Nullable,
		}
		info.columns = append(info.columns, ci)
	}

	info.schemaDoc = buildSchemaDoc(info)
	return info
}

// unwrapSQLType returns the value type wrapped by a spectypes SQL value. These
// types are scalar columns even when their Go representation is a struct or a
// slice (for example, SqlNull[string] and SqlJSONB).
func unwrapSQLType(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.PkgPath() != "github.com/bitechdev/ResolveSpec/pkg/spectypes" {
		return nil, false
	}
	if t.Kind() == reflect.Struct {
		if value, ok := t.FieldByName("Val"); ok {
			return value.Type, true
		}
	}
	return t, true
}

// fieldJSONName returns the JSON tag name for a struct field, falling back to the field name.
func fieldJSONName(modelType reflect.Type, fieldName string) string {
	field, ok := modelType.FieldByName(fieldName)
	if !ok {
		return fieldName
	}
	tag := field.Tag.Get("json")
	if tag == "" {
		return fieldName
	}
	parts := strings.SplitN(tag, ",", 2)
	if parts[0] == "" {
		return fieldName
	}
	return parts[0]
}

// buildSchemaDoc builds a human-readable column listing for inclusion in tool descriptions.
func buildSchemaDoc(info modelInfo) string {
	if len(info.columns) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Columns:\n")
	for _, c := range info.columns {
		line := fmt.Sprintf("  • %s", c.jsonName)

		typeDesc := c.goType
		if c.sqlType != "" {
			typeDesc = c.sqlType
		}
		if typeDesc != "" {
			line += fmt.Sprintf(" (%s)", typeDesc)
		}

		var flags []string
		if c.isPrimary {
			flags = append(flags, "primary key")
		}
		if c.isUnique {
			flags = append(flags, "unique")
		}
		if c.isFK {
			flags = append(flags, "foreign key")
		}
		if !c.nullable && !c.isPrimary {
			flags = append(flags, "not null")
		} else if c.nullable {
			flags = append(flags, "nullable")
		}
		if len(flags) > 0 {
			line += " — " + strings.Join(flags, ", ")
		}

		sb.WriteString(line + "\n")
	}

	if len(info.relationNames) > 0 {
		sb.WriteString("Relations (preloadable): " + strings.Join(info.relationNames, ", ") + "\n")
	}

	return sb.String()
}

// columnNameList returns a comma-separated list of JSON column names (for descriptions).
func columnNameList(cols []columnInfo) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.jsonName
	}
	return strings.Join(names, ", ")
}

// writableColumnNames returns JSON names for all non-primary-key columns.
func writableColumnNames(cols []columnInfo) []string {
	var names []string
	for _, c := range cols {
		if !c.isPrimary {
			names = append(names, c.jsonName)
		}
	}
	return names
}

// parseRequestOptions reads the paging, filter, sort, column and preload arguments shared
// by the read tools.
func parseRequestOptions(args map[string]interface{}) common.RequestOptions {
	options := common.RequestOptions{}

	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit := int(n)
			options.Limit = &limit
		case int:
			options.Limit = &n
		}
	}

	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset := int(n)
			options.Offset = &offset
		case int:
			options.Offset = &n
		}
	}

	if v, ok := args["cursor_forward"].(string); ok {
		options.CursorForward = v
	}
	if v, ok := args["cursor_backward"].(string); ok {
		options.CursorBackward = v
	}

	options.Columns = parseStringArray(args["columns"])
	options.OmitColumns = parseStringArray(args["omit_columns"])
	options.Filters = parseFilters(args["filters"])
	options.Sort = parseSortOptions(args["sort"])
	options.Preload = parsePreloadOptions(args["preloads"])

	return options
}

func parseStringArray(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func parseFilters(raw interface{}) []common.FilterOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.FilterOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var f common.FilterOption
		if err := json.Unmarshal(b, &f); err != nil {
			continue
		}
		if f.Column == "" || f.Operator == "" {
			continue
		}
		if strings.EqualFold(f.LogicOperator, "or") {
			f.LogicOperator = "OR"
		} else {
			f.LogicOperator = "AND"
		}
		result = append(result, f)
	}
	return result
}

func parseSortOptions(raw interface{}) []common.SortOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.SortOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var s common.SortOption
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		if s.Column == "" {
			continue
		}
		result = append(result, s)
	}
	return result
}

func parsePreloadOptions(raw interface{}) []common.PreloadOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.PreloadOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var p common.PreloadOption
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		if p.Relation == "" {
			continue
		}
		result = append(result, p)
	}
	return result
}

// marshalResult marshals a value to JSON and returns it as an MCP text result.
func marshalResult(v interface{}) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("error marshaling result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
