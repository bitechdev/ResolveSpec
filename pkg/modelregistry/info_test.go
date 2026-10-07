package modelregistry

import (
	"reflect"
	"strings"
	"testing"
)

type infoModel struct {
	ID    int    `json:"id" gorm:"primaryKey;comment:Row id"`
	Email string `json:"email" bun:"email,comment:Login address"`
	Name  string `json:"name" note:"Display name"`
	Plain string `json:"plain"`
}

func (infoModel) ModelDescription() string { return " From the model " }

func TestFieldComment(t *testing.T) {
	typ := reflect.TypeOf(infoModel{})
	want := map[string]string{"ID": "Row id", "Email": "Login address", "Name": "Display name", "Plain": ""}
	for field, exp := range want {
		sf, _ := typ.FieldByName(field)
		if got := FieldComment(sf); got != exp {
			t.Errorf("%s = %q, want %q", field, got, exp)
		}
	}
}

func TestModelInfoPrecedence(t *testing.T) {
	r := NewModelRegistry()
	if err := r.RegisterModel("public.items", infoModel{}); err != nil {
		t.Fatal(err)
	}
	if got := r.ResolveModelInfo("public.items").Description; got != "From the model" {
		t.Errorf("describer fallback = %q", got)
	}

	n, err := r.LoadModelInfo(strings.NewReader(
		`{"public.items":{"description":"From file","tags":["a"],"columns":{"email":"Mail"}},"public.later":{"purpose":"p"}}`))
	if err != nil || n != 2 {
		t.Fatalf("load n=%d err=%v", n, err)
	}
	info := r.ResolveModelInfo("public.items")
	if info.Description != "From file" || info.Columns["email"] != "Mail" || len(info.Tags) != 1 {
		t.Errorf("file info = %+v", info)
	}
	if _, ok := r.GetModelInfo("public.later"); !ok {
		t.Error("info for a not-yet-registered model must be kept")
	}

	// returned info is a copy
	info.Columns["email"] = "changed"
	if got, _ := r.GetModelInfo("public.items"); got.Columns["email"] != "Mail" {
		t.Error("GetModelInfo leaked internal map")
	}
}

func TestLoadModelInfoRejectsBadInput(t *testing.T) {
	r := NewModelRegistry()
	for _, in := range []string{`not json`, `{"a":{"descripton":"typo"}}`} {
		if _, err := r.LoadModelInfo(strings.NewReader(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
	if _, err := r.LoadModelInfoFile("/nonexistent/x.json"); err == nil {
		t.Error("expected error for missing file")
	}
}
