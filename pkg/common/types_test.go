package common

import (
	"reflect"
	"testing"
)

func TestResolveSortColumns(t *testing.T) {
	sort := []SortOption{
		{Column: PrimaryKeySortColumn, Direction: "asc"},
		{Column: "name", Direction: "desc"},
	}

	got := ResolveSortColumns(sort, "user_id")
	want := []SortOption{
		{Column: "user_id", Direction: "asc"},
		{Column: "name", Direction: "desc"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveSortColumns() = %v, want %v", got, want)
	}

	// Original slice must not be mutated
	if sort[0].Column != PrimaryKeySortColumn {
		t.Errorf("ResolveSortColumns() mutated input slice: %v", sort)
	}
}

func TestResolveSortColumnsDesc(t *testing.T) {
	sort := []SortOption{
		{Column: PrimaryKeySortColumn, Direction: "desc"},
	}

	got := ResolveSortColumns(sort, "id")
	want := []SortOption{{Column: "id", Direction: "desc"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveSortColumns() = %v, want %v", got, want)
	}
}

func TestResolveSortColumnsEmptyPK(t *testing.T) {
	sort := []SortOption{
		{Column: PrimaryKeySortColumn, Direction: "asc"},
		{Column: "name", Direction: "desc"},
	}

	got := ResolveSortColumns(sort, "")
	want := []SortOption{{Column: "name", Direction: "desc"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveSortColumns() with empty pk = %v, want %v", got, want)
	}
}

func TestResolveSortColumnsNil(t *testing.T) {
	if got := ResolveSortColumns(nil, "id"); len(got) != 0 {
		t.Errorf("ResolveSortColumns(nil) = %v, want empty", got)
	}
}
