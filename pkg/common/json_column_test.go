package common

import (
	"reflect"
	"testing"
)

func TestParseColumnRef_Valid(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		base      string
		path      []string
		asText    bool
		cast      string
		alias     string
		ambiguous bool
	}{
		{
			name:  "arrow text extraction",
			input: "data->>'city'",
			base:  "data", path: []string{"city"}, asText: true,
		},
		{
			name:  "arrow whitespace tolerant",
			input: "data ->> 'city'",
			base:  "data", path: []string{"city"}, asText: true,
		},
		{
			name:  "nested arrow chain",
			input: "data->'address'->>'city'",
			base:  "data", path: []string{"address", "city"}, asText: true,
		},
		{
			name:  "arrow jsonb result",
			input: "data->'address'",
			base:  "data", path: []string{"address"}, asText: false,
		},
		{
			name:  "arrow array index",
			input: "items->0->>'name'",
			base:  "items", path: []string{"0", "name"}, asText: true,
		},
		{
			name:  "hash path text",
			input: "data#>>'{address,city}'",
			base:  "data", path: []string{"address", "city"}, asText: true,
		},
		{
			name:  "hash path jsonb",
			input: "data#>'{address,city}'",
			base:  "data", path: []string{"address", "city"}, asText: false,
		},
		{
			name:  "dotted shorthand",
			input: "data.address.city",
			base:  "data", path: []string{"address", "city"}, asText: true, ambiguous: true,
		},
		{
			name:  "trailing cast",
			input: "data->>'age'::int",
			base:  "data", path: []string{"age"}, asText: true, cast: "integer",
		},
		{
			name:  "cast normalises",
			input: "data->>'ts'::timestamptz",
			base:  "data", path: []string{"ts"}, asText: true, cast: "timestamptz",
		},
		{
			name:  "parenthesised with alias",
			input: "(data->>'city') AS city_name",
			base:  "data", path: []string{"city"}, asText: true, alias: "city_name",
		},
		{
			name:  "paren wrap and cast",
			input: "(data->>'age')::numeric",
			base:  "data", path: []string{"age"}, asText: true, cast: "numeric",
		},
		{
			name:  "quoted key with spaces",
			input: "data->>'key with space'",
			base:  "data", path: []string{"key with space"}, asText: true,
		},
		{
			name:  "quoted key with escaped quote",
			input: "data->>'o''brien'",
			base:  "data", path: []string{"o'brien"}, asText: true,
		},
		{
			name:  "relation column is ambiguous json",
			input: "orders.total",
			base:  "orders", path: []string{"total"}, asText: true, ambiguous: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok := ParseColumnRef(tc.input)
			if !ok {
				t.Fatalf("ParseColumnRef(%q) returned ok=false", tc.input)
			}
			if ref.Base != tc.base {
				t.Errorf("Base = %q, want %q", ref.Base, tc.base)
			}
			if !reflect.DeepEqual(ref.Path, tc.path) {
				t.Errorf("Path = %#v, want %#v", ref.Path, tc.path)
			}
			if ref.AsText != tc.asText {
				t.Errorf("AsText = %v, want %v", ref.AsText, tc.asText)
			}
			if ref.Cast != tc.cast {
				t.Errorf("Cast = %q, want %q", ref.Cast, tc.cast)
			}
			if ref.Alias != tc.alias {
				t.Errorf("Alias = %q, want %q", ref.Alias, tc.alias)
			}
			if ref.Ambiguous != tc.ambiguous {
				t.Errorf("Ambiguous = %v, want %v", ref.Ambiguous, tc.ambiguous)
			}
		})
	}
}

func TestParseColumnRef_NotJSON(t *testing.T) {
	// These must return ok=false so callers fall back to their normal handling.
	inputs := []string{
		"",
		"   ",
		"name",
		"data",
		"created_at",
		"(id)",
	}
	for _, in := range inputs {
		if ref, ok := ParseColumnRef(in); ok {
			t.Errorf("ParseColumnRef(%q) = %+v, ok=true; want ok=false", in, ref)
		}
	}
}

func TestParseColumnRef_Rejected(t *testing.T) {
	// Malformed or unsafe tokens must be rejected outright.
	inputs := []string{
		"data->>'x'::bogus",                     // cast not on allowlist
		"data->>'x' AS 1bad",                    // invalid alias
		"(data->>'a') OR (x->>'b')",             // not a single wrapped expr
		"data->>'x'); DROP TABLE users; --",     // injection attempt
		"data->b",                               // unquoted non-numeric key
		"data->>''",                             // empty key
		"data#>>'{}'",                           // empty hash path
		"data#>>address",                        // hash path not a quoted literal
		"weird col->>'x'",                       // base not an identifier
		"data.address.city.but.way.too...deep.", // trailing dot -> empty segment
	}
	for _, in := range inputs {
		if ref, ok := ParseColumnRef(in); ok {
			t.Errorf("ParseColumnRef(%q) = %+v, ok=true; want rejected", in, ref)
		}
	}
}

func TestColumnRef_SQL(t *testing.T) {
	tests := []struct {
		name     string
		ref      ColumnRef
		alias    string
		wantExpr string
		wantArgs []interface{}
	}{
		{
			name:     "text extraction qualified",
			ref:      ColumnRef{Base: "data", Path: []string{"address", "city"}, AsText: true},
			alias:    "u",
			wantExpr: `("u"."data" #>> ?::text[])`,
			wantArgs: []interface{}{"{address,city}"},
		},
		{
			name:     "jsonb extraction unqualified",
			ref:      ColumnRef{Base: "data", Path: []string{"a"}, AsText: false},
			alias:    "",
			wantExpr: `("data" #> ?::text[])`,
			wantArgs: []interface{}{"{a}"},
		},
		{
			name:     "with cast",
			ref:      ColumnRef{Base: "data", Path: []string{"age"}, AsText: true, Cast: "integer"},
			alias:    "t",
			wantExpr: `(("t"."data" #>> ?::text[]))::integer`,
			wantArgs: []interface{}{"{age}"},
		},
		{
			name:     "schema qualified alias",
			ref:      ColumnRef{Base: "data", Path: []string{"k"}, AsText: true},
			alias:    "public.users",
			wantExpr: `("public"."users"."data" #>> ?::text[])`,
			wantArgs: []interface{}{"{k}"},
		},
		{
			name:     "key needing quoting",
			ref:      ColumnRef{Base: "data", Path: []string{"key with space", `ev"il`}, AsText: true},
			alias:    "",
			wantExpr: `("data" #>> ?::text[])`,
			wantArgs: []interface{}{`{"key with space","ev\"il"}`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, args := tc.ref.SQL(tc.alias)
			if expr != tc.wantExpr {
				t.Errorf("expr = %q, want %q", expr, tc.wantExpr)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}
}

func TestColumnRef_SQL_RoundTrip(t *testing.T) {
	ref, ok := ParseColumnRef("profile->'contact'->>'email'")
	if !ok {
		t.Fatal("parse failed")
	}
	expr, args := ref.SQL("customers")
	wantExpr := `("customers"."profile" #>> ?::text[])`
	if expr != wantExpr {
		t.Errorf("expr = %q, want %q", expr, wantExpr)
	}
	if len(args) != 1 || args[0] != "{contact,email}" {
		t.Errorf("args = %#v, want [{contact,email}]", args)
	}
}

func TestColumnRef_OutputAlias(t *testing.T) {
	cases := []struct {
		ref  ColumnRef
		want string
	}{
		{ColumnRef{Base: "data", Path: []string{"address", "city"}, AsText: true}, "data_address_city"},
		{ColumnRef{Base: "data", Path: []string{"city"}, Alias: "city"}, "city"},
		{ColumnRef{Base: "data", Path: []string{"weird key"}}, "data_weird_key"},
		{ColumnRef{Base: "data"}, "data"},
	}
	for _, c := range cases {
		if got := c.ref.OutputAlias(); got != c.want {
			t.Errorf("OutputAlias(%+v) = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestNormalizeCastTarget(t *testing.T) {
	ok := map[string]string{
		"int":         "integer",
		"INT":         "integer",
		" bigint ":    "bigint",
		"decimal":     "numeric",
		"float8":      "double precision",
		"bool":        "boolean",
		"timestamptz": "timestamptz",
		"uuid":        "uuid",
	}
	for in, want := range ok {
		got, allowed := NormalizeCastTarget(in)
		if !allowed || got != want {
			t.Errorf("NormalizeCastTarget(%q) = %q, %v; want %q, true", in, got, allowed, want)
		}
	}
	for _, in := range []string{"", "regclass", "int; drop", "text[]"} {
		if got, allowed := NormalizeCastTarget(in); allowed {
			t.Errorf("NormalizeCastTarget(%q) = %q, true; want not allowed", in, got)
		}
	}
}
