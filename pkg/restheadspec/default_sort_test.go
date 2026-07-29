package restheadspec

import (
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestSetDefaultSort(t *testing.T) {
	handler := NewHandler(nil, nil)

	// No default configured yet
	if got := handler.getDefaultSort("public", "users"); got != nil {
		t.Errorf("Expected no default sort, got %v", got)
	}

	// Global default
	global := []common.SortOption{{Column: "created_at", Direction: "desc"}}
	handler.SetDefaultSort("", "", global...)

	if got := handler.getDefaultSort("public", "users"); !reflect.DeepEqual(got, global) {
		t.Errorf("Expected global default sort %v, got %v", global, got)
	}

	// Per-model default overrides the global default
	perModel := []common.SortOption{{Column: "name", Direction: "asc"}}
	handler.SetDefaultSort("public", "users", perModel...)

	if got := handler.getDefaultSort("public", "users"); !reflect.DeepEqual(got, perModel) {
		t.Errorf("Expected per-model default sort %v, got %v", perModel, got)
	}

	// Other models still fall back to the global default
	if got := handler.getDefaultSort("public", "orders"); !reflect.DeepEqual(got, global) {
		t.Errorf("Expected global default sort %v for unrelated model, got %v", global, got)
	}
}
