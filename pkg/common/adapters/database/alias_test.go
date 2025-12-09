package database

import (
	"testing"
)

func TestNormalizeTableAlias(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectedAlias string
		tableName     string
		want          string
	}{
		{
			name:          "strips incorrect alias from simple condition",
			query:         "APIL.rid_hub = 2576",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "rid_hub = 2576",
		},
		{
			name:          "keeps correct alias",
			query:         "apiproviderlink.rid_hub = 2576",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "apiproviderlink.rid_hub = 2576",
		},
		{
			name:          "strips incorrect alias with multiple conditions",
			query:         "APIL.rid_hub = ? AND APIL.active = ?",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "rid_hub = ? AND active = ?",
		},
		{
			name:          "handles mixed correct and incorrect aliases",
			query:         "APIL.rid_hub = ? AND apiproviderlink.active = ?",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "rid_hub = ? AND apiproviderlink.active = ?",
		},
		{
			name:          "handles parentheses",
			query:         "(APIL.rid_hub = ?)",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "(rid_hub = ?)",
		},
		{
			name:          "no alias in query",
			query:         "rid_hub = ?",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "rid_hub = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeTableAlias(tt.query, tt.expectedAlias, tt.tableName)
			if got != tt.want {
				t.Errorf("normalizeTableAlias() = %q, want %q", got, tt.want)
			}
		})
	}
}
