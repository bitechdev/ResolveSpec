package resolvespec

import (
	"context"
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

// jsonColModel has a real JSONB column so the dotted "data.x" shorthand is
// recognised as JSON access.
type jsonColModel struct {
	ID   int64              `json:"id" bun:"id,pk"`
	Name string             `json:"name" bun:"name"`
	Data spectypes.SqlJSONB `json:"data" bun:"data"`
}

type jsonCapCall struct {
	method string
	query  string
	args   []interface{}
}

// jsonCapQuery records the string + args of the calls the handler makes.
type jsonCapQuery struct {
	calls []jsonCapCall
}

func (m *jsonCapQuery) rec(method, query string, args []interface{}) common.SelectQuery {
	m.calls = append(m.calls, jsonCapCall{method: method, query: query, args: args})
	return m
}

func (m *jsonCapQuery) Model(interface{}) common.SelectQuery { return m }
func (m *jsonCapQuery) Table(string) common.SelectQuery      { return m }
func (m *jsonCapQuery) Column(cols ...string) common.SelectQuery {
	for _, c := range cols {
		m.rec("Column", c, nil)
	}
	return m
}
func (m *jsonCapQuery) ColumnExpr(q string, args ...interface{}) common.SelectQuery {
	return m.rec("ColumnExpr", q, args)
}
func (m *jsonCapQuery) Where(q string, args ...interface{}) common.SelectQuery {
	return m.rec("Where", q, args)
}
func (m *jsonCapQuery) WhereOr(q string, args ...interface{}) common.SelectQuery {
	return m.rec("WhereOr", q, args)
}
func (m *jsonCapQuery) Join(string, ...interface{}) common.SelectQuery     { return m }
func (m *jsonCapQuery) LeftJoin(string, ...interface{}) common.SelectQuery { return m }
func (m *jsonCapQuery) Preload(string, ...interface{}) common.SelectQuery  { return m }
func (m *jsonCapQuery) PreloadRelation(string, ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	return m
}
func (m *jsonCapQuery) JoinRelation(string, ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	return m
}
func (m *jsonCapQuery) Order(o string) common.SelectQuery { return m.rec("Order", o, nil) }
func (m *jsonCapQuery) OrderExpr(o string, args ...interface{}) common.SelectQuery {
	return m.rec("OrderExpr", o, args)
}
func (m *jsonCapQuery) Limit(int) common.SelectQuery                     { return m }
func (m *jsonCapQuery) Offset(int) common.SelectQuery                    { return m }
func (m *jsonCapQuery) Group(string) common.SelectQuery                  { return m }
func (m *jsonCapQuery) Having(string, ...interface{}) common.SelectQuery { return m }
func (m *jsonCapQuery) Scan(context.Context, interface{}) error          { return nil }
func (m *jsonCapQuery) ScanModel(context.Context) error                  { return nil }
func (m *jsonCapQuery) Count(context.Context) (int, error)               { return 0, nil }
func (m *jsonCapQuery) Exists(context.Context) (bool, error)             { return false, nil }

func (m *jsonCapQuery) only(t *testing.T) jsonCapCall {
	t.Helper()
	if len(m.calls) != 1 {
		t.Fatalf("expected exactly 1 recorded call, got %d: %+v", len(m.calls), m.calls)
	}
	return m.calls[0]
}

func TestBuildFilterCondition_JSONColumn(t *testing.T) {
	h := &Handler{}
	model := jsonColModel{}

	tests := []struct {
		name     string
		filter   common.FilterOption
		wantCond string
		wantArgs []interface{}
	}{
		{
			name:     "arrow syntax eq stays text",
			filter:   common.FilterOption{Column: "data->>'city'", Operator: "eq", Value: "LA"},
			wantCond: `("data" #>> ?::text[]) = ?`,
			wantArgs: []interface{}{"{city}", "LA"},
		},
		{
			name:     "dotted shorthand numeric cast inference",
			filter:   common.FilterOption{Column: "data.age", Operator: "gt", Value: 18},
			wantCond: `(("data" #>> ?::text[]))::numeric > ?`,
			wantArgs: []interface{}{"{age}", 18},
		},
		{
			name:     "hash path with explicit cast",
			filter:   common.FilterOption{Column: "data#>>'{a,b}'::int", Operator: "lte", Value: "5"},
			wantCond: `(("data" #>> ?::text[]))::integer <= ?`,
			wantArgs: []interface{}{"{a,b}", "5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cond, args := h.buildFilterCondition(tc.filter, model)
			if cond != tc.wantCond {
				t.Fatalf("cond = %q, want %q", cond, tc.wantCond)
			}
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tc.wantArgs)
			}
		})
	}

	// Non-JSON column falls through to ordinary handling.
	cond, _ := h.buildFilterCondition(common.FilterOption{Column: "name", Operator: "eq", Value: "x"}, model)
	if cond != "name = ?" {
		t.Fatalf("non-JSON cond = %q", cond)
	}

	// Without a model the dotted shorthand must NOT be treated as JSON.
	cond, _ = h.buildFilterCondition(common.FilterOption{Column: "data.age", Operator: "eq", Value: "x"}, nil)
	if cond != "data.age = ?" {
		t.Fatalf("nil-model dotted cond = %q, want ordinary handling", cond)
	}
}

func TestApplyFilter_JSONColumn(t *testing.T) {
	h := &Handler{}
	model := jsonColModel{}

	q := &jsonCapQuery{}
	h.applyFilter(q, common.FilterOption{
		Column: "data->>'tier'", Operator: "in", Value: []string{"a", "b"}, LogicOperator: "OR",
	}, model)
	c := q.only(t)
	if c.method != "WhereOr" || c.query != `("data" #>> ?::text[]) IN (?,?)` {
		t.Fatalf("call = %+v", c)
	}
	if !reflect.DeepEqual(c.args, []interface{}{"{tier}", "a", "b"}) {
		t.Fatalf("args = %#v", c.args)
	}
}
