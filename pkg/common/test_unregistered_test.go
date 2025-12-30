package common

import (
"fmt"
"testing"
)

func TestAddTablePrefixToColumns_UnregisteredModel(t *testing.T) {
tests := []struct {
name      string
where     string
tableName string
expected  string
}{
{
name:      "SQL literal true with unregistered model - should not add prefix",
where:     "true",
tableName: "unregistered_table",
expected:  "true",
},
{
name:      "SQL literal false with unregistered model - should not add prefix",
where:     "false",
tableName: "unregistered_table",
expected:  "false",
},
{
name:      "SQL literal null with unregistered model - should not add prefix",
where:     "null",
tableName: "unregistered_table",
expected:  "null",
},
{
name:      "Valid column with unregistered model - should not add prefix (no validation)",
where:     "status = 'active'",
tableName: "unregistered_table",
expected:  "unregistered_table.status = 'active'",
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
result := AddTablePrefixToColumns(tt.where, tt.tableName)
if result != tt.expected {
t.Errorf("AddTablePrefixToColumns(%q, %q) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
} else {
fmt.Printf("✓ Test passed: %s\n", tt.name)
}
})
}
}
