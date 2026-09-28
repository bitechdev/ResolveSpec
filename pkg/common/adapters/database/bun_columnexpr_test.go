package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBunSelectQuery_ColumnExpr_SpreadsArgs is a regression test for a bug
// where ColumnExpr passed its variadic args slice as a single argument
// (b.query.ColumnExpr(query, args) instead of args...), causing bun to
// serialize the arg slice itself (e.g. producing `'["{product,cost}"]'`
// instead of `'{product,cost}'` for a JSON path parameter).
func TestBunSelectQuery_ColumnExpr_SpreadsArgs(t *testing.T) {
	db := setupBunTestDB(t)
	defer db.Close()

	adapter := NewBunAdapter(db)

	sq := adapter.NewSelect().
		Table("test_inserts").
		ColumnExpr("(jsonvalue #>> ?::text[]) AS jsonvalue_product_cost", "{product,cost}")

	bsq, ok := sq.(*BunSelectQuery)
	require.True(t, ok, "expected *BunSelectQuery")

	sqlStr := bsq.query.String()
	require.NotContains(t, sqlStr, `["{product,cost}"]`, "arg slice must not be serialized as a JSON array: %s", sqlStr)
	require.True(t, strings.Contains(sqlStr, `'{product,cost}'`), "expected the bound text[] literal in SQL: %s", sqlStr)
}
