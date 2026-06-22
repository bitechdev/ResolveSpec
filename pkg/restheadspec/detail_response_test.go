package restheadspec

import (
	"encoding/json"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// detailTestModel is a simple model with gorm column/type tags for detail format tests.
type detailTestModel struct {
	ID          int64   `bun:"rid,pk" gorm:"column:rid;primaryKey" json:"rid"`
	Name        string  `bun:"name" gorm:"column:name;type:citext" json:"name"`
	Description *string `bun:"description" gorm:"column:description;type:text;nullable" json:"description"`
	Score       float64 `bun:"score" gorm:"column:score;type:numeric" json:"score"`
	Active      bool    `bun:"active" gorm:"column:active;type:boolean;not null" json:"active"`
}

func TestSendFormattedResponse_DetailFormat(t *testing.T) {
	handler := &Handler{}

	name := "hello"
	items := []*detailTestModel{
		{ID: 1, Name: "first", Description: &name, Score: 1.5, Active: true},
		{ID: 2, Name: "second", Description: nil, Score: 2.0, Active: false},
	}
	metadata := &common.Metadata{
		Total:    36,
		Count:    2,
		Filtered: 36,
		Limit:    10,
		Offset:   0,
	}
	options := ExtendedRequestOptions{
		ResponseFormat: "detail",
	}

	mockWriter := &MockTestResponseWriter{headers: make(map[string]string)}
	handler.sendFormattedResponse(mockWriter, items, metadata, "myschema.myentity", detailTestModel{}, options)

	if mockWriter.statusCode != 200 {
		t.Fatalf("expected status 200, got %d", mockWriter.statusCode)
	}

	body, err := json.Marshal(mockWriter.body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	t.Run("top-level keys", func(t *testing.T) {
		for _, key := range []string{"count", "fields", "items", "tablename", "tableprefix", "total"} {
			if _, ok := resp[key]; !ok {
				t.Errorf("missing key %q in detail response", key)
			}
		}
	})

	t.Run("count and total are string", func(t *testing.T) {
		var count, total string
		if err := json.Unmarshal(resp["count"], &count); err != nil {
			t.Errorf("count is not a string: %v", err)
		}
		if err := json.Unmarshal(resp["total"], &total); err != nil {
			t.Errorf("total is not a string: %v", err)
		}
		if count != "2" {
			t.Errorf("expected count %q, got %q", "2", count)
		}
		if total != "36" {
			t.Errorf("expected total %q, got %q", "36", total)
		}
	})

	t.Run("tablename and tableprefix", func(t *testing.T) {
		var tablename, tableprefix string
		json.Unmarshal(resp["tablename"], &tablename)
		json.Unmarshal(resp["tableprefix"], &tableprefix)
		if tablename != "myschema.myentity" {
			t.Errorf("expected tablename %q, got %q", "myschema.myentity", tablename)
		}
		if tableprefix != "myentity" {
			t.Errorf("expected tableprefix %q, got %q", "myentity", tableprefix)
		}
	})

	t.Run("items contains data", func(t *testing.T) {
		var itemSlice []map[string]interface{}
		if err := json.Unmarshal(resp["items"], &itemSlice); err != nil {
			t.Fatalf("items is not an array: %v", err)
		}
		if len(itemSlice) != 2 {
			t.Errorf("expected 2 items, got %d", len(itemSlice))
		}
	})

	t.Run("fields contains column metadata", func(t *testing.T) {
		var fields []map[string]interface{}
		if err := json.Unmarshal(resp["fields"], &fields); err != nil {
			t.Fatalf("fields is not an array: %v", err)
		}
		if len(fields) == 0 {
			t.Fatal("expected fields to be non-empty")
		}

		bySQL := make(map[string]map[string]interface{}, len(fields))
		for _, f := range fields {
			if sqlname, ok := f["sqlname"].(string); ok {
				bySQL[sqlname] = f
			}
		}

		// Check required field keys are present
		for _, f := range fields {
			for _, key := range []string{"name", "datatype", "sqlname", "sqldatatype", "sqlkey", "nullable"} {
				if _, ok := f[key]; !ok {
					t.Errorf("field %v missing key %q", f, key)
				}
			}
		}

		// Validate specific columns
		if col, ok := bySQL["rid"]; ok {
			if col["sqlkey"] != "primary_key" {
				t.Errorf("rid: expected sqlkey %q, got %v", "primary_key", col["sqlkey"])
			}
		} else {
			t.Error("expected column 'rid' in fields")
		}

		if col, ok := bySQL["name"]; ok {
			if col["sqldatatype"] != "citext" {
				t.Errorf("name: expected sqldatatype %q, got %v", "citext", col["sqldatatype"])
			}
			if col["nullable"] != false {
				t.Errorf("name: expected nullable false, got %v", col["nullable"])
			}
		} else {
			t.Error("expected column 'name' in fields")
		}

		if col, ok := bySQL["description"]; ok {
			if col["sqldatatype"] != "text" {
				t.Errorf("description: expected sqldatatype %q, got %v", "text", col["sqldatatype"])
			}
			if col["nullable"] != true {
				t.Errorf("description: expected nullable true, got %v", col["nullable"])
			}
		} else {
			t.Error("expected column 'description' in fields")
		}
	})
}

func TestSendFormattedResponse_DetailFormat_EmptyItems(t *testing.T) {
	handler := &Handler{}
	metadata := &common.Metadata{Total: 0, Count: 0, Filtered: 0}
	options := ExtendedRequestOptions{ResponseFormat: "detail"}

	mockWriter := &MockTestResponseWriter{headers: make(map[string]string)}
	handler.sendFormattedResponse(mockWriter, []*detailTestModel{}, metadata, "s.t", detailTestModel{}, options)

	body, _ := json.Marshal(mockWriter.body)
	var resp map[string]json.RawMessage
	json.Unmarshal(body, &resp)

	var count, total string
	json.Unmarshal(resp["count"], &count)
	json.Unmarshal(resp["total"], &total)

	if count != "0" || total != "0" {
		t.Errorf("expected count/total both %q, got count=%q total=%q", "0", count, total)
	}

	var fields []interface{}
	json.Unmarshal(resp["fields"], &fields)
	if len(fields) == 0 {
		t.Error("fields should still list column metadata even when items is empty")
	}
}

func TestBuildDetailFields_SkipsRelations(t *testing.T) {
	type child struct {
		ID int64 `bun:"id,pk" gorm:"column:id;primaryKey" json:"id"`
	}
	type parent struct {
		ID       int64   `bun:"id,pk" gorm:"column:id;primaryKey" json:"id"`
		Name     string  `bun:"name" gorm:"column:name" json:"name"`
		Children []child `bun:"rel:has-many" json:"children"`
		Child    *child  `bun:"rel:has-one" json:"child"`
	}

	handler := &Handler{}
	fields := handler.buildDetailFields(parent{})

	for _, f := range fields {
		if f.SQLName == "children" || f.SQLName == "child" {
			t.Errorf("relation field %q should not appear in detail fields", f.SQLName)
		}
	}

	if len(fields) != 2 {
		t.Errorf("expected 2 scalar fields (id, name), got %d", len(fields))
	}
}
