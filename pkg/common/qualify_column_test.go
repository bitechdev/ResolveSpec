package common

import "testing"

type qualifyModel struct {
	ID   int64  `bun:"id,pk"`
	Name string `bun:"name"`
}

func TestQualifyModelColumn(t *testing.T) {
	m := qualifyModel{}
	cases := []struct{ alias, col, want string }{
		{"t", "name", `"t"."name"`},
		{"t", "NAME", `"t"."NAME"`},
		{"t", "missing", "missing"},
		{"t", "data->>'x'", "data->>'x'"},
		{"t", "rel.name", "rel.name"},
		{"", "name", "name"},
	}
	for _, c := range cases {
		if got := QualifyModelColumn(m, c.alias, c.col); got != c.want {
			t.Errorf("Qualify(%q,%q) = %q, want %q", c.alias, c.col, got, c.want)
		}
	}
	if got := QualifyModelColumn(nil, "t", "name"); got != "name" {
		t.Errorf("nil model: %q", got)
	}
}

func TestStripMainTablePrefix(t *testing.T) {
	m := qualifyModel{}
	cases := []struct{ col, want string }{
		{"province_state.name", "name"},
		{`"province_state"."name"`, "name"},
		{"PROVINCE_STATE.name", "name"},
		{"rel_rid_country.name", "rel_rid_country.name"},
		{"province_state.missing", "province_state.missing"},
		{"name", "name"},
	}
	for _, c := range cases {
		if got := StripMainTablePrefix(m, c.col, "province_state"); got != c.want {
			t.Errorf("Strip(%q) = %q, want %q", c.col, got, c.want)
		}
	}
}

func TestStripMainTablePrefix_ValidatorKeepsFilter(t *testing.T) {
	m := qualifyModel{}
	filters := []FilterOption{{Column: "province_state.name", Operator: "eq", Value: "x"}}
	StripMainTablePrefixFromFilters(m, filters, "province_state")
	out := NewColumnValidator(m).FilterRequestOptions(RequestOptions{Filters: filters})
	if len(out.Filters) != 1 || out.Filters[0].Column != "name" {
		t.Fatalf("filters = %+v", out.Filters)
	}
}
