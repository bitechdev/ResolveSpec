package restheadspec

import (
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

// atdetailModel mirrors the real-world model that triggered this regression:
// rid_parent is a nullable bigint foreign key, backed by spectypes.SqlInt64
// (a SqlNull[int64] alias). An eq filter on it was being rendered as
// CAST(atdetail.rid_parent AS TEXT) = '90446096', which can't use the index
// on rid_parent. Name is a citext column, which must never be cast to TEXT
// either (that would switch to case-sensitive matching and lose its index).
type atdetailModel struct {
	RidParent spectypes.SqlInt64 `json:"rid_parent" bun:"rid_parent"`
	Name      string             `json:"name" bun:"name,type:citext"`
}

func TestValidateAndAdjustFilterForColumnType_SqlNullNumeric(t *testing.T) {
	h := &Handler{}
	model := atdetailModel{}

	filter := &common.FilterOption{Column: "rid_parent", Operator: "eq", Value: "90446096"}
	info := h.ValidateAndAdjustFilterForColumnType(filter, model)

	if info.NeedsCast {
		t.Fatalf("expected NeedsCast=false for a numeric SqlInt64 column with a numeric value, got true")
	}
	if !info.IsNumericType {
		t.Fatalf("expected IsNumericType=true for a SqlInt64 column")
	}
	if v, ok := filter.Value.(int64); !ok || v != 90446096 {
		t.Fatalf("expected filter.Value to be converted to int64(90446096), got %#v", filter.Value)
	}
}

func TestApplyFilter_SqlNullNumeric_NoCastKeepsIndexUsable(t *testing.T) {
	h := &Handler{}
	model := atdetailModel{}

	filter := common.FilterOption{Column: "rid_parent", Operator: "eq", Value: "90446096"}
	castInfo := h.ValidateAndAdjustFilterForColumnType(&filter, model)

	q := &jsonCapQuery{}
	h.applyFilter(q, filter, "public.atdetail", castInfo.NeedsCast, "AND", model)

	c := q.only(t)
	const want = "atdetail.rid_parent = ?"
	if c.query != want {
		t.Fatalf("query = %q, want %q (must not CAST a numeric column to TEXT)", c.query, want)
	}
	if !reflect.DeepEqual(c.args, []interface{}{int64(90446096)}) {
		t.Fatalf("args = %#v", c.args)
	}
}

// TestFieldFilterHeader_SqlNullNumeric_EndToEnd reproduces the exact reported
// regression: a request carrying the header
//
//	x-fieldfilter-rid_parent: 90446096
//
// against a model whose rid_parent field is a nullable bigint (spectypes.SqlInt64).
// Before the fix, this parsed to a filter that got CAST(atdetail.rid_parent AS TEXT) = '90446096',
// making the query unable to use the index on rid_parent. It must now parse to
// a native "atdetail.rid_parent = ?" comparison with an int64 argument.
func TestFieldFilterHeader_SqlNullNumeric_EndToEnd(t *testing.T) {
	h := NewHandler(nil, nil)
	model := atdetailModel{}

	req := &MockRequest{
		headers: map[string]string{
			"x-fieldfilter-rid_parent": "90446096",
		},
		queryParams: map[string]string{},
	}

	options := h.parseOptionsFromHeaders(req, model)
	if len(options.Filters) != 1 {
		t.Fatalf("expected 1 filter parsed from x-fieldfilter-rid_parent, got %d: %+v", len(options.Filters), options.Filters)
	}

	filter := options.Filters[0]
	if filter.Column != "rid_parent" || filter.Operator != "eq" {
		t.Fatalf("unexpected parsed filter: %+v", filter)
	}
	if filter.Value != "90446096" {
		t.Fatalf("expected raw header string value before type validation, got %#v", filter.Value)
	}

	// This is the exact step that decided whether to CAST: ValidateAndAdjustFilterForColumnType
	// used to see reflect.Struct for the SqlInt64-wrapped column and cast to TEXT.
	castInfo := h.ValidateAndAdjustFilterForColumnType(&filter, model)
	if castInfo.NeedsCast {
		t.Fatalf("regression: numeric SqlInt64 column x-fieldfilter-rid_parent got NeedsCast=true, " +
			"which renders CAST(atdetail.rid_parent AS TEXT) = '90446096' and defeats the column's index")
	}

	q := &jsonCapQuery{}
	h.applyFilter(q, filter, "public.atdetail", castInfo.NeedsCast, filter.LogicOperator, model)

	c := q.only(t)
	const want = "atdetail.rid_parent = ?"
	if c.query != want {
		t.Fatalf("SQL condition = %q, want %q (no CAST, so the rid_parent index can still be used)", c.query, want)
	}
	if !reflect.DeepEqual(c.args, []interface{}{int64(90446096)}) {
		t.Fatalf("args = %#v, want [int64(90446096)]", c.args)
	}
}

func TestApplyFilter_Citext_NeverCastForEqOrIlike(t *testing.T) {
	h := &Handler{}
	model := atdetailModel{}

	t.Run("eq", func(t *testing.T) {
		filter := common.FilterOption{Column: "name", Operator: "eq", Value: "Acme"}
		castInfo := h.ValidateAndAdjustFilterForColumnType(&filter, model)
		if castInfo.NeedsCast {
			t.Fatalf("citext column must never need a CAST")
		}
		q := &jsonCapQuery{}
		h.applyFilter(q, filter, "public.atdetail", castInfo.NeedsCast, "AND", model)
		if c := q.only(t); c.query != "atdetail.name = ?" {
			t.Fatalf("query = %q", c.query)
		}
	})

	t.Run("ilike", func(t *testing.T) {
		filter := common.FilterOption{Column: "name", Operator: "ilike", Value: "%acme%"}
		q := &jsonCapQuery{}
		h.applyFilter(q, filter, "public.atdetail", false, "AND", model)
		if c := q.only(t); c.query != "atdetail.name ILIKE ?" {
			t.Fatalf("query = %q, want no CAST for a citext column", c.query)
		}
	})
}

// TestValidateAndAdjustFilterForColumnType_NumericColumn_Ilike reproduces a
// global "search all columns" request (x-searchor-contains-<col> per column,
// e.g. the X-Filter-All style OR group) landing an ILIKE filter with a
// '%...%'-wrapped numeric-looking value on a numeric column such as
// rid_parent. Before the fix, ValidateAndAdjustFilterForColumnType trimmed
// the '%' wildcards, saw a numeric string, and rewrote filter.Value to an
// int64 -- so applyFilter's CAST(col AS TEXT) ILIKE ? bound an integer
// argument instead of the wildcard string, and Postgres rejected it with
// "operator does not exist: text ~~* integer".
func TestValidateAndAdjustFilterForColumnType_NumericColumn_Ilike(t *testing.T) {
	h := &Handler{}
	model := atdetailModel{}

	filter := &common.FilterOption{Column: "rid_parent", Operator: "ilike", Value: "%345346346%"}
	info := h.ValidateAndAdjustFilterForColumnType(filter, model)

	if !info.NeedsCast {
		t.Fatalf("expected NeedsCast=true so the numeric column is cast to TEXT for ILIKE")
	}
	if filter.Value != "%345346346%" {
		t.Fatalf("ILIKE must keep the wildcard-wrapped string value untouched, got %#v", filter.Value)
	}

	q := &jsonCapQuery{}
	h.applyFilter(q, *filter, "public.atdetail", info.NeedsCast, "OR", model)

	c := q.only(t)
	const want = "CAST(atdetail.rid_parent AS TEXT) ILIKE ?"
	if c.query != want {
		t.Fatalf("query = %q, want %q", c.query, want)
	}
	if !reflect.DeepEqual(c.args, []interface{}{"%345346346%"}) {
		t.Fatalf("args = %#v, want [\"%%345346346%%\"]", c.args)
	}
}
