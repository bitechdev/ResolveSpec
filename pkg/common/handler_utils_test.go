package common

import (
	"testing"
)

func TestExtractTagValue(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		key      string
		expected string
	}{
		{
			name:     "Extract existing key",
			tag:      "json:name;validate:required",
			key:      "json",
			expected: "name",
		},
		{
			name:     "Extract key with spaces",
			tag:      "json:name ; validate:required",
			key:      "validate",
			expected: "required",
		},
		{
			name:     "Extract key at end",
			tag:      "json:name;validate:required;db:column_name",
			key:      "db",
			expected: "column_name",
		},
		{
			name:     "Extract key at beginning",
			tag:      "primary:true;json:id;db:user_id",
			key:      "primary",
			expected: "true",
		},
		{
			name:     "Key not found",
			tag:      "json:name;validate:required",
			key:      "db",
			expected: "",
		},
		{
			name:     "Empty tag",
			tag:      "",
			key:      "json",
			expected: "",
		},
		{
			name:     "Single key-value pair",
			tag:      "json:name",
			key:      "json",
			expected: "name",
		},
		{
			name:     "Key with empty value",
			tag:      "json:;validate:required",
			key:      "json",
			expected: "",
		},
		{
			name:     "Key with complex value",
			tag:      "json:user_name,omitempty;validate:required,min=3",
			key:      "json",
			expected: "user_name,omitempty",
		},
		{
			name:     "Multiple semicolons",
			tag:      "json:name;;validate:required",
			key:      "validate",
			expected: "required",
		},
		{
			name:     "BUN Tag with comma separator",
			tag:      "rel:has-many,join:rid_hub=rid_hub_child",
			key:      "join",
			expected: "rid_hub=rid_hub_child",
		},
		{
			name:     "Extract foreignKey",
			tag:      "foreignKey:UserID;references:ID",
			key:      "foreignKey",
			expected: "UserID",
		},
		{
			name:     "Extract references",
			tag:      "foreignKey:UserID;references:ID",
			key:      "references",
			expected: "ID",
		},
		{
			name:     "Extract many2many",
			tag:      "many2many:user_roles",
			key:      "many2many",
			expected: "user_roles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractTagValue(tt.tag, tt.key)
			if result != tt.expected {
				t.Errorf("ExtractTagValue(%q, %q) = %q; want %q", tt.tag, tt.key, result, tt.expected)
			}
		})
	}
}
