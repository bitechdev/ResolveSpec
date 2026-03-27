package restheadspec

import (
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// CursorDirection defines pagination direction
type CursorDirection int

const (
	CursorForward  CursorDirection = 1
	CursorBackward CursorDirection = -1
)

// GetCursorFilter generates a SQL `EXISTS` subquery for cursor-based pagination.
// It uses the current request's sort, cursor, joins (via Expand), and CQL (via ComputedQL).
//
// Parameters:
//   - tableName: name of the main table (e.g. "post")
//   - pkName: primary key column (e.g. "id")
//   - modelColumns: optional list of valid main-table columns (for validation). Pass nil to skip.
//   - expandJoins: optional map[alias]string of JOIN clauses (e.g. "user": "LEFT JOIN user ON ...")
//
// Returns SQL snippet to embed in WHERE clause.
func (opts *ExtendedRequestOptions) GetCursorFilter(
	tableName string,
	pkName string,
	modelColumns []string, // optional: for validation
	expandJoins map[string]string, // optional: alias → JOIN SQL
) (string, error) {
	// Separate schema prefix from bare table name
	fullTableName := tableName
	if strings.Contains(tableName, ".") {
		tableName = strings.SplitN(tableName, ".", 2)[1]
	}
	// --------------------------------------------------------------------- //
	// 1. Determine active cursor
	// --------------------------------------------------------------------- //
	cursorID, direction := opts.getActiveCursor()
	if cursorID == "" {
		return "", fmt.Errorf("no cursor provided for table %s", tableName)
	}

	// --------------------------------------------------------------------- //
	// 2. Extract sort columns
	// --------------------------------------------------------------------- //
	sortItems := opts.getSortColumns()
	if len(sortItems) == 0 {
		return "", fmt.Errorf("no sort columns defined")
	}

	// --------------------------------------------------------------------- //
	// 3. Prepare
	// --------------------------------------------------------------------- //
	var whereClauses []string
	joinSQL := ""
	reverse := direction < 0

	// --------------------------------------------------------------------- //
	// 4. Process each sort column
	// --------------------------------------------------------------------- //
	for _, s := range sortItems {
		col := strings.Trim(strings.TrimSpace(s.Column), "()")
		if col == "" {
			continue
		}

		// Parse: "user.name desc nulls last"
		parts := strings.Split(col, ".")
		field := strings.TrimSpace(parts[len(parts)-1])
		prefix := strings.Join(parts[:len(parts)-1], ".")

		// Direction from struct or string
		desc := strings.EqualFold(s.Direction, "desc") ||
			strings.Contains(strings.ToLower(field), "desc")
		field = opts.cleanSortField(field)

		if reverse {
			desc = !desc
		}

		// Resolve column
		cursorCol, targetCol, isJoin, err := opts.resolveColumn(
			field, prefix, tableName, modelColumns,
		)
		if err != nil {
			logger.Warn("Skipping invalid sort column %q: %v", col, err)
			continue
		}

		// Handle joins
		if isJoin {
			if expandJoins != nil {
				if joinClause, ok := expandJoins[prefix]; ok {
					jSQL, cRef := rewriteJoin(joinClause, tableName, prefix)
					joinSQL = jSQL
					cursorCol = cRef + "." + field
					targetCol = prefix + "." + field
				}
			}
			if cursorCol == "" {
				logger.Warn("Skipping cursor sort column %q: join alias %q not in expandJoins", col, prefix)
				continue
			}
		}

		// Build inequality
		op := "<"
		if desc {
			op = ">"
		}
		whereClauses = append(whereClauses, fmt.Sprintf("%s %s %s", cursorCol, op, targetCol))
	}

	if len(whereClauses) == 0 {
		return "", fmt.Errorf("no valid sort columns after filtering")
	}

	// --------------------------------------------------------------------- //
	// 5. Build priority OR-AND chain
	// --------------------------------------------------------------------- //
	orSQL := buildPriorityChain(whereClauses)

	// --------------------------------------------------------------------- //
	// 6. Final EXISTS subquery
	// --------------------------------------------------------------------- //
	query := fmt.Sprintf(`EXISTS (
  SELECT 1
  FROM %s cursor_select
  %s
  WHERE cursor_select.%s = %s
    AND (%s)
)`,
		fullTableName,
		joinSQL,
		pkName,
		cursorID,
		orSQL,
	)

	return query, nil
}

// ------------------------------------------------------------------------- //
// Helper: get active cursor (forward or backward)
func (opts *ExtendedRequestOptions) getActiveCursor() (id string, direction CursorDirection) {
	if opts.CursorForward != "" {
		return opts.CursorForward, CursorForward
	}
	if opts.CursorBackward != "" {
		return opts.CursorBackward, CursorBackward
	}
	return "", 0
}

// Helper: extract sort columns
func (opts *ExtendedRequestOptions) getSortColumns() []common.SortOption {
	if opts.Sort != nil {
		return opts.Sort
	}
	return nil
}

// Helper: clean sort field (remove desc, asc, nulls)
func (opts *ExtendedRequestOptions) cleanSortField(field string) string {
	f := strings.ToLower(field)
	for _, token := range []string{"desc", "asc", "nulls last", "nulls first"} {
		f = strings.ReplaceAll(f, token, "")
	}
	return strings.TrimSpace(f)
}

// Helper: resolve column (main, JSON, CQL, join)
func (opts *ExtendedRequestOptions) resolveColumn(
	field, prefix, tableName string,
	modelColumns []string,
) (cursorCol, targetCol string, isJoin bool, err error) {

	// JSON field
	if strings.Contains(field, "->") {
		return "cursor_select." + field, tableName + "." + field, false, nil
	}

	// CQL via ComputedQL
	if strings.Contains(strings.ToLower(field), "cql") && opts.ComputedQL != nil {
		if expr, ok := opts.ComputedQL[field]; ok {
			return "cursor_select." + expr, expr, false, nil
		}
	}

	// Main table column
	if modelColumns != nil {
		for _, col := range modelColumns {
			if strings.EqualFold(col, field) {
				return "cursor_select." + field, tableName + "." + field, false, nil
			}
		}
	} else {
		// No validation → allow all main-table fields
		return "cursor_select." + field, tableName + "." + field, false, nil
	}

	// Joined column
	if prefix != "" && prefix != tableName {
		return "", "", true, nil
	}

	return "", "", false, fmt.Errorf("invalid column: %s", field)
}

// ------------------------------------------------------------------------- //
// Helper: rewrite JOIN clause for cursor subquery
func rewriteJoin(joinClause, mainTable, alias string) (joinSQL, cursorAlias string) {
	joinSQL = strings.ReplaceAll(joinClause, mainTable+".", "cursor_select.")
	cursorAlias = "cursor_select_" + alias
	joinSQL = strings.ReplaceAll(joinSQL, " "+alias+" ", " "+cursorAlias+" ")
	joinSQL = strings.ReplaceAll(joinSQL, " "+alias+".", " "+cursorAlias+".")
	return joinSQL, cursorAlias
}

// ------------------------------------------------------------------------- //
// Helper: build OR-AND priority chain
func buildPriorityChain(clauses []string) string {
	var or []string
	for i := 0; i < len(clauses); i++ {
		and := strings.Join(clauses[:i+1], "\n    AND ")
		or = append(or, "("+and+")")
	}
	return strings.Join(or, "\n  OR ")
}
