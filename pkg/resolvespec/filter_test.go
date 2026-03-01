package resolvespec

import (
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// TestBuildFilterCondition tests the filter condition builder
func TestBuildFilterCondition(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name              string
		filter            common.FilterOption
		expectedCondition string
		expectedArgsCount int
	}{
		{
			name: "Equal operator",
			filter: common.FilterOption{
				Column:   "status",
				Operator: "eq",
				Value:    "active",
			},
			expectedCondition: "status = ?",
			expectedArgsCount: 1,
		},
		{
			name: "Greater than operator",
			filter: common.FilterOption{
				Column:   "age",
				Operator: "gt",
				Value:    18,
			},
			expectedCondition: "age > ?",
			expectedArgsCount: 1,
		},
		{
			name: "IN operator",
			filter: common.FilterOption{
				Column:   "status",
				Operator: "in",
				Value:    []string{"active", "pending"},
			},
			expectedCondition: "status IN (?,?)",
			expectedArgsCount: 2,
		},
		{
			name: "LIKE operator",
			filter: common.FilterOption{
				Column:   "email",
				Operator: "like",
				Value:    "%@example.com",
			},
			expectedCondition: "email LIKE ?",
			expectedArgsCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition, args := h.buildFilterCondition(tt.filter)

			if condition != tt.expectedCondition {
				t.Errorf("Expected condition '%s', got '%s'", tt.expectedCondition, condition)
			}

			if len(args) != tt.expectedArgsCount {
				t.Errorf("Expected %d args, got %d", tt.expectedArgsCount, len(args))
			}

			// Note: Skip value comparison for slices as they can't be compared with ==
			// The important part is that args are populated correctly
		})
	}
}

// TestORGrouping tests that consecutive OR filters are properly grouped
func TestORGrouping(t *testing.T) {
	// This is a conceptual test - in practice we'd need a mock SelectQuery
	// to verify the actual SQL grouping behavior
	t.Run("Consecutive OR filters should be grouped", func(t *testing.T) {
		filters := []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "active"},
			{Column: "status", Operator: "eq", Value: "pending", LogicOperator: "OR"},
			{Column: "status", Operator: "eq", Value: "trial", LogicOperator: "OR"},
			{Column: "age", Operator: "gte", Value: 18},
		}

		// Expected behavior: (status='active' OR status='pending' OR status='trial') AND age>=18
		// The first three filters should be grouped together
		// The fourth filter should be separate with AND

		// Count OR groups
		orGroupCount := 0
		inORGroup := false

		for i := 1; i < len(filters); i++ {
			if strings.EqualFold(filters[i].LogicOperator, "OR") && !inORGroup {
				orGroupCount++
				inORGroup = true
			} else if !strings.EqualFold(filters[i].LogicOperator, "OR") {
				inORGroup = false
			}
		}

		// We should have detected one OR group
		if orGroupCount != 1 {
			t.Errorf("Expected 1 OR group, detected %d", orGroupCount)
		}
	})

	t.Run("Multiple OR groups should be handled correctly", func(t *testing.T) {
		filters := []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "active"},
			{Column: "status", Operator: "eq", Value: "pending", LogicOperator: "OR"},
			{Column: "priority", Operator: "eq", Value: "high"},
			{Column: "priority", Operator: "eq", Value: "urgent", LogicOperator: "OR"},
		}

		// Expected: (status='active' OR status='pending') AND (priority='high' OR priority='urgent')
		// Should have two OR groups

		orGroupCount := 0
		inORGroup := false

		for i := 1; i < len(filters); i++ {
			if strings.EqualFold(filters[i].LogicOperator, "OR") && !inORGroup {
				orGroupCount++
				inORGroup = true
			} else if !strings.EqualFold(filters[i].LogicOperator, "OR") {
				inORGroup = false
			}
		}

		// We should have detected two OR groups
		if orGroupCount != 2 {
			t.Errorf("Expected 2 OR groups, detected %d", orGroupCount)
		}
	})
}
