package common

import (
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// This file wires the canonical JSON column parser (json_column.go) into the
// three query-building paths that every spec handler shares: SELECT column
// lists, WHERE filters and ORDER BY. The helpers here are the single place
// those paths call so that JSON access is resolved (and made injection-safe)
// identically everywhere. They mirror the style of BuildSpatialCondition /
// BuildVectorCondition: a boolean ok result tells the caller whether the token
// was a JSON reference it should take over, otherwise the caller keeps its
// existing (non-JSON) behaviour.

// jsonComparisonOps are the operators for which a JSON text extraction should be
// cast to a concrete type when the value looks numeric — otherwise "10" < "9".
var jsonComparisonOps = map[string]bool{
	"gt": true, "greater_than": true, ">": true,
	"gte": true, "greater_than_equals": true, "ge": true, ">=": true,
	"lt": true, "less_than": true, "<": true,
	"lte": true, "less_than_equals": true, "le": true, "<=": true,
	"between": true, "between_inclusive": true,
}

// ResolveJSONColumnRef parses token and, when it is a usable JSON reference for
// model, returns the parsed ColumnRef. For the dotted "a.b" shorthand (which is
// otherwise indistinguishable from a table-qualified column) ok is true only
// when model confirms the base is a JSON column.
func ResolveJSONColumnRef(model interface{}, token string) (ColumnRef, bool) {
	ref, ok := ParseColumnRef(token)
	if !ok {
		return ColumnRef{}, false
	}
	if ref.Ambiguous && !reflection.IsJSONColumn(model, ref.Base) {
		return ColumnRef{}, false
	}
	return ref, true
}

// IsJSONColumnToken reports whether token is a JSON reference this package can
// resolve for model (arrow/hash syntax always; dotted shorthand only when the
// base is a JSON column).
func IsJSONColumnToken(model interface{}, token string) bool {
	_, ok := ResolveJSONColumnRef(model, token)
	return ok
}

// ResolveJSONColumnExpr resolves a raw column token that traverses into a JSON
// value into a parameterised SQL expression plus its args and a deterministic
// output alias. ok is false when the token is not a JSON reference, in which
// case the caller should handle it the way it did before.
//
// tableAlias, when non-empty, qualifies the base column.
func ResolveJSONColumnExpr(model interface{}, tableAlias, token string) (expr string, args []interface{}, alias string, ok bool) {
	ref, ok := ResolveJSONColumnRef(model, token)
	if !ok {
		return "", nil, "", false
	}
	expr, args = ref.SQL(tableAlias)
	return expr, args, ref.OutputAlias(), true
}

// ApplySelectColumns adds the requested columns to query, resolving any that are
// JSON sub-field references (data->>'x', data#>>'{a,b}', or the dotted data.x
// shorthand for a JSON column) into safe parameterised expressions with a
// deterministic alias. Plain columns are passed through reflection.ExtractSourceColumn
// exactly as before. tableAlias, when non-empty, qualifies JSON base columns.
func ApplySelectColumns(query SelectQuery, model interface{}, tableAlias string, columns []string) SelectQuery {
	for _, col := range columns {
		if expr, args, alias, ok := ResolveJSONColumnExpr(model, tableAlias, col); ok {
			query = query.ColumnExpr(expr+" AS "+QuoteIdent(alias), args...)
			continue
		}
		query = query.Column(reflection.ExtractSourceColumn(col))
	}
	return query
}

// BuildJSONFilterCondition builds a complete WHERE condition for a JSON column
// token. ok is false when the token is not a JSON reference or the operator is
// not one this builder handles (the caller then keeps its existing behaviour).
//
// The JSON path is always bound as a parameter, never interpolated. When the
// reference carries no explicit ::cast and the operator is an ordered
// comparison against a numeric value, the extracted text is cast to numeric so
// the comparison is numeric rather than lexical.
func BuildJSONFilterCondition(model interface{}, tableAlias, token, operator string, value interface{}) (condition string, args []interface{}, ok bool) {
	ref, ok := ResolveJSONColumnRef(model, token)
	if !ok {
		return "", nil, false
	}

	op := strings.ToLower(strings.TrimSpace(operator))

	// Infer a cast for ordered comparisons on numeric values so "10" > "9".
	if ref.Cast == "" && jsonComparisonOps[op] && jsonValueIsNumeric(value) {
		ref.Cast = "numeric"
	}

	colExpr, colArgs := ref.SQL(tableAlias)

	// prepend copies the column-expression args (the bound JSON path, and any
	// others) ahead of the value args so placeholder order matches the SQL.
	prepend := func(valueArgs ...interface{}) []interface{} {
		out := make([]interface{}, 0, len(colArgs)+len(valueArgs))
		out = append(out, colArgs...)
		out = append(out, valueArgs...)
		return out
	}

	switch op {
	case "eq", "equals", "=":
		return fmt.Sprintf("%s = ?", colExpr), prepend(value), true
	case "neq", "not_equals", "ne", "!=", "<>":
		return fmt.Sprintf("%s != ?", colExpr), prepend(value), true
	case "gt", "greater_than", ">":
		return fmt.Sprintf("%s > ?", colExpr), prepend(value), true
	case "gte", "greater_than_equals", "ge", ">=":
		return fmt.Sprintf("%s >= ?", colExpr), prepend(value), true
	case "lt", "less_than", "<":
		return fmt.Sprintf("%s < ?", colExpr), prepend(value), true
	case "lte", "less_than_equals", "le", "<=":
		return fmt.Sprintf("%s <= ?", colExpr), prepend(value), true
	case "like":
		return fmt.Sprintf("%s LIKE ?", colExpr), prepend(value), true
	case "ilike":
		return fmt.Sprintf("%s ILIKE ?", colExpr), prepend(value), true
	case "in":
		inCond, inArgs := BuildInCondition(colExpr, value)
		if inCond == "" {
			return "", nil, false
		}
		return inCond, prepend(inArgs...), true
	case "between", "between_inclusive":
		lo, hi, bok := twoBoundValues(value)
		if !bok {
			return "", nil, false
		}
		loOp, hiOp := ">", "<"
		if op == "between_inclusive" {
			loOp, hiOp = ">=", "<="
		}
		// colExpr appears twice, so its bound args (the JSON path) appear twice.
		betweenArgs := make([]interface{}, 0, 2*len(colArgs)+2)
		betweenArgs = append(betweenArgs, colArgs...)
		betweenArgs = append(betweenArgs, lo)
		betweenArgs = append(betweenArgs, colArgs...)
		betweenArgs = append(betweenArgs, hi)
		return fmt.Sprintf("(%s %s ? AND %s %s ?)", colExpr, loOp, colExpr, hiOp), betweenArgs, true
	case "is_null", "isnull":
		return fmt.Sprintf("%s IS NULL", colExpr), prepend(), true
	case "is_not_null", "isnotnull":
		return fmt.Sprintf("%s IS NOT NULL", colExpr), prepend(), true
	default:
		return "", nil, false
	}
}

// jsonValueIsNumeric reports whether value (or every element of a 2-slice) is a
// number or a numeric-looking string.
func jsonValueIsNumeric(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		if len(v) == 0 {
			return false
		}
		for _, e := range v {
			if !jsonValueIsNumeric(e) {
				return false
			}
		}
		return true
	case []string:
		if len(v) == 0 {
			return false
		}
		for _, e := range v {
			if _, ok := toFloat(e); !ok {
				return false
			}
		}
		return true
	case string:
		_, ok := toFloat(v)
		return ok
	default:
		_, ok := toFloat(value)
		return ok
	}
}

// twoBoundValues extracts the low/high bounds from a BETWEEN filter value.
func twoBoundValues(value interface{}) (lo, hi interface{}, ok bool) {
	switch v := value.(type) {
	case []interface{}:
		if len(v) == 2 {
			return v[0], v[1], true
		}
	case []string:
		if len(v) == 2 {
			return v[0], v[1], true
		}
	}
	return nil, nil, false
}
