package restheadspec

import (
	"context"
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

// jsonColModel exercises the JSON-column wiring: Data is a real JSONB column so
// the dotted "data.x" shorthand is recognised as JSON access.
type jsonColModel struct {
	ID   int64              `json:"id" bun:"id,pk"`
	Name string             `json:"name" bun:"name"`
	Data spectypes.SqlJSONB `json:"data" bun:"data"`
}

// jsonCapQuery is a minimal common.SelectQuery that records the string + args of
// the calls the handler makes so a test can assert on them.
type jsonCapQuery struct {
	calls []jsonCapCall
}

type jsonCapCall struct {
	method string
	query  string
	args   []interface{}
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
func (m *jsonCapQuery) WhereIn(col string, values interface{}) common.SelectQuery {
	return m.rec("WhereIn", col, []interface{}{values})
}
func (m *jsonCapQuery) Order(o string) common.SelectQuery { return m.rec("Order", o, nil) }
func (m *jsonCapQuery) OrderExpr(o string, args ...interface{}) common.SelectQuery {
	return m.rec("OrderExpr", o, args)
}
func (m *jsonCapQuery) Limit(int) common.SelectQuery                       { return m }
func (m *jsonCapQuery) Offset(int) common.SelectQuery                      { return m }
func (m *jsonCapQuery) Join(string, ...interface{}) common.SelectQuery     { return m }
func (m *jsonCapQuery) LeftJoin(string, ...interface{}) common.SelectQuery { return m }
func (m *jsonCapQuery) Group(string) common.SelectQuery                    { return m }
func (m *jsonCapQuery) Having(string, ...interface{}) common.SelectQuery   { return m }
func (m *jsonCapQuery) Preload(string, ...interface{}) common.SelectQuery  { return m }
func (m *jsonCapQuery) PreloadRelation(string, ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	return m
}
func (m *jsonCapQuery) JoinRelation(string, ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	return m
}
func (m *jsonCapQuery) Scan(context.Context, interface{}) error { return nil }
func (m *jsonCapQuery) ScanModel(context.Context) error         { return nil }
func (m *jsonCapQuery) Count(context.Context) (int, error)      { return 0, nil }
func (m *jsonCapQuery) Exists(context.Context) (bool, error)    { return false, nil }
func (m *jsonCapQuery) GetUnderlyingQuery() interface{}         { return nil }
func (m *jsonCapQuery) GetModel() interface{}                   { return nil }

func (m *jsonCapQuery) only(t *testing.T) jsonCapCall {
	t.Helper()
	if len(m.calls) != 1 {
		t.Fatalf("expected exactly 1 recorded call, got %d: %+v", len(m.calls), m.calls)
	}
	return m.calls[0]
}

func TestApplyFilter_JSONColumn(t *testing.T) {
	h := &Handler{}
	model := jsonColModel{}

	t.Run("arrow syntax eq", func(t *testing.T) {
		q := &jsonCapQuery{}
		h.applyFilter(q, common.FilterOption{
			Column: "data->>'city'", Operator: "eq", Value: "LA",
		}, "public.things", false, "AND", model)
		c := q.only(t)
		if c.method != "Where" || c.query != `("things"."data" #>> ?::text[]) = ?` {
			t.Fatalf("call = %+v", c)
		}
		if !reflect.DeepEqual(c.args, []interface{}{"{city}", "LA"}) {
			t.Fatalf("args = %#v", c.args)
		}
	})

	t.Run("dotted shorthand with numeric cast inference, OR logic", func(t *testing.T) {
		q := &jsonCapQuery{}
		h.applyFilter(q, common.FilterOption{
			Column: "data.age", Operator: "gt", Value: 18,
		}, "public.things", false, "OR", model)
		c := q.only(t)
		if c.method != "WhereOr" || c.query != `(("things"."data" #>> ?::text[]))::numeric > ?` {
			t.Fatalf("call = %+v", c)
		}
		if !reflect.DeepEqual(c.args, []interface{}{"{age}", 18}) {
			t.Fatalf("args = %#v", c.args)
		}
	})

	t.Run("non-JSON column is untouched", func(t *testing.T) {
		q := &jsonCapQuery{}
		h.applyFilter(q, common.FilterOption{
			Column: "name", Operator: "eq", Value: "x",
		}, "public.things", false, "AND", model)
		c := q.only(t)
		if c.query != "things.name = ?" {
			t.Fatalf("call = %+v", c)
		}
	})

	t.Run("nil model: explicit syntax still works, dotted does not", func(t *testing.T) {
		q := &jsonCapQuery{}
		h.applyFilter(q, common.FilterOption{
			Column: "data->>'city'", Operator: "eq", Value: "LA",
		}, "public.things", false, "AND", nil)
		if c := q.only(t); c.query != `("things"."data" #>> ?::text[]) = ?` {
			t.Fatalf("explicit call = %+v", c)
		}

		q2 := &jsonCapQuery{}
		h.applyFilter(q2, common.FilterOption{
			Column: "data.city", Operator: "eq", Value: "LA",
		}, "public.things", false, "AND", nil)
		if c := q2.only(t); c.query == `("things"."data" #>> ?::text[]) = ?` {
			t.Fatalf("dotted shorthand should not resolve without a model: %+v", c)
		}
	})
}
