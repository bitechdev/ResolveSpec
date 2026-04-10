package database

import (
	"reflect"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/metrics"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

const maxMetricFallbackEntityLength = 120

func recordQueryMetrics(enabled bool, operation, schema, entity, table string, startedAt time.Time, err error) {
	if !enabled {
		return
	}

	metrics.GetProvider().RecordDBQuery(
		normalizeMetricOperation(operation),
		normalizeMetricSchema(schema),
		normalizeMetricEntity(entity, table),
		normalizeMetricTable(table),
		time.Since(startedAt),
		err,
	)
}

func normalizeMetricOperation(operation string) string {
	operation = strings.ToUpper(strings.TrimSpace(operation))
	if operation == "" {
		return "UNKNOWN"
	}
	return operation
}

func normalizeMetricSchema(schema string) string {
	schema = cleanMetricIdentifier(schema)
	if schema == "" {
		return "default"
	}
	return schema
}

func normalizeMetricEntity(entity, table string) string {
	entity = cleanMetricIdentifier(entity)
	if entity != "" {
		return entity
	}

	table = cleanMetricIdentifier(table)
	if table != "" {
		return table
	}

	return "unknown"
}

func normalizeMetricTable(table string) string {
	table = cleanMetricIdentifier(table)
	if table == "" {
		return "unknown"
	}
	return table
}

func entityNameFromModel(model interface{}, table string) string {
	if model == nil {
		return cleanMetricIdentifier(table)
	}

	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil {
		return cleanMetricIdentifier(table)
	}

	if modelType.Kind() == reflect.Struct && modelType.Name() != "" {
		return reflection.ToSnakeCase(modelType.Name())
	}

	return cleanMetricIdentifier(table)
}

func schemaAndTableFromModel(model interface{}, driverName string) (schema, table string) {
	provider, ok := tableNameProviderFromModel(model)
	if !ok {
		return "", ""
	}

	return parseTableName(provider.TableName(), driverName)
}

// tableNameProviderType is cached to avoid repeated reflection on every call.
var tableNameProviderType = reflect.TypeOf((*common.TableNameProvider)(nil)).Elem()

func tableNameProviderFromModel(model interface{}) (common.TableNameProvider, bool) {
	if model == nil {
		return nil, false
	}

	if provider, ok := model.(common.TableNameProvider); ok {
		return provider, true
	}

	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array) {
		modelType = modelType.Elem()
	}

	if modelType == nil || modelType.Kind() != reflect.Struct {
		return nil, false
	}

	// Check whether *T implements TableNameProvider before allocating.
	ptrType := reflect.PointerTo(modelType)
	if !ptrType.Implements(tableNameProviderType) && !modelType.Implements(tableNameProviderType) {
		return nil, false
	}

	modelValue := reflect.New(modelType)
	if provider, ok := modelValue.Interface().(common.TableNameProvider); ok {
		return provider, true
	}

	if provider, ok := modelValue.Elem().Interface().(common.TableNameProvider); ok {
		return provider, true
	}

	return nil, false
}

func metricTargetFromRawQuery(query, driverName string) (operation, schema, entity, table string) {
	operation = normalizeMetricOperation(firstQueryKeyword(query))
	tableRef := tableFromRawQuery(query, operation)
	if tableRef == "" {
		return operation, "", fallbackMetricEntityFromQuery(query), "unknown"
	}

	schema, table = parseTableName(tableRef, driverName)
	entity = cleanMetricIdentifier(table)
	return operation, schema, entity, table
}

func fallbackMetricEntityFromQuery(query string) string {
	query = sanitizeMetricQueryShape(query)
	if query == "" {
		return "unknown"
	}

	if len(query) > maxMetricFallbackEntityLength {
		return query[:maxMetricFallbackEntityLength-3] + "..."
	}

	return query
}

func sanitizeMetricQueryShape(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	var out strings.Builder
	for i := 0; i < len(query); {
		if query[i] == '\'' {
			out.WriteByte('?')
			i++
			for i < len(query) {
				if query[i] == '\'' {
					if i+1 < len(query) && query[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}

		if query[i] == '?' {
			out.WriteByte('?')
			i++
			continue
		}

		if query[i] == '$' && i+1 < len(query) && isASCIIDigit(query[i+1]) {
			out.WriteByte('?')
			i++
			for i < len(query) && isASCIIDigit(query[i]) {
				i++
			}
			continue
		}

		if query[i] == ':' && (i == 0 || query[i-1] != ':') && i+1 < len(query) && isIdentifierStart(query[i+1]) {
			out.WriteByte('?')
			i++
			for i < len(query) && isIdentifierPart(query[i]) {
				i++
			}
			continue
		}

		if query[i] == '@' && (i == 0 || query[i-1] != '@') && i+1 < len(query) && isIdentifierStart(query[i+1]) {
			out.WriteByte('?')
			i++
			for i < len(query) && isIdentifierPart(query[i]) {
				i++
			}
			continue
		}

		if startsNumericLiteral(query, i) {
			out.WriteByte('?')
			i++
			for i < len(query) && (isASCIIDigit(query[i]) || query[i] == '.') {
				i++
			}
			continue
		}

		out.WriteByte(query[i])
		i++
	}

	return strings.Join(strings.Fields(out.String()), " ")
}

func startsNumericLiteral(query string, idx int) bool {
	if idx >= len(query) {
		return false
	}

	start := idx
	if query[idx] == '-' {
		if idx+1 >= len(query) || !isASCIIDigit(query[idx+1]) {
			return false
		}
		start++
	}

	if !isASCIIDigit(query[start]) {
		return false
	}

	if idx > 0 && isIdentifierPart(query[idx-1]) {
		return false
	}

	if start+1 < len(query) && query[start] == '0' && (query[start+1] == 'x' || query[start+1] == 'X') {
		return false
	}

	return true
}

func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentifierStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentifierPart(ch byte) bool {
	return isIdentifierStart(ch) || isASCIIDigit(ch)
}

func firstQueryKeyword(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func tableFromRawQuery(query, operation string) string {
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return ""
	}

	switch operation {
	case "SELECT":
		return tokenAfter(tokens, "FROM")
	case "INSERT":
		return tokenAfter(tokens, "INTO")
	case "UPDATE":
		return tokenAfter(tokens, "UPDATE")
	case "DELETE":
		return tokenAfter(tokens, "FROM")
	default:
		return ""
	}
}

func tokenAfter(tokens []string, keyword string) string {
	for idx, token := range tokens {
		if strings.EqualFold(token, keyword) && idx+1 < len(tokens) {
			return cleanMetricIdentifier(tokens[idx+1])
		}
	}
	return ""
}

func tokenizeQuery(query string) []string {
	replacer := strings.NewReplacer(
		"\n", " ",
		"\t", " ",
		"(", " ",
		")", " ",
		",", " ",
	)
	return strings.Fields(replacer.Replace(query))
}

func cleanMetricIdentifier(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'`[]")
	value = strings.TrimRight(value, ";")
	return value
}
