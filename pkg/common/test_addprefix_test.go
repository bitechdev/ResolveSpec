package common

import (
"fmt"
"testing"
)

func TestAddTablePrefixToColumns_BugRepro(t *testing.T) {
tests := []struct {
name      string
where     string
tableName string
expected  string
}{
{
name:      "SQL literal true - should not add prefix",
where:     "true",
tableName: "mastertask",
expected:  "true",
},
{
name:      "SQL literal TRUE uppercase - should not add prefix",
where:     "TRUE",
tableName: "mastertask",
expected:  "TRUE",
},
{
name:      "SQL literal false - should not add prefix",
where:     "false",
tableName: "mastertask",
expected:  "false",
},
{
name:      "SQL literal null - should not add prefix",
where:     "null",
tableName: "mastertask",
expected:  "null",
},
{
name:      "SQL literal NULL uppercase - should not add prefix",
where:     "NULL",
tableName: "mastertask",
expected:  "NULL",
},
{
name:      "Multiple true conditions",
where:     "true AND true",
tableName: "mastertask",
expected:  "true AND true",
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
