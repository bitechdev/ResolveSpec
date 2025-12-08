package restheadspec

import (
	"testing"
)

func TestDecodeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "String without encoding prefix",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeHeaderValue(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Note: The following functions are unexported (lowercase) and cannot be tested directly:
// - parseSelectFields
// - parseFieldFilter
// - mapSearchOperator
// - parseCommaSeparated
// - parseSorting
// These are tested indirectly through parseOptionsFromHeaders in query_params_test.go
