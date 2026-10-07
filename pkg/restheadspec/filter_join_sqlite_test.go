package restheadspec

import (
	"context"
	"database/sql"
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

func setupJoinDB(t *testing.T) *bun.DB {
	t.Helper()
	sqldb, err := sql.Open(sqliteshim.ShimName, "file:rhsfilterjoin?mode=memory&cache=private")
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

// countFilters applies filters the way handleRead does (single AND filters via
// applyFilter, consecutive OR filters via applyOrFilterGroup) and counts.
func countFilters(t *testing.T, db *bun.DB, withJoin bool, filters []common.FilterOption) (int, error) {
	t.Helper()
	h := &Handler{}
	model := &joinProvince{}
	var q common.SelectQuery = database.NewBunAdapter(db).NewSelect().Model(&[]*joinProvince{})
	if withJoin {
		q = q.Join(joinSQL)
	}
	for i := 0; i < len(filters); {
		f := filters[i]
		castInfo := h.ValidateAndAdjustFilterForColumnType(&f, model)
		if f.LogicOperator == "OR" {
			group := []*common.FilterOption{&f}
			info := []ColumnCastInfo{castInfo}
			j := i + 1
			for j < len(filters) && filters[j].LogicOperator == "OR" {
				g := filters[j]
				info = append(info, h.ValidateAndAdjustFilterForColumnType(&g, model))
				group = append(group, &g)
				j++
			}
			q = h.applyOrFilterGroup(q, group, info, "public.province_state", model)
			i = j
			continue
		}
		q = h.applyFilter(q, f, "public.province_state", castInfo.NeedsCast, "AND", model)
		i++
	}
	return q.Count(context.Background())
}

func TestRHSFilters_WithAndWithoutJoin_RealQuery(t *testing.T) {
	db := setupJoinDB(t)
	cases := []struct {
		name    string
		filters []common.FilterOption
		want    int
	}{
		{"eq", []common.FilterOption{{Column: "name", Operator: "eq", Value: "nope"}}, 1},
		{"neq", []common.FilterOption{{Column: "name", Operator: "neq", Value: "nope"}}, 2},
		{"like", []common.FilterOption{{Column: "name", Operator: "like", Value: "%abc%"}}, 1},
		{"in", []common.FilterOption{{Column: "name", Operator: "in", Value: []string{"nope", "xyz two"}}}, 2},
		{"gt", []common.FilterOption{{Column: "id", Operator: "gt", Value: 1}}, 2},
		{"between", []common.FilterOption{{Column: "id", Operator: "between", Value: []interface{}{0, 3}}}, 2},
		{"between_inclusive", []common.FilterOption{{Column: "id", Operator: "between_inclusive", Value: []interface{}{1, 3}}}, 3},
		{"is_not_null", []common.FilterOption{{Column: "name", Operator: "is_not_null"}}, 3},
		{"or group", []common.FilterOption{
			{Column: "name", Operator: "like", Value: "%abc%", LogicOperator: "OR"},
			{Column: "abbreviation", Operator: "like", Value: "%abc%", LogicOperator: "OR"},
		}, 2},
		{"or group then and", []common.FilterOption{
			{Column: "name", Operator: "like", Value: "%abc%", LogicOperator: "OR"},
			{Column: "abbreviation", Operator: "like", Value: "%abc%", LogicOperator: "OR"},
			{Column: "id", Operator: "eq", Value: 2},
		}, 1},
	}
	for _, c := range cases {
		for _, withJoin := range []bool{false, true} {
			name := c.name + "/no_join"
			if withJoin {
				name = c.name + "/join"
			}
			t.Run(name, func(t *testing.T) {
				n, err := countFilters(t, db, withJoin, c.filters)
				if err != nil {
					t.Fatalf("query failed (ambiguous column?): %v", err)
				}
				if n != c.want {
					t.Fatalf("count = %d, want %d", n, c.want)
				}
			})
		}
	}
}

func TestRHSFilters_JoinedTableColumnPassesThrough(t *testing.T) {
	db := setupJoinDB(t)
	n, err := countFilters(t, db, true, []common.FilterOption{
		{Column: "rel_rid_country.name", Operator: "eq", Value: "Abcland"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

// Client sends "province_state.name": it must survive validation and be applied.
func TestRHSFilters_ClientQualifiedColumn_NotDropped(t *testing.T) {
	db := setupJoinDB(t)
	h := &Handler{}
	model := &joinProvince{}

	opts := ExtendedRequestOptions{}
	opts.Filters = []common.FilterOption{{Column: "province_state.name", Operator: "like", Value: "%abc%"}}
	common.NormalizeMainTableFilters(model, "public.province_state", &opts.RequestOptions)
	opts = h.filterExtendedOptions(common.NewColumnValidator(model), opts, model)
	if len(opts.Filters) != 1 || opts.Filters[0].Column != "name" {
		t.Fatalf("filter was dropped or not normalised: %+v", opts.Filters)
	}

	n, err := countFilters(t, db, true, opts.Filters)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestRHSFilters_ILikeSQLQualified(t *testing.T) {
	h := &Handler{}
	q := &jsonCapQuery{}
	f := common.FilterOption{Column: "name", Operator: "ilike", Value: "%abc%"}
	h.applyFilter(q, f, "info.province_state", false, "AND", jsonColModel{})
	if got := q.only(t).query; got != "CAST(province_state.name AS TEXT) ILIKE ?" {
		t.Fatalf("query = %q", got)
	}
}
