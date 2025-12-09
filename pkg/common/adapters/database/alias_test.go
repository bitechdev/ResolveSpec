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
			name:          "strips plausible alias from simple condition",
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
			name:          "strips plausible alias with multiple conditions",
			query:         "APIL.rid_hub = ? AND APIL.active = ?",
			expectedAlias: "apiproviderlink",
			tableName:     "apiproviderlink",
			want:          "rid_hub = ? AND active = ?",
		},
		{
			name:          "handles mixed correct and plausible aliases",
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
		{
			name:          "keeps reference to different table (not in current table name)",
			query:         "APIL.rid_hub = ?",
			expectedAlias: "apiprovider",
			tableName:     "apiprovider",
			want:          "APIL.rid_hub = ?",
		},
		{
			name:          "keeps reference with short prefix that might be ambiguous",
			query:         "AP.rid = ?",
			expectedAlias: "apiprovider",
			tableName:     "apiprovider",
			want:          "AP.rid = ?",
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
