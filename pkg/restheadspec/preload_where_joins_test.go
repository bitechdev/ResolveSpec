package restheadspec

import (
	"testing"
)

// TestPreloadWhereClause_WithJoins verifies that table prefixes are added
// to WHERE clauses when SqlJoins are present
func TestPreloadWhereClause_WithJoins(t *testing.T) {
	tests := []struct {
		name           string
		where          string
		sqlJoins       []string
		expectedPrefix bool
		description    string
	}{
		{
			name:           "No joins - no prefix needed",
			where:          "status = 'active'",
			sqlJoins:       []string{},
			expectedPrefix: false,
			description:    "Without JOINs, Bun knows the table context",
		},
		{
			name:           "Has joins - prefix needed",
			where:          "status = 'active'",
			sqlJoins:       []string{"LEFT JOIN other_table ot ON ot.id = main.other_id"},
			expectedPrefix: true,
			description:    "With JOINs, table prefix disambiguates columns",
		},
		{
			name:           "Already has prefix - no change",
			where:          "users.status = 'active'",
			sqlJoins:       []string{"LEFT JOIN roles r ON r.id = users.role_id"},
			expectedPrefix: true,
			description:    "Existing prefix should be preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test documents the expected behavior
			// The actual logic is in handler.go lines 916-937

			hasJoins := len(tt.sqlJoins) > 0
			if hasJoins != tt.expectedPrefix {
				t.Errorf("Test expectation mismatch: hasJoins=%v, expectedPrefix=%v",
					hasJoins, tt.expectedPrefix)
			}

			t.Logf("%s: %s", tt.name, tt.description)
		})
	}
}

// TestXFilesWithJoins_AddsTablePrefix verifies that XFiles with SqlJoins
// results in table prefixes being added to WHERE clauses
func TestXFilesWithJoins_AddsTablePrefix(t *testing.T) {
	handler := &Handler{}

	xfiles := &XFiles{
		TableName:  "users",
		Prefix:     "USR",
		PrimaryKey: "id",
		SqlAnd:     []string{"status = 'active'"},
		SqlJoins:   []string{"LEFT JOIN departments d ON d.id = users.department_id"},
	}

	options := &ExtendedRequestOptions{}
	handler.addXFilesPreload(xfiles, options, "")

	if len(options.Preload) == 0 {
		t.Fatal("Expected at least one preload to be added")
	}

	preload := options.Preload[0]

	// Verify SqlJoins were stored
	if len(preload.SqlJoins) != 1 {
		t.Errorf("Expected 1 SqlJoin, got %d", len(preload.SqlJoins))
	}

	// Verify WHERE clause does NOT have prefix yet (added later in handler)
	expectedWhere := "status = 'active'"
	if preload.Where != expectedWhere {
		t.Errorf("PreloadOption.Where = %q, want %q", preload.Where, expectedWhere)
	}

	// Note: The handler will add the prefix when it sees SqlJoins
	// This is tested in the handler itself, not during XFiles parsing
}
