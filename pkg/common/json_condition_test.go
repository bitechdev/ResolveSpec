package common

import (
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

type jsonCondModel struct {
	ID   int64              `json:"id"`
	Name string             `json:"name"`
	Data spectypes.SqlJSONB `json:"data"`
}

func TestResolveJSONColumnRef_Gate(t *testing.T) {
	m := jsonCondModel{}

	// Explicit operator syntax needs no model confirmation.
	if _, ok := ResolveJSONColumnRef(m, "data->>'city'"); !ok {
		t.Error("arrow syntax should resolve")
	}
	// Dotted shorthand on a real JSON column resolves.
	if ref, ok := ResolveJSONColumnRef(m, "data.city"); !ok || !reflect.DeepEqual(ref.Path, []string{"city"}) {
		t.Errorf("dotted shorthand on JSON column should resolve, got ok=%v ref=%+v", ok, ref)
	}
	// Dotted shorthand on a non-JSON column must NOT be treated as JSON.
	if _, ok := ResolveJSONColumnRef(m, "name.first"); ok {
		t.Error("dotted shorthand on non-JSON column must not resolve as JSON")
	}
	// Plain columns never resolve.
	if _, ok := ResolveJSONColumnRef(m, "name"); ok {
		t.Error("plain column must not resolve")
	}
}

func TestResolveJSONColumnExpr(t *testing.T) {
	m := jsonCondModel{}

	expr, args, alias, ok := ResolveJSONColumnExpr(m, "t", "data->'addr'->>'city'")
	if !ok {
		t.Fatal("expected ok")
	}
	if expr != `("t"."data" #>> ?::text[])` {
		t.Errorf("expr = %q", expr)
	}
	if !reflect.DeepEqual(args, []interface{}{"{addr,city}"}) {
		t.Errorf("args = %#v", args)
	}
	if alias != "data_addr_city" {
		t.Errorf("alias = %q", alias)
	}

	if _, _, _, ok := ResolveJSONColumnExpr(m, "t", "name"); ok {
		t.Error("plain column must not resolve")
	}
}

func TestBuildJSONFilterCondition(t *testing.T) {
	m := jsonCondModel{}

	tests := []struct {
		name     string
		token    string
		operator string
		value    interface{}
		wantCond string
		wantArgs []interface{}
	}{
		{
			name: "eq stays text", token: "data->>'city'", operator: "eq", value: "LA",
			wantCond: `("data" #>> ?::text[]) = ?`,
			wantArgs: []interface{}{"{city}", "LA"},
		},
		{
			name: "gt numeric value infers numeric cast", token: "data->>'age'", operator: "gt", value: 18,
			wantCond: `(("data" #>> ?::text[]))::numeric > ?`,
			wantArgs: []interface{}{"{age}", 18},
		},
		{
			name: "gt non-numeric value stays text", token: "data->>'name'", operator: "gt", value: "m",
			wantCond: `("data" #>> ?::text[]) > ?`,
			wantArgs: []interface{}{"{name}", "m"},
		},
		{
			name: "explicit cast is respected for lt", token: "data->>'ts'::timestamptz", operator: "lt", value: "2020-01-01",
			wantCond: `(("data" #>> ?::text[]))::timestamptz < ?`,
			wantArgs: []interface{}{"{ts}", "2020-01-01"},
		},
		{
			name: "ilike", token: "data->>'city'", operator: "ilike", value: "%la%",
			wantCond: `("data" #>> ?::text[]) ILIKE ?`,
			wantArgs: []interface{}{"{city}", "%la%"},
		},
		{
			name: "in", token: "data->>'tier'", operator: "in", value: []string{"a", "b"},
			wantCond: `("data" #>> ?::text[]) IN (?,?)`,
			wantArgs: []interface{}{"{tier}", "a", "b"},
		},
		{
			name: "between numeric", token: "data->>'age'", operator: "between", value: []interface{}{10, 20},
			wantCond: `((("data" #>> ?::text[]))::numeric > ? AND (("data" #>> ?::text[]))::numeric < ?)`,
			wantArgs: []interface{}{"{age}", 10, "{age}", 20},
		},
		{
			name: "is_null", token: "data->>'city'", operator: "is_null", value: nil,
			wantCond: `("data" #>> ?::text[]) IS NULL`,
			wantArgs: []interface{}{"{city}"},
		},
		{
			name: "hash path", token: "data#>>'{a,b}'", operator: "eq", value: "x",
			wantCond: `("data" #>> ?::text[]) = ?`,
			wantArgs: []interface{}{"{a,b}", "x"},
		},
		{
			name: "dotted shorthand on json column", token: "data.city", operator: "eq", value: "x",
			wantCond: `("data" #>> ?::text[]) = ?`,
			wantArgs: []interface{}{"{city}", "x"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cond, args, ok := BuildJSONFilterCondition(m, "", tc.token, tc.operator, tc.value)
			if !ok {
				t.Fatalf("ok=false for %q", tc.token)
			}
			if cond != tc.wantCond {
				t.Errorf("cond = %q, want %q", cond, tc.wantCond)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}

func TestBuildJSONFilterCondition_NotJSON(t *testing.T) {
	m := jsonCondModel{}
	for _, tok := range []string{"name", "id", "name.first"} {
		if _, _, ok := BuildJSONFilterCondition(m, "", tok, "eq", "x"); ok {
			t.Errorf("BuildJSONFilterCondition(%q) ok=true, want false", tok)
		}
	}
	// Unknown operator on a real JSON ref -> caller keeps its own handling.
	if _, _, ok := BuildJSONFilterCondition(m, "", "data->>'x'", "st_intersects", "y"); ok {
		t.Error("unknown operator must yield ok=false")
	}
}

func TestBuildJSONFilterCondition_QualifiedAndInjectionSafe(t *testing.T) {
	m := jsonCondModel{}
	// A hostile key never reaches the SQL string — it is bound in the text[] arg.
	cond, args, ok := BuildJSONFilterCondition(m, "pub.tbl", "data->>'ev\"il'", "eq", "x")
	if !ok {
		t.Fatal("ok=false")
	}
	if cond != `("pub"."tbl"."data" #>> ?::text[]) = ?` {
		t.Errorf("cond = %q", cond)
	}
	if !reflect.DeepEqual(args, []interface{}{`{"ev\"il"}`, "x"}) {
		t.Errorf("args = %#v", args)
	}
}
