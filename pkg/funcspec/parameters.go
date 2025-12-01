package funcspec

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

// RequestParameters holds parsed parameters from headers and query string
type RequestParameters struct {
	// Field selection
	SelectFields    []string
	NotSelectFields []string
	Distinct        bool

	// Filtering
	FieldFilters  map[string]string         // column -> value (exact match)
	SearchFilters map[string]string         // column -> value (ILIKE)
	SearchOps     map[string]FilterOperator // column -> {operator, value, logic}
	CustomSQLWhere string
	CustomSQLOr    string

	// Sorting & Pagination
	SortColumns string
	Limit       int
	Offset      int

	// Advanced features
	SkipCount bool
	SkipCache bool

	// Response format
	ResponseFormat string // "simple", "detail", "syncfusion"
	ComplexAPI     bool   // true if NOT simple API
}

// FilterOperator represents a filter with operator
type FilterOperator struct {
	Operator string // eq, neq, gt, lt, gte, lte, like, ilike, in, between, etc.
	Value    string
	Logic    string // AND or OR
}

// ParseParameters parses all parameters from request headers and query string
func (h *Handler) ParseParameters(r *http.Request) *RequestParameters {
	params := &RequestParameters{
		FieldFilters:   make(map[string]string),
		SearchFilters:  make(map[string]string),
		SearchOps:      make(map[string]FilterOperator),
		Limit:          20,    // Default limit
		Offset:         0,     // Default offset
		ResponseFormat: "simple", // Default format
		ComplexAPI:     false, // Default to simple API
	}

	// Merge headers and query parameters
	combined := make(map[string]string)

	// Add all headers (normalize to lowercase)
	for key, values := range r.Header {
		if len(values) > 0 {
			combined[strings.ToLower(key)] = values[0]
		}
	}

	// Add all query parameters (override headers)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			combined[strings.ToLower(key)] = values[0]
		}
	}

	// Parse each parameter
	for key, value := range combined {
		// Decode value if base64 encoded
		decodedValue := h.decodeValue(value)

		switch {
		// Field Selection
		case strings.HasPrefix(key, "x-select-fields"):
			params.SelectFields = h.parseCommaSeparated(decodedValue)
		case strings.HasPrefix(key, "x-not-select-fields"):
			params.NotSelectFields = h.parseCommaSeparated(decodedValue)
		case strings.HasPrefix(key, "x-distinct"):
			params.Distinct = strings.EqualFold(decodedValue, "true")

		// Filtering
		case strings.HasPrefix(key, "x-fieldfilter-"):
			colName := strings.TrimPrefix(key, "x-fieldfilter-")
			params.FieldFilters[colName] = decodedValue
		case strings.HasPrefix(key, "x-searchfilter-"):
			colName := strings.TrimPrefix(key, "x-searchfilter-")
			params.SearchFilters[colName] = decodedValue
		case strings.HasPrefix(key, "x-searchop-"):
			h.parseSearchOp(params, key, decodedValue, "AND")
		case strings.HasPrefix(key, "x-searchor-"):
			h.parseSearchOp(params, key, decodedValue, "OR")
		case strings.HasPrefix(key, "x-searchand-"):
			h.parseSearchOp(params, key, decodedValue, "AND")
		case strings.HasPrefix(key, "x-custom-sql-w"):
			if params.CustomSQLWhere != "" {
				params.CustomSQLWhere = fmt.Sprintf("%s AND (%s)", params.CustomSQLWhere, decodedValue)
			} else {
				params.CustomSQLWhere = decodedValue
			}
		case strings.HasPrefix(key, "x-custom-sql-or"):
			if params.CustomSQLOr != "" {
				params.CustomSQLOr = fmt.Sprintf("%s OR (%s)", params.CustomSQLOr, decodedValue)
			} else {
				params.CustomSQLOr = decodedValue
			}

		// Sorting & Pagination
		case key == "sort" || strings.HasPrefix(key, "x-sort"):
			params.SortColumns = decodedValue
		case strings.HasPrefix(key, "sort(") && strings.Contains(key, ")"):
			// Handle sort(col1,-col2) syntax
			sortValue := key[strings.Index(key, "(")+1 : strings.Index(key, ")")]
			params.SortColumns = sortValue
		case key == "limit" || strings.HasPrefix(key, "x-limit"):
			if limit, err := strconv.Atoi(decodedValue); err == nil && limit > 0 {
				params.Limit = limit
			}
		case strings.HasPrefix(key, "limit(") && strings.Contains(key, ")"):
			// Handle limit(offset,limit) or limit(limit) syntax
			limitValue := key[strings.Index(key, "(")+1 : strings.Index(key, ")")]
			parts := strings.Split(limitValue, ",")
			if len(parts) > 1 {
				if offset, err := strconv.Atoi(parts[0]); err == nil {
					params.Offset = offset
				}
				if limit, err := strconv.Atoi(parts[1]); err == nil {
					params.Limit = limit
				}
			} else {
				if limit, err := strconv.Atoi(parts[0]); err == nil {
					params.Limit = limit
				}
			}
		case key == "offset" || strings.HasPrefix(key, "x-offset"):
			if offset, err := strconv.Atoi(decodedValue); err == nil && offset >= 0 {
				params.Offset = offset
			}

		// Advanced features
		case strings.HasPrefix(key, "x-skipcount"):
			params.SkipCount = strings.EqualFold(decodedValue, "true")
		case strings.HasPrefix(key, "x-skipcache"):
			params.SkipCache = strings.EqualFold(decodedValue, "true")

		// Response Format
		case strings.HasPrefix(key, "x-simpleapi"):
			params.ResponseFormat = "simple"
			params.ComplexAPI = !(decodedValue == "1" || strings.EqualFold(decodedValue, "true"))
		case strings.HasPrefix(key, "x-detailapi"):
			params.ResponseFormat = "detail"
			params.ComplexAPI = true
		case strings.HasPrefix(key, "x-syncfusion"):
			params.ResponseFormat = "syncfusion"
			params.ComplexAPI = true
		}
	}

	return params
}

// parseSearchOp parses x-searchop-{operator}-{column} or x-searchor-{operator}-{column}
func (h *Handler) parseSearchOp(params *RequestParameters, headerKey, value, logic string) {
	var prefix string
	if logic == "OR" {
		prefix = "x-searchor-"
	} else {
		prefix = "x-searchop-"
		if strings.HasPrefix(headerKey, "x-searchand-") {
			prefix = "x-searchand-"
		}
	}

	rest := strings.TrimPrefix(headerKey, prefix)
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) != 2 {
		logger.Warn("Invalid search operator header format: %s", headerKey)
		return
	}

	operator := parts[0]
	colName := parts[1]

	params.SearchOps[colName] = FilterOperator{
		Operator: operator,
		Value:    value,
		Logic:    logic,
	}

	logger.Debug("%s search operator: %s %s %s", logic, colName, operator, value)
}

// decodeValue decodes base64 encoded values (ZIP_ or __ prefix)
func (h *Handler) decodeValue(value string) string {
	decoded, _ := restheadspec.DecodeParam(value)
	return decoded
}

// parseCommaSeparated parses comma-separated values
func (h *Handler) parseCommaSeparated(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// ApplyFieldSelection applies column selection to SQL query
func (h *Handler) ApplyFieldSelection(sqlQuery string, params *RequestParameters) string {
	if len(params.SelectFields) == 0 && len(params.NotSelectFields) == 0 {
		return sqlQuery
	}

	// This is a simplified implementation
	// A full implementation would parse the SQL and replace the SELECT clause
	// For now, we log a warning that this feature needs manual implementation
	if len(params.SelectFields) > 0 {
		logger.Debug("Field selection requested: %v (manual SQL adjustment may be needed)", params.SelectFields)
	}
	if len(params.NotSelectFields) > 0 {
		logger.Debug("Field exclusion requested: %v (manual SQL adjustment may be needed)", params.NotSelectFields)
	}

	return sqlQuery
}

// ApplyFilters applies all filters to the SQL query
func (h *Handler) ApplyFilters(sqlQuery string, params *RequestParameters) string {
	// Apply field filters (exact match)
	for colName, value := range params.FieldFilters {
		condition := ""
		if value == "" || value == "0" {
			condition = fmt.Sprintf("COALESCE(%s, 0) = %s", ValidSQL(colName, "colname"), ValidSQL(value, "colvalue"))
		} else {
			condition = fmt.Sprintf("%s = %s", ValidSQL(colName, "colname"), ValidSQL(value, "colvalue"))
		}
		sqlQuery = sqlQryWhere(sqlQuery, condition)
		logger.Debug("Applied field filter: %s", condition)
	}

	// Apply search filters (ILIKE)
	for colName, value := range params.SearchFilters {
		sval := strings.ReplaceAll(value, "'", "")
		if sval != "" {
			condition := fmt.Sprintf("%s ILIKE '%%%s%%'", ValidSQL(colName, "colname"), ValidSQL(sval, "colvalue"))
			sqlQuery = sqlQryWhere(sqlQuery, condition)
			logger.Debug("Applied search filter: %s", condition)
		}
	}

	// Apply search operators
	for colName, filterOp := range params.SearchOps {
		condition := h.buildFilterCondition(colName, filterOp)
		if condition != "" {
			if filterOp.Logic == "OR" {
				sqlQuery = sqlQryWhereOr(sqlQuery, condition)
			} else {
				sqlQuery = sqlQryWhere(sqlQuery, condition)
			}
			logger.Debug("Applied search operator: %s", condition)
		}
	}

	// Apply custom SQL WHERE
	if params.CustomSQLWhere != "" {
		colval := ValidSQL(params.CustomSQLWhere, "select")
		if colval != "" {
			sqlQuery = sqlQryWhere(sqlQuery, colval)
			logger.Debug("Applied custom SQL WHERE: %s", colval)
		}
	}

	// Apply custom SQL OR
	if params.CustomSQLOr != "" {
		colval := ValidSQL(params.CustomSQLOr, "select")
		if colval != "" {
			sqlQuery = sqlQryWhereOr(sqlQuery, colval)
			logger.Debug("Applied custom SQL OR: %s", colval)
		}
	}

	return sqlQuery
}

// buildFilterCondition builds a SQL condition from a FilterOperator
func (h *Handler) buildFilterCondition(colName string, op FilterOperator) string {
	safCol := ValidSQL(colName, "colname")
	operator := strings.ToLower(op.Operator)
	value := op.Value

	switch operator {
	case "contains", "contain", "like":
		return fmt.Sprintf("%s ILIKE '%%%s%%'", safCol, ValidSQL(value, "colvalue"))
	case "beginswith", "startswith":
		return fmt.Sprintf("%s ILIKE '%s%%'", safCol, ValidSQL(value, "colvalue"))
	case "endswith":
		return fmt.Sprintf("%s ILIKE '%%%s'", safCol, ValidSQL(value, "colvalue"))
	case "equals", "eq", "=":
		if IsNumeric(value) {
			return fmt.Sprintf("%s = %s", safCol, ValidSQL(value, "colvalue"))
		}
		return fmt.Sprintf("%s = '%s'", safCol, ValidSQL(value, "colvalue"))
	case "notequals", "neq", "ne", "!=", "<>":
		if IsNumeric(value) {
			return fmt.Sprintf("%s != %s", safCol, ValidSQL(value, "colvalue"))
		}
		return fmt.Sprintf("%s != '%s'", safCol, ValidSQL(value, "colvalue"))
	case "greaterthan", "gt", ">":
		return fmt.Sprintf("%s > %s", safCol, ValidSQL(value, "colvalue"))
	case "lessthan", "lt", "<":
		return fmt.Sprintf("%s < %s", safCol, ValidSQL(value, "colvalue"))
	case "greaterthanorequal", "gte", "ge", ">=":
		return fmt.Sprintf("%s >= %s", safCol, ValidSQL(value, "colvalue"))
	case "lessthanorequal", "lte", "le", "<=":
		return fmt.Sprintf("%s <= %s", safCol, ValidSQL(value, "colvalue"))
	case "between":
		parts := strings.Split(value, ",")
		if len(parts) == 2 {
			return fmt.Sprintf("%s > %s AND %s < %s", safCol, ValidSQL(parts[0], "colvalue"), safCol, ValidSQL(parts[1], "colvalue"))
		}
	case "betweeninclusive":
		parts := strings.Split(value, ",")
		if len(parts) == 2 {
			return fmt.Sprintf("%s >= %s AND %s <= %s", safCol, ValidSQL(parts[0], "colvalue"), safCol, ValidSQL(parts[1], "colvalue"))
		}
	case "in":
		values := strings.Split(value, ",")
		safeValues := make([]string, len(values))
		for i, v := range values {
			safeValues[i] = fmt.Sprintf("'%s'", ValidSQL(v, "colvalue"))
		}
		return fmt.Sprintf("%s IN (%s)", safCol, strings.Join(safeValues, ", "))
	case "empty", "isnull", "null":
		return fmt.Sprintf("(%s IS NULL OR %s = '')", safCol, safCol)
	case "notempty", "isnotnull", "notnull":
		return fmt.Sprintf("(%s IS NOT NULL AND %s != '')", safCol, safCol)
	default:
		logger.Warn("Unknown filter operator: %s, defaulting to equals", operator)
		return fmt.Sprintf("%s = '%s'", safCol, ValidSQL(value, "colvalue"))
	}

	return ""
}

// ApplyDistinct adds DISTINCT to SQL query if requested
func (h *Handler) ApplyDistinct(sqlQuery string, params *RequestParameters) string {
	if !params.Distinct {
		return sqlQuery
	}

	// Add DISTINCT after SELECT
	selectPos := strings.Index(strings.ToUpper(sqlQuery), "SELECT")
	if selectPos >= 0 {
		beforeSelect := sqlQuery[:selectPos+6] // "SELECT"
		afterSelect := sqlQuery[selectPos+6:]
		sqlQuery = beforeSelect + " DISTINCT" + afterSelect
		logger.Debug("Applied DISTINCT to query")
	}

	return sqlQuery
}

// sqlQryWhereOr adds a WHERE clause with OR logic
func sqlQryWhereOr(sqlquery, condition string) string {
	lowerQuery := strings.ToLower(sqlquery)
	wherePos := strings.Index(lowerQuery, " where ")
	groupPos := strings.Index(lowerQuery, " group by")
	orderPos := strings.Index(lowerQuery, " order by")
	limitPos := strings.Index(lowerQuery, " limit ")

	// Find the insertion point
	insertPos := len(sqlquery)
	if groupPos > 0 && groupPos < insertPos {
		insertPos = groupPos
	}
	if orderPos > 0 && orderPos < insertPos {
		insertPos = orderPos
	}
	if limitPos > 0 && limitPos < insertPos {
		insertPos = limitPos
	}

	if wherePos > 0 {
		// WHERE exists, add OR condition
		before := sqlquery[:insertPos]
		after := sqlquery[insertPos:]
		return fmt.Sprintf("%s OR (%s) %s", before, condition, after)
	} else {
		// No WHERE exists, add it
		before := sqlquery[:insertPos]
		after := sqlquery[insertPos:]
		return fmt.Sprintf("%s WHERE %s %s", before, condition, after)
	}
}
