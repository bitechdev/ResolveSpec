package resolvespec

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
)

type joinCountry struct {
	bun.BaseModel `bun:"table:country,alias:country"`
	ID            int64  `bun:"id,pk"`
	Name          string `bun:"name"`
}

type joinProvince struct {
	bun.BaseModel `bun:"table:province_state,alias:province_state"`
	ID            int64  `bun:"id,pk"`
	Name          string `bun:"name"`
	Abbreviation  string `bun:"abbreviation"`
	RidCountry    int64  `bun:"rid_country"`
}

// SQLite has no ILIKE; the ilike operator is covered by SQL-string tests, and
// LIKE here proves the CAST(... AS TEXT) wrapping executes with qualified columns.
func setupJoinDB(t *testing.T) *bun.DB {
	t.Helper()
	sqldb, err := sql.Open(sqliteshim.ShimName, "file:filterjoin?mode=memory&cache=private")
	if err != nil {
		t.Fatal(err)
	}
	db := bun.NewDB(sqldb, sqlitedialect.New())
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, m := range []interface{}{(*joinCountry)(nil), (*joinProvince)(nil)} {
		if _, err := db.NewCreateTable().Model(m).IfNotExists().Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.NewInsert().Model(&[]joinCountry{{ID: 1, Name: "Abcland"}, {ID: 2, Name: "Other"}}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewInsert().Model(&[]joinProvince{
		{ID: 1, Name: "abc one", Abbreviation: "A1", RidCountry: 1},
		{ID: 2, Name: "xyz two", Abbreviation: "ABC", RidCountry: 1},
		{ID: 3, Name: "nope", Abbreviation: "N3", RidCountry: 2},
	}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

const joinSQL = "LEFT JOIN country AS rel_rid_country ON rel_rid_country.id = province_state.rid_country"

func countWith(t *testing.T, db *bun.DB, withJoin bool, filters []common.FilterOption, alias string) (int, error) {
	t.Helper()
	h := &Handler{}
	var q common.SelectQuery = database.NewBunAdapter(db).NewSelect().Model(&[]*joinProvince{})
	if withJoin {
		q = q.Join(joinSQL)
	}
	q = h.applyFilters(q, filters, &joinProvince{}, alias)
	return q.Count(context.Background())
}

func TestFilters_AmbiguousColumnWithJoin_RealQuery(t *testing.T) {
	db := setupJoinDB(t)

	filters := []common.FilterOption{
		{Column: "name", Operator: "like", Value: "%abc%"},
		{Column: "abbreviation", Operator: "like", Value: "%abc%", LogicOperator: "OR"},
	}

	t.Run("unqualified with join is ambiguous (the bug)", func(t *testing.T) {
		_, err := countWith(t, db, true, filters, "")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
			t.Fatalf("expected ambiguous column error, got %v", err)
		}
	})

	t.Run("qualified with join", func(t *testing.T) {
		n, err := countWith(t, db, true, filters, "province_state")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("count = %d, want 2", n)
		}
	})

	t.Run("qualified without join", func(t *testing.T) {
		n, err := countWith(t, db, false, filters, "province_state")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("count = %d, want 2", n)
		}
	})

	t.Run("unqualified without join still works", func(t *testing.T) {
		n, err := countWith(t, db, false, filters, "")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("count = %d, want 2", n)
		}
	})
}

func TestFilters_AllOperatorsWithJoin_RealQuery(t *testing.T) {
	db := setupJoinDB(t)
	cases := []struct {
		name   string
		filter common.FilterOption
		want   int
	}{
		{"eq", common.FilterOption{Column: "name", Operator: "eq", Value: "nope"}, 1},
		{"neq", common.FilterOption{Column: "name", Operator: "neq", Value: "nope"}, 2},
		{"like", common.FilterOption{Column: "name", Operator: "like", Value: "abc%"}, 1},
		{"in", common.FilterOption{Column: "name", Operator: "in", Value: []string{"nope", "xyz two"}}, 2},
		{"gt", common.FilterOption{Column: "id", Operator: "gt", Value: 1}, 2},
		{"gte", common.FilterOption{Column: "id", Operator: "gte", Value: 2}, 2},
		{"lt", common.FilterOption{Column: "id", Operator: "lt", Value: 3}, 2},
		{"lte", common.FilterOption{Column: "id", Operator: "lte", Value: 1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := countWith(t, db, true, []common.FilterOption{c.filter}, "province_state")
			if err != nil {
				t.Fatal(err)
			}
			if n != c.want {
				t.Fatalf("count = %d, want %d", n, c.want)
			}
		})
	}
}

// A filter on the joined table's own column, sent already qualified, must pass through untouched.
func TestFilters_JoinedTableColumnPassesThrough_RealQuery(t *testing.T) {
	db := setupJoinDB(t)
	n, err := countWith(t, db, true, []common.FilterOption{
		{Column: "rel_rid_country.name", Operator: "eq", Value: "Abcland"},
	}, "province_state")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

// Client sends "province_state.name": it must survive validation and be applied.
func TestFilters_ClientQualifiedColumn_NotDropped(t *testing.T) {
	db := setupJoinDB(t)
	model := &joinProvince{}

	opts := common.RequestOptions{Filters: []common.FilterOption{
		{Column: "province_state.name", Operator: "like", Value: "%abc%"},
	}}
	common.NormalizeMainTableFilters(model, "public.province_state", &opts)
	opts = common.NewColumnValidator(model).FilterRequestOptions(opts)
	if len(opts.Filters) != 1 {
		t.Fatalf("filter was dropped: %+v", opts.Filters)
	}

	n, err := countWith(t, db, true, opts.Filters, "province_state")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1 (filter must narrow the result)", n)
	}
}
