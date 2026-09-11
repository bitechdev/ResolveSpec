package reflection

import (
	"encoding/json"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

type jsonColModel struct {
	ID       int64                  `json:"id"`
	Name     string                 `json:"name"`
	Meta     spectypes.SqlJSONB     `json:"meta"`
	Raw      json.RawMessage        `json:"raw"`
	Attrs    map[string]interface{} `json:"attrs"`
	Config   []byte                 `json:"config" bun:"config,type:jsonb"`
	Settings string                 `json:"settings" gorm:"column:settings;type:json"`
	Blob     []byte                 `json:"blob"`
}

func TestIsJSONColumn(t *testing.T) {
	m := jsonColModel{}

	jsonCols := []string{"meta", "raw", "attrs", "config", "settings"}
	for _, c := range jsonCols {
		if !IsJSONColumn(m, c) {
			t.Errorf("expected %q to be a JSON column", c)
		}
	}

	notJSON := []string{"id", "name", "blob", "missing"}
	for _, c := range notJSON {
		if IsJSONColumn(m, c) {
			t.Errorf("expected %q NOT to be a JSON column", c)
		}
	}

	if IsJSONColumn(nil, "meta") {
		t.Error("nil model must not report JSON columns")
	}
}

func TestTagDeclaresJSON(t *testing.T) {
	cases := map[string]bool{
		"config,type:jsonb":          true,
		"column:settings;type:json":  true,
		"col,type:text":              false,
		"column:name":                false,
		"":                           false,
		"col,type:jsonb,notnull":     true,
		"column:x;type:varchar(255)": false,
		"col , type:json":            true,
	}
	for tag, want := range cases {
		if got := tagDeclaresJSON(tag); got != want {
			t.Errorf("tagDeclaresJSON(%q) = %v; want %v", tag, got, want)
		}
	}
}
