package resolvespec

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
// It uses the current request's sort and cursor values.
//
// Parameters:
//   - tableName: name of the main table (e.g. "posts")
//   - pkName: primary key column (e.g. "id")
//   - modelColumns: optional list of valid main-table columns (for validation). Pass nil to skip.
//   - options: the request options containing sort and cursor information
//
// Returns SQL snippet to embed in WHERE clause.
func GetCursorFilter(
	tableName string,
	pkName string,
	modelColumns []string,
	options common.RequestOptions,
) (string, error) {
	// Remove schema prefix if present
	if strings.Contains(tableName, ".") {
		tableName = strings.SplitN(tableName, ".", 2)[1]
	}

	// --------------------------------------------------------------------- //
	// 1. Determine active cursor
	// --------------------------------------------------------------------- //
	cursorID, direction := getActiveCursor(options)
	if cursorID == "" {
		return "", fmt.Errorf("no cursor provided for table %s", tableName)
	}

	// --------------------------------------------------------------------- //
	// 2. Extract sort columns
	// --------------------------------------------------------------------- //
	sortItems := options.Sort
	if len(sortItems) == 0 {
		return "", fmt.Errorf("no sort columns defined")
	}

	// --------------------------------------------------------------------- //
	// 3. Prepare
	// --------------------------------------------------------------------- //
	var whereClauses []string
	reverse := direction < 0

	// --------------------------------------------------------------------- //
	// 4. Process each sort column
	// --------------------------------------------------------------------- //
	for _, s := range sortItems {
		col := strings.TrimSpace(s.Column)
		if col == "" {
			continue
		}

		// Parse: "created_at", "user.name", etc.
		parts := strings.Split(col, ".")
		field := strings.TrimSpace(parts[len(parts)-1])
		prefix := strings.Join(parts[:len(parts)-1], ".")

		// Direction from struct
		desc := strings.EqualFold(s.Direction, "desc")

		if reverse {
			desc = !desc
		}

		// Resolve column
		cursorCol, targetCol, err := resolveColumn(
			field, prefix, tableName, modelColumns,
		)
		if err != nil {
			logger.Warn("Skipping invalid sort column %q: %v", col, err)
			continue
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
  WHERE cursor_select.%s = %s
    AND (%s)
)`,
		tableName,
		pkName,
		cursorID,
		orSQL,
	)

	return query, nil
}

// ------------------------------------------------------------------------- //
// Helper: get active cursor (forward or backward)
func getActiveCursor(options common.RequestOptions) (id string, direction CursorDirection) {
	if options.CursorForward != "" {
		return options.CursorForward, CursorForward
	}
	if options.CursorBackward != "" {
		return options.CursorBackward, CursorBackward
	}
	return "", 0
}

// Helper: resolve column (main table only for now)
func resolveColumn(
	field, prefix, tableName string,
	modelColumns []string,
) (cursorCol, targetCol string, err error) {

	// JSON field
	if strings.Contains(field, "->") {
		return "cursor_select." + field, tableName + "." + field, nil
	}

	// Main table column
	if modelColumns != nil {
		for _, col := range modelColumns {
			if strings.EqualFold(col, field) {
				return "cursor_select." + field, tableName + "." + field, nil
			}
		}
	} else {
		// No validation → allow all main-table fields
		return "cursor_select." + field, tableName + "." + field, nil
	}

	// Joined column (not supported in resolvespec yet)
	if prefix != "" && prefix != tableName {
		return "", "", fmt.Errorf("joined columns not supported in cursor pagination: %s", field)
	}

	return "", "", fmt.Errorf("invalid column: %s", field)
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
