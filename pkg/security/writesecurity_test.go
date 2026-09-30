package security

import (
	"context"
	"reflect"
	"testing"
)

type wsCtx struct {
	data  interface{}
	model interface{}
	user  bool
}

func (c *wsCtx) GetContext() context.Context {
	if c.user {
		return context.WithValue(context.Background(), UserIDKey, 7)
	}
	return context.Background()
}
func (c *wsCtx) GetUserID() (int, bool)  { return 7, c.user }
func (c *wsCtx) GetUserRef() (any, bool) { return 7, c.user }
func (c *wsCtx) GetSchema() string       { return "public" }
func (c *wsCtx) GetEntity() string       { return "items" }
func (c *wsCtx) GetModel() interface{}   { return c.model }
func (c *wsCtx) GetQuery() interface{}   { return nil }
func (c *wsCtx) SetQuery(interface{})    {}
func (c *wsCtx) GetResult() interface{}  { return nil }
func (c *wsCtx) SetResult(interface{})   {}
func (c *wsCtx) GetData() interface{}    { return c.data }
func (c *wsCtx) SetData(d interface{})   { c.data = d }

type wsModel struct {
	ID    int    `json:"id" bun:"id,pk"`
	Name  string `json:"name" bun:"name"`
	Email string `json:"email" gorm:"column:email_addr"`
	Other string `json:"other" bun:"other"`
}

func wsList(rules ...ColumnSecurity) *SecurityList {
	l := &SecurityList{ColumnSecurity: map[string][]ColumnSecurity{"public.items@7": rules}}
	if rules == nil {
		l.ColumnSecurity["public.items@7"] = []ColumnSecurity{}
	}
	return l
}

func TestApplyWriteColumnSecurityStripsHiddenAndMasked(t *testing.T) {
	list := wsList(
		ColumnSecurity{Path: []string{"Name"}, Accesstype: "hide"},
		ColumnSecurity{Path: []string{"email_addr"}, Accesstype: "mask"},
		ColumnSecurity{Path: []string{"other"}, Accesstype: "allow"},
		ColumnSecurity{Path: []string{"id", "sub"}, Accesstype: "hide"},
	)
	tests := map[string]struct{ in, want interface{} }{
		"map": {
			map[string]interface{}{"id": 1, "name": "a", "email": "e", "other": "o"},
			map[string]interface{}{"id": 1, "other": "o"},
		},
		"slice": {
			[]interface{}{map[string]interface{}{"NAME": "a", "id": 1}, map[string]interface{}{"email_addr": "e"}},
			[]interface{}{map[string]interface{}{"id": 1}, map[string]interface{}{}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := &wsCtx{data: tc.in, model: &wsModel{}, user: true}
			if err := ApplyWriteColumnSecurity(c, list); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.data, tc.want) {
				t.Fatalf("got %v want %v", c.data, tc.want)
			}
		})
	}
}

func TestApplyWriteColumnSecurityNoRulesKeepsPayload(t *testing.T) {
	c := &wsCtx{data: map[string]interface{}{"name": "a"}, model: &wsModel{}, user: true}
	if err := ApplyWriteColumnSecurity(c, wsList()); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.data.(map[string]interface{})["name"]; !ok {
		t.Fatal("payload changed without rules")
	}
}

func TestApplyWriteColumnSecurityFailsClosedWhenRulesNotLoaded(t *testing.T) {
	c := &wsCtx{data: map[string]interface{}{"name": "a"}, model: &wsModel{}, user: true}
	if err := ApplyWriteColumnSecurity(c, &SecurityList{}); err == nil {
		t.Fatal("expected an error when rules are not loaded")
	}
}

func TestApplyWriteColumnSecuritySkipsWithoutUser(t *testing.T) {
	c := &wsCtx{data: map[string]interface{}{"name": "a"}, model: &wsModel{}}
	if err := ApplyWriteColumnSecurity(c, &SecurityList{}); err != nil {
		t.Fatal(err)
	}
}
