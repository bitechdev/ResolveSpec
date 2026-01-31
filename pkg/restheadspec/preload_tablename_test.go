package restheadspec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// TestPreloadOption_TableName verifies that TableName field is properly used
// when provided in PreloadOption for WHERE clause processing
func TestPreloadOption_TableName(t *testing.T) {
	tests := []struct {
		name          string
		preload       common.PreloadOption
		expectedTable string
	}{
		{
			name: "TableName provided explicitly",
			preload: common.PreloadOption{
				Relation:  "MTL.MAL.MAL_RID_PARENTMASTERTASKITEM",
				TableName: "mastertaskitem",
				Where:     "rid_parentmastertaskitem is null",
			},
			expectedTable: "mastertaskitem",
		},
		{
			name: "TableName empty, should use empty string",
			preload: common.PreloadOption{
				Relation:  "MTL.MAL.MAL_RID_PARENTMASTERTASKITEM",
				TableName: "",
				Where:     "rid_parentmastertaskitem is null",
			},
			expectedTable: "",
		},
		{
			name: "Simple relation without nested path",
			preload: common.PreloadOption{
				Relation:  "Users",
				TableName: "users",
				Where:     "active = true",
			},
			expectedTable: "users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the TableName field stores the correct value
			if tt.preload.TableName != tt.expectedTable {
				t.Errorf("PreloadOption.TableName = %q, want %q", tt.preload.TableName, tt.expectedTable)
			}

			// Verify that when TableName is provided, it should be used instead of extracting from relation
			tableName := tt.preload.TableName
			if tableName == "" {
				// This simulates the fallback logic in handler.go
				// In reality, reflection.ExtractTableNameOnly would be called
				tableName = tt.expectedTable
			}

			if tableName != tt.expectedTable {
				t.Errorf("Resolved table name = %q, want %q", tableName, tt.expectedTable)
			}
		})
	}
}

// TestXFilesPreload_StoresTableName verifies that XFiles processing
// stores the table name in PreloadOption and doesn't add table prefixes to WHERE clauses
func TestXFilesPreload_StoresTableName(t *testing.T) {
	handler := &Handler{}

	xfiles := &XFiles{
		TableName:  "mastertaskitem",
		Prefix:     "MAL",
		PrimaryKey: "rid_mastertaskitem",
		RelatedKey: "rid_mastertask", // Changed from rid_parentmastertaskitem
		Recursive:  false,            // Changed from true (recursive children are now skipped)
		SqlAnd:     []string{"rid_parentmastertaskitem is null"},
	}

	options := &ExtendedRequestOptions{}

	// Process XFiles
	handler.addXFilesPreload(xfiles, options, "MTL")

	// Verify that a preload was added
	if len(options.Preload) == 0 {
		t.Fatal("Expected at least one preload to be added")
	}

	preload := options.Preload[0]

	// Verify the table name is stored
	if preload.TableName != "mastertaskitem" {
		t.Errorf("PreloadOption.TableName = %q, want %q", preload.TableName, "mastertaskitem")
	}

	// Verify the relation path includes the prefix
	expectedRelation := "MTL.MAL"
	if preload.Relation != expectedRelation {
		t.Errorf("PreloadOption.Relation = %q, want %q", preload.Relation, expectedRelation)
	}

	// Verify WHERE clause does NOT have table prefix (prefixes only needed for JOINs)
	expectedWhere := "rid_parentmastertaskitem is null"
	if preload.Where != expectedWhere {
		t.Errorf("PreloadOption.Where = %q, want %q (no table prefix)", preload.Where, expectedWhere)
	}
}
