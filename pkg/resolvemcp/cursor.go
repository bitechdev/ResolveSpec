package resolvemcp

// Cursor-based pagination adapted from pkg/resolvespec/cursor.go.

import (
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

type cursorDirection int

const (
	cursorForward  cursorDirection = 1
	cursorBackward cursorDirection = -1
)

// getCursorFilter generates a SQL EXISTS subquery for cursor-based pagination.
func getCursorFilter(
	tableName string,
	pkName string,
	modelColumns []string,
	options common.RequestOptions,
) (string, error) {
	fullTableName := tableName
	if strings.Contains(tableName, ".") {
		tableName = strings.SplitN(tableName, ".", 2)[1]
	}

	cursorID, direction := getActiveCursor(options)
	if cursorID == "" {
		return "", fmt.Errorf("no cursor provided for table %s", tableName)
	}

	sortItems := options.Sort
	if len(sortItems) == 0 {
		return "", fmt.Errorf("no sort columns defined")
	}

	var whereClauses []string
	reverse := direction < 0

	for _, s := range sortItems {
		col := strings.TrimSpace(s.Column)
		if col == "" {
			continue
		}

		parts := strings.Split(col, ".")
		field := strings.TrimSpace(parts[len(parts)-1])
		prefix := strings.Join(parts[:len(parts)-1], ".")

		desc := strings.EqualFold(s.Direction, "desc")
		if reverse {
			desc = !desc
		}

		cursorCol, targetCol, err := resolveCursorColumn(field, prefix, tableName, modelColumns)
		if err != nil {
			logger.Warn("Skipping invalid sort column %q: %v", col, err)
			continue
		}

		op := "<"
		if desc {
			op = ">"
		}
		whereClauses = append(whereClauses, fmt.Sprintf("%s %s %s", cursorCol, op, targetCol))
	}

	if len(whereClauses) == 0 {
		return "", fmt.Errorf("no valid sort columns after filtering")
	}

	orSQL := buildCursorPriorityChain(whereClauses)

	query := fmt.Sprintf(`EXISTS (
  SELECT 1
  FROM %s cursor_select
  WHERE cursor_select.%s = %s
    AND (%s)
)`,
		fullTableName,
		pkName,
		cursorID,
		orSQL,
	)

	return query, nil
}

func getActiveCursor(options common.RequestOptions) (id string, direction cursorDirection) {
	if options.CursorForward != "" {
		return options.CursorForward, cursorForward
	}
	if options.CursorBackward != "" {
		return options.CursorBackward, cursorBackward
	}
	return "", 0
}

func resolveCursorColumn(field, prefix, tableName string, modelColumns []string) (cursorCol, targetCol string, err error) {
	if strings.Contains(field, "->") {
		return "cursor_select." + field, tableName + "." + field, nil
	}

	if modelColumns != nil {
		for _, col := range modelColumns {
			if strings.EqualFold(col, field) {
				return "cursor_select." + field, tableName + "." + field, nil
			}
		}
	} else {
		return "cursor_select." + field, tableName + "." + field, nil
	}

	if prefix != "" && prefix != tableName {
		return "", "", fmt.Errorf("joined columns not supported in cursor pagination: %s", field)
	}

	return "", "", fmt.Errorf("invalid column: %s", field)
}

func buildCursorPriorityChain(clauses []string) string {
	var or []string
	for i := 0; i < len(clauses); i++ {
		and := strings.Join(clauses[:i+1], "\n    AND ")
		or = append(or, "("+and+")")
	}
	return strings.Join(or, "\n  OR ")
}
