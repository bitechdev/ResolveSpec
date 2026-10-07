package common

import (
	"regexp"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

var rePlainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MainTableAlias returns the alias the main table gets in the SELECT:
// the model's TableAlias() when provided, otherwise the bare table name.
func MainTableAlias(model interface{}, tableName string) string {
	if p, ok := model.(TableAliasProvider); ok {
		if a := p.TableAlias(); a != "" {
			return a
		}
	}
	return reflection.ExtractTableNameOnly(tableName)
}

func isModelSQLColumn(model interface{}, column string) bool {
	for _, c := range reflection.GetSQLModelColumns(model) {
		if strings.EqualFold(c, column) {
			return true
		}
	}
	return false
}

// QualifyModelColumn returns "alias"."column" when column is a plain identifier
// that exists on the model, so it stays unambiguous once joins are added.
// Anything else (expressions, JSON paths, other tables' columns) is returned unchanged.
func QualifyModelColumn(model interface{}, alias, column string) string {
	if alias == "" || model == nil || !rePlainIdent.MatchString(column) || !isModelSQLColumn(model, column) {
		return column
	}
	return QuoteIdent(alias) + "." + QuoteIdent(column)
}

// StripMainTablePrefix turns "<prefix>.<column>" into "<column>" when prefix is
// one of the main table's names/aliases and column exists on the model.
// Any other input is returned unchanged.
func StripMainTablePrefix(model interface{}, column string, prefixes ...string) string {
	idx := strings.Index(column, ".")
	if idx <= 0 || model == nil {
		return column
	}
	prefix := strings.Trim(column[:idx], `"`)
	col := strings.Trim(column[idx+1:], `"`)
	if !rePlainIdent.MatchString(prefix) || !rePlainIdent.MatchString(col) {
		return column
	}
	for _, p := range prefixes {
		if p != "" && strings.EqualFold(p, prefix) && isModelSQLColumn(model, col) {
			return col
		}
	}
	return column
}

// StripMainTablePrefixFromFilters applies StripMainTablePrefix to every filter column.
func StripMainTablePrefixFromFilters(model interface{}, filters []FilterOption, prefixes ...string) {
	for i := range filters {
		filters[i].Column = StripMainTablePrefix(model, filters[i].Column, prefixes...)
	}
}

// NormalizeMainTableFilters rewrites "<main table or alias>.<column>" filter
// columns on opts to the bare model column (on a copy, never the caller's
// slice) so the column validator keeps them.
func NormalizeMainTableFilters(model interface{}, tableName string, opts *RequestOptions) {
	opts.Filters = append([]FilterOption(nil), opts.Filters...)
	StripMainTablePrefixFromFilters(model, opts.Filters,
		MainTableAlias(model, tableName), reflection.ExtractTableNameOnly(tableName))
}
