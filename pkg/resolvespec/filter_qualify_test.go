package resolvespec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestBuildFilterConditionAlias_QualifiesModelColumns(t *testing.T) {
	h := &Handler{}
	model := jsonColModel{}

	tests := []struct {
		name   string
		filter common.FilterOption
		want   string
	}{
		{"eq", common.FilterOption{Column: "name", Operator: "eq", Value: "x"}, `"province_state"."name" = ?`},
		{"ilike", common.FilterOption{Column: "name", Operator: "ilike", Value: "%a%"}, `CAST("province_state"."name" AS TEXT) ILIKE ?`},
		{"like", common.FilterOption{Column: "name", Operator: "like", Value: "%a%"}, `CAST("province_state"."name" AS TEXT) LIKE ?`},
		{"in", common.FilterOption{Column: "name", Operator: "in", Value: []string{"a", "b"}}, `"province_state"."name" IN (?,?)`},
		{"non-model column untouched", common.FilterOption{Column: "other", Operator: "eq", Value: 1}, `other = ?`},
		{"already qualified untouched", common.FilterOption{Column: "rel.name", Operator: "eq", Value: 1}, `rel.name = ?`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := h.buildFilterConditionAlias(tt.filter, model, "province_state")
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildFilterConditionAlias_NoAliasUnchanged(t *testing.T) {
	h := &Handler{}
	got, _ := h.buildFilterConditionAlias(common.FilterOption{Column: "name", Operator: "ilike", Value: "%a%"}, jsonColModel{}, "")
	if got != "CAST(name AS TEXT) ILIKE ?" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyFilter_QualifiesWithAlias(t *testing.T) {
	h := &Handler{}
	q := &jsonCapQuery{}
	h.applyFilter(q, common.FilterOption{Column: "name", Operator: "ilike", Value: "%a%"}, jsonColModel{}, "province_state")
	c := q.only(t)
	if c.query != `CAST("province_state"."name" AS TEXT) ILIKE ?` {
		t.Fatalf("query = %q", c.query)
	}
}

func TestApplyFilters_OrGroupQualified(t *testing.T) {
	h := &Handler{}
	q := &jsonCapQuery{}
	h.applyFilters(q, []common.FilterOption{
		{Column: "name", Operator: "ilike", Value: "%a%"},
		{Column: "id", Operator: "eq", Value: 1, LogicOperator: "OR"},
	}, jsonColModel{}, "t")
	c := q.only(t)
	want := `(CAST("t"."name" AS TEXT) ILIKE ? OR "t"."id" = ?)`
	if c.query != want {
		t.Fatalf("query = %q, want %q", c.query, want)
	}
}
