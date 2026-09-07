package common

import (
	"fmt"
	"regexp"
	"strings"
)

// This file implements a single canonical parser + SQL builder for column
// references that traverse into JSON / JSONB values. It is used by SELECT,
// WHERE (filter) and ORDER BY handling so that all three treat JSON access
// consistently and safely.
//
// Supported input syntaxes (all PostgreSQL-oriented):
//
//	data->>'city'                 arrow chain, text extraction
//	data->'addr'->>'city'         nested arrow chain
//	data->2->>'name'              arrow chain with array index
//	data#>>'{addr,city}'          hash-path, text extraction
//	data#>'{addr,city}'           hash-path, jsonb result
//	data.addr.city                dotted shorthand (Ambiguous: caller must
//	                              confirm "data" is a JSON column)
//	data->>'age'::int             trailing cast (whitelisted targets only)
//	(data->>'city') AS city       parenthesised, with output alias
//
// JSON path segments are never interpolated into SQL: SQL() emits a `#>>` /
// `#>` operator with the path bound as a single `text[]` parameter.

// ColumnRef is a parsed reference to a (possibly JSON-traversing) column.
type ColumnRef struct {
	// Base is the bare base column name, e.g. "data". Always a simple
	// identifier ([A-Za-z_][A-Za-z0-9_]*); qualified names are rejected.
	Base string
	// Path is the JSON key / array-index path, e.g. ["address", "city"].
	// Empty for a plain column reference.
	Path []string
	// AsText is true when the final extraction should yield text (->> / #>>)
	// rather than jsonb (-> / #>).
	AsText bool
	// Cast is a normalised SQL type name to cast the whole expression to
	// (e.g. "integer", "numeric", "timestamptz"), or "" for no cast.
	Cast string
	// Alias is a validated output identifier for `AS <alias>`, or "".
	Alias string
	// Ambiguous is true when Path was produced from the dotted "a.b.c"
	// shorthand. The caller MUST verify that Base is a JSON column
	// (reflection.IsJSONColumn) before treating this as a JSON expression,
	// otherwise "a.b" is an ordinary table-qualified column.
	Ambiguous bool
}

var (
	reSimpleIdent   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reSimpleSegment = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	reAliasSuffix   = regexp.MustCompile(`(?i)\s+AS\s+("?[A-Za-z_][A-Za-z0-9_]*"?)\s*$`)
	reArrowStep     = regexp.MustCompile(`^\s*(->>|->)\s*(?:'((?:[^']|'')*)'|(\d+))\s*`)
)

const (
	maxJSONPathDepth   = 32
	maxJSONSegmentSize = 128
)

// castAliases maps accepted cast spellings to their canonical PostgreSQL type.
var castAliases = map[string]string{
	"int":              "integer",
	"int4":             "integer",
	"integer":          "integer",
	"int2":             "smallint",
	"smallint":         "smallint",
	"int8":             "bigint",
	"bigint":           "bigint",
	"numeric":          "numeric",
	"decimal":          "numeric",
	"real":             "real",
	"float4":           "real",
	"float":            "double precision",
	"float8":           "double precision",
	"double precision": "double precision",
	"bool":             "boolean",
	"boolean":          "boolean",
	"text":             "text",
	"varchar":          "text",
	"uuid":             "uuid",
	"date":             "date",
	"time":             "time",
	"timestamp":        "timestamp",
	"timestamptz":      "timestamptz",
	"json":             "json",
	"jsonb":            "jsonb",
}

// NormalizeCastTarget returns the canonical PostgreSQL type name for a
// user-supplied cast spelling, and whether it is on the allowlist.
func NormalizeCastTarget(s string) (string, bool) {
	c, ok := castAliases[strings.ToLower(strings.TrimSpace(s))]
	return c, ok
}

// ParseColumnRef parses a column token that traverses into a JSON value.
//
// ok is true only when the token carries JSON traversal syntax (arrow chain,
// hash-path, or dotted shorthand with at least one sub-key). For a plain
// column name — with or without an alias/cast — ok is false and the caller
// should handle the token the way it did before.
//
// When ok is true and ref.Ambiguous is true, the caller must confirm that
// ref.Base is a JSON column before using ref.SQL; otherwise the dotted token
// is an ordinary "table.column" reference.
func ParseColumnRef(raw string) (ColumnRef, bool) {
	expr := strings.TrimSpace(raw)
	if expr == "" {
		return ColumnRef{}, false
	}

	var ref ColumnRef

	// 1. Trailing `AS <alias>`.
	if m := reAliasSuffix.FindStringSubmatch(expr); m != nil {
		ref.Alias = strings.Trim(m[1], `"`)
		expr = strings.TrimSpace(expr[:len(expr)-len(m[0])])
		if expr == "" {
			return ColumnRef{}, false
		}
	}

	// 2. Trailing `::<type>` cast (take the last `::` in the string).
	if idx := strings.LastIndex(expr, "::"); idx != -1 {
		candidate := strings.TrimSpace(expr[idx+2:])
		if canonical, allowed := NormalizeCastTarget(candidate); allowed {
			ref.Cast = canonical
			expr = strings.TrimSpace(expr[:idx])
		} else if candidate != "" && looksLikeCastTail(candidate) {
			// An explicit but unsupported cast target — reject rather than
			// silently dropping it.
			return ColumnRef{}, false
		}
	}

	// 3. One layer of wrapping parentheses: "(expr)" -> "expr".
	if wrapped, ok := stripWrappingParens(expr); ok {
		expr = strings.TrimSpace(wrapped)
		if expr == "" {
			return ColumnRef{}, false
		}
	}

	// 4. Parse the core expression.
	switch {
	case strings.Contains(expr, "#>>") || strings.Contains(expr, "#>"):
		if !parseHashPath(expr, &ref) {
			return ColumnRef{}, false
		}
	case strings.Contains(expr, "->"):
		if !parseArrowChain(expr, &ref) {
			return ColumnRef{}, false
		}
	case strings.Contains(expr, "."):
		if !parseDottedPath(expr, &ref) {
			return ColumnRef{}, false
		}
	default:
		// Plain column — nothing JSON about it.
		return ColumnRef{}, false
	}

	if !validateRef(&ref) {
		return ColumnRef{}, false
	}
	return ref, true
}

// SQL renders the reference as a parameterised SQL expression plus its args.
// tableAlias, when non-empty, qualifies the base column (each dot-separated
// part is quoted independently, so "public.users" -> `"public"."users"`).
func (r ColumnRef) SQL(tableAlias string) (expr string, args []interface{}) {
	base := quoteQualifiedIdent(r.Base)
	if tableAlias != "" {
		base = quoteQualifiedIdent(tableAlias) + "." + QuoteIdent(r.Base)
	}

	if len(r.Path) == 0 {
		if r.Cast != "" {
			return fmt.Sprintf("(%s)::%s", base, r.Cast), nil
		}
		return base, nil
	}

	op := "#>"
	if r.AsText {
		op = "#>>"
	}
	expr = fmt.Sprintf("(%s %s ?::text[])", base, op)
	args = []interface{}{pgTextArrayLiteral(r.Path)}

	if r.Cast != "" {
		expr = fmt.Sprintf("(%s)::%s", expr, r.Cast)
	}
	return expr, args
}

// OutputAlias returns the alias to use for this reference in a SELECT list:
// the explicit alias when given, otherwise a deterministic name derived from
// the base column and path (e.g. "data_address_city").
func (r ColumnRef) OutputAlias() string {
	if r.Alias != "" {
		return r.Alias
	}
	if len(r.Path) == 0 {
		return r.Base
	}
	parts := make([]string, 0, len(r.Path)+1)
	parts = append(parts, r.Base)
	for _, p := range r.Path {
		parts = append(parts, sanitizeAliasPart(p))
	}
	return strings.Join(parts, "_")
}

// ── parsing helpers ─────────────────────────────────────────────────────────

func parseHashPath(expr string, ref *ColumnRef) bool {
	op := "#>>"
	ref.AsText = true
	if !strings.Contains(expr, "#>>") {
		op = "#>"
		ref.AsText = false
	}
	parts := strings.SplitN(expr, op, 2)
	if len(parts) != 2 {
		return false
	}
	ref.Base = strings.TrimSpace(parts[0])

	rhs := strings.TrimSpace(parts[1])
	// Expect a single-quoted array literal: '{a,b,c}'
	if len(rhs) < 2 || rhs[0] != '\'' || rhs[len(rhs)-1] != '\'' {
		return false
	}
	rhs = rhs[1 : len(rhs)-1]
	rhs = strings.TrimSpace(rhs)
	rhs = strings.TrimPrefix(rhs, "{")
	rhs = strings.TrimSuffix(rhs, "}")
	if strings.TrimSpace(rhs) == "" {
		return false
	}
	for _, seg := range strings.Split(rhs, ",") {
		seg = strings.TrimSpace(seg)
		seg = strings.Trim(seg, `"`)
		if seg == "" {
			return false
		}
		ref.Path = append(ref.Path, seg)
	}
	return true
}

func parseArrowChain(expr string, ref *ColumnRef) bool {
	arrowIdx := strings.Index(expr, "->")
	if arrowIdx <= 0 {
		return false
	}
	ref.Base = strings.TrimSpace(expr[:arrowIdx])

	rest := expr[arrowIdx:]
	for strings.TrimSpace(rest) != "" {
		m := reArrowStep.FindStringSubmatch(rest)
		if m == nil {
			return false
		}
		ref.AsText = m[1] == "->>"
		if m[3] != "" {
			// unquoted array index
			ref.Path = append(ref.Path, m[3])
		} else {
			// quoted key; unescape doubled single quotes
			ref.Path = append(ref.Path, strings.ReplaceAll(m[2], "''", "'"))
		}
		rest = rest[len(m[0]):]
	}
	return len(ref.Path) > 0
}

func parseDottedPath(expr string, ref *ColumnRef) bool {
	segs := strings.Split(expr, ".")
	if len(segs) < 2 {
		return false
	}
	for i, s := range segs {
		s = strings.TrimSpace(s)
		if !reSimpleSegment.MatchString(s) {
			return false
		}
		if i == 0 {
			ref.Base = s
		} else {
			ref.Path = append(ref.Path, s)
		}
	}
	ref.AsText = true
	ref.Ambiguous = true
	return true
}

func validateRef(ref *ColumnRef) bool {
	if !reSimpleIdent.MatchString(ref.Base) {
		return false
	}
	if len(ref.Path) == 0 || len(ref.Path) > maxJSONPathDepth {
		return false
	}
	for _, seg := range ref.Path {
		if seg == "" || len(seg) > maxJSONSegmentSize || strings.ContainsRune(seg, 0) {
			return false
		}
	}
	if ref.Alias != "" && !reSimpleIdent.MatchString(ref.Alias) {
		return false
	}
	return true
}

// looksLikeCastTail reports whether s is plausibly meant as a `::type` target
// (letters/digits/spaces only) rather than, say, part of a JSON operator.
func looksLikeCastTail(s string) bool {
	for _, r := range s {
		isCastChar := r == ' ' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isCastChar {
			return false
		}
	}
	return true
}

// stripWrappingParens removes one layer of parentheses when they wrap the whole
// expression, e.g. "(a->>'b')" -> "a->>'b'". It respects single-quoted strings.
func stripWrappingParens(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s, false
	}
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
			// skip
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				// closing paren is not the last char -> not a full wrap
				return s, false
			}
		}
	}
	if depth != 0 {
		return s, false
	}
	return s[1 : len(s)-1], true
}

// pgTextArrayLiteral builds a PostgreSQL text[] array literal ("{a,b,c}") from
// path segments, quoting and escaping any segment that is not a bare word.
func pgTextArrayLiteral(segs []string) string {
	escaper := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	parts := make([]string, len(segs))
	for i, s := range segs {
		if reSimpleSegment.MatchString(s) {
			parts[i] = s
		} else {
			parts[i] = `"` + escaper.Replace(s) + `"`
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// quoteQualifiedIdent quotes each dot-separated part of an identifier.
func quoteQualifiedIdent(ident string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}

func sanitizeAliasPart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
