package database

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// The client's OR must widen only the client's own conditions, never the
// server-side condition ANDed after it.
func TestBunWhereGroupConfinesOr(t *testing.T) {
	db := bun.NewDB(&sql.DB{}, pgdialect.New())
	q := &BunSelectQuery{query: db.NewSelect().TableExpr("items"), db: db, driverName: "postgres"}

	var sq common.SelectQuery = q.Where("a = 1")
	sq = sq.(common.WhereGrouper).WhereGroup(func(g common.SelectQuery) common.SelectQuery {
		return g.Where("b = 2").WhereOr("(c = 3)")
	})
	sq = sq.Where("tenant = 5")

	got := sq.(*BunSelectQuery).query.String()
	want := `WHERE (a = 1) AND ((b = 2) OR ((c = 3))) AND (tenant = 5)`
	if !strings.Contains(got, want) {
		t.Fatalf("unexpected SQL:\n got: %s\nwant to contain: %s", got, want)
	}
}

func TestPgSQLWhereGroupConfinesOr(t *testing.T) {
	var q common.SelectQuery = &PgSQLSelectQuery{driverName: "postgres", tableName: "items", columns: []string{"*"}, args: []interface{}{}}
	q = q.Where("a = ?", 1)
	q = q.(common.WhereGrouper).WhereGroup(func(g common.SelectQuery) common.SelectQuery {
		return g.Where("b = ?", 2).WhereOr("(c = 3)")
	})
	q = q.Where("tenant = ?", 5)

	pq := q.(*PgSQLSelectQuery)
	got := pq.buildSQL()
	want := `WHERE (a = $1 AND ((b = $2) OR (c = 3)) AND tenant = $3)`
	if !strings.Contains(got, want) {
		t.Fatalf("unexpected SQL:\n got: %s\nwant to contain: %s", got, want)
	}
	if len(pq.args) != 3 || pq.args[0] != 1 || pq.args[1] != 2 || pq.args[2] != 5 {
		t.Fatalf("args out of order: %v", pq.args)
	}
}
