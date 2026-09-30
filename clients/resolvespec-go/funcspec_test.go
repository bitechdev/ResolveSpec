package resolvespec

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildHeadersFilters(t *testing.T) {
	got := BuildHeaders(&FuncSpecOptions{Filters: []FilterOption{
		{Column: "status", Operator: "eq", Value: "active"},
		{Column: "age", Operator: "gte", Value: 18},
		{Column: "name", Operator: "contains", Value: "x", LogicOperator: "OR"},
		{Column: "deleted", Operator: "is_null"},
		{Column: "id", Operator: "in", Value: []int{1, 2}},
		{Column: "p", Operator: "between_inclusive", Value: []any{1, 5}},
	}})
	want := map[string]string{
		"X-FieldFilter-status":              "active",
		"X-SearchOp-greaterthanorequal-age": "18",
		"X-SearchOr-contains-name":          "x",
		"X-SearchOp-empty-deleted":          "",
		"X-SearchOp-in-id":                  "1,2",
		"X-SearchOp-betweeninclusive-p":     "1,5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestBuildHeadersMisc(t *testing.T) {
	got := BuildHeaders(&FuncSpecOptions{
		SearchFilters: map[string]string{"name": "bob"}, CustomSQLWhere: "a = 1", CustomSQLOr: "b = 2",
		Sort:  []SortOption{{"name", "asc"}, {"created_at", "DESC"}},
		Limit: Int(5), Offset: Int(10), Distinct: Bool(true), SkipCount: Bool(true), SkipCache: Bool(false),
		ResponseFormat: "syncfusion",
	})
	want := map[string]string{
		"X-SearchFilter-name": "bob", "X-Custom-SQL-W": "a = 1", "X-Custom-SQL-Or": "b = 2",
		"X-Sort": "name ASC,created_at DESC", "X-Limit": "5", "X-Offset": "10", "X-Distinct": "true",
		"X-SkipCount": "true", "X-SkipCache": "false", "X-Syncfusion": "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestEncodeUnsafe(t *testing.T) {
	h := BuildHeaders(&FuncSpecOptions{Filters: []FilterOption{{Column: "n", Operator: "eq", Value: "héllo"}, {Column: "m", Operator: "eq", Value: " pad"}}})
	for _, k := range []string{"X-FieldFilter-n", "X-FieldFilter-m"} {
		if len(h[k]) < 4 || h[k][:4] != "ZIP_" {
			t.Fatalf("%s=%q", k, h[k])
		}
	}
	if DecodeHeaderValue(h["X-FieldFilter-n"]) != "héllo" || DecodeHeaderValue(h["X-FieldFilter-m"]) != " pad" {
		t.Fatal("roundtrip")
	}
}

func TestBuildQuery(t *testing.T) {
	q := BuildQuery(Params{"a": true, "b": []string{"x", "y"}, "c": nil, "d": 3})
	if q.Get("a") != "true" || !reflect.DeepEqual(q["b"], []string{"x", "y"}) || q.Has("c") || q.Get("d") != "3" {
		t.Fatalf("%v", q)
	}
}

func TestQueryListMetadata(t *testing.T) {
	srv, s := server(t, 206, `[{"id":1},{"id":2}]`, map[string]string{"Content-Range": "items 10-12/50"})
	c := NewFuncSpecClient(srv.URL, WithToken("tok"))
	resp, err := c.QueryList(context.Background(), "/api/users", Params{"org": 1}, &FuncSpecOptions{Limit: Int(2)})
	if err != nil {
		t.Fatal(err)
	}
	if s.method != "GET" || s.path != "/api/users?org=1" || s.header.Get("X-Limit") != "2" {
		t.Fatalf("%s %v", s.path, s.header)
	}
	m := resp.Metadata
	if m.Total != 50 || m.Count != 2 || m.Offset != 10 || m.Limit != 2 || m.Filtered != 50 {
		t.Fatalf("%+v", m)
	}
	var rows []map[string]any
	if err := resp.Decode(&rows); err != nil || len(rows) != 2 {
		t.Fatal(err)
	}
}

func TestQuerySingleNoMetadataAndError(t *testing.T) {
	srv, _ := server(t, 200, `{"id":1}`, nil)
	resp, err := NewFuncSpecClient(srv.URL).Query(context.Background(), "api/u", nil, nil)
	if err != nil || resp.Metadata != nil {
		t.Fatalf("%v %v", resp, err)
	}
	srv2, _ := server(t, 400, `{"success":false,"error":{"code":"hook_error","message":"Hook execution failed","detail":"authentication required"}}`, nil)
	_, err = NewFuncSpecClient(srv2.URL).Query(context.Background(), "api/u", nil, nil)
	if e := err.(*Error); e.Code != "hook_error" || e.Detail != "authentication required" {
		t.Fatalf("%#v", e)
	}
}
