package common

import "testing"

func TestMergeUpdateValues(t *testing.T) {
	newExisting := func() map[string]interface{} {
		return map[string]interface{}{"name": "old", "note": "keep", "other": "x"}
	}
	incoming := map[string]interface{}{"name": "", "note": nil}

	got := MergeUpdateValues(newExisting(), incoming, false)
	if got["name"] != "" {
		t.Errorf("name = %v, want empty string", got["name"])
	}
	if v, ok := got["note"]; !ok || v != nil {
		t.Errorf("note = %v (present=%v), want nil", v, ok)
	}
	if got["other"] != "x" {
		t.Errorf("absent key changed: %v", got["other"])
	}

	got = MergeUpdateValues(newExisting(), incoming, true)
	if got["name"] != "" {
		t.Errorf("disallowNulls: name = %v, want empty string", got["name"])
	}
	if got["note"] != "keep" {
		t.Errorf("disallowNulls: note = %v, want keep", got["note"])
	}
}
