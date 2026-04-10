package database

import (
	"reflect"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/metrics"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

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
		return operation, "", "unknown", "unknown"
	}

	schema, table = parseTableName(tableRef, driverName)
	entity = cleanMetricIdentifier(table)
	return operation, schema, entity, table
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
