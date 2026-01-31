//go:build !integration
// +build !integration

package restheadspec

import (
	"context"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// TestRecursivePreloadClearsWhereClause tests that recursive preloads
// correctly clear the WHERE clause from the parent level to allow
// Bun to use foreign key relationships for loading children
func TestRecursivePreloadClearsWhereClause(t *testing.T) {
	// Create a mock handler
	handler := &Handler{}

	// Create a preload option with a WHERE clause that filters root items
	// This simulates the xfiles use case where the first level has a filter
	// like "rid_parentmastertaskitem is null" to get root items
	preload := common.PreloadOption{
		Relation:   "MastertaskItems",
		Recursive:  true,
		RelatedKey: "rid_parentmastertaskitem",
		Where:      "rid_parentmastertaskitem is null",
		Filters: []common.FilterOption{
			{
				Column:   "rid_parentmastertaskitem",
				Operator: "is null",
				Value:    nil,
			},
		},
	}

	// Create a mock query that tracks operations
	mockQuery := &mockSelectQuery{
		operations: []string{},
	}

	// Apply the recursive preload at depth 0
	// This should:
	// 1. Apply the initial preload with the WHERE clause
	// 2. Create a recursive preload without the WHERE clause
	allPreloads := []common.PreloadOption{preload}
	result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 0)

	// Verify the mock query received the operations
	mock := result.(*mockSelectQuery)

	// Check that we have at least 2 PreloadRelation calls:
	// 1. The initial "MastertaskItems" with WHERE clause
	// 2. The recursive "MastertaskItems.MastertaskItems_RID_PARENTMASTERTASKITEM" without WHERE clause
	preloadCount := 0
	recursivePreloadFound := false
	whereAppliedToRecursive := false

	for _, op := range mock.operations {
		if op == "PreloadRelation:MastertaskItems" {
			preloadCount++
		}
		if op == "PreloadRelation:MastertaskItems.MastertaskItems_RID_PARENTMASTERTASKITEM" {
			recursivePreloadFound = true
		}
		// Check if WHERE was applied to the recursive preload (it shouldn't be)
		if op == "Where:rid_parentmastertaskitem is null" && recursivePreloadFound {
			whereAppliedToRecursive = true
		}
	}

	if preloadCount < 1 {
		t.Errorf("Expected at least 1 PreloadRelation call, got %d", preloadCount)
	}

	if !recursivePreloadFound {
		t.Errorf("Expected recursive preload 'MastertaskItems.MastertaskItems_RID_PARENTMASTERTASKITEM' to be created. Operations: %v", mock.operations)
	}

	if whereAppliedToRecursive {
		t.Error("WHERE clause should not be applied to recursive preload levels")
	}
}

// TestRecursivePreloadWithChildRelations tests that child relations
// (like DEF in MAL.DEF) are properly extended to recursive levels
func TestRecursivePreloadWithChildRelations(t *testing.T) {
	handler := &Handler{}

	// Create the main recursive preload
	recursivePreload := common.PreloadOption{
		Relation:   "MAL",
		Recursive:  true,
		RelatedKey: "rid_parentmastertaskitem",
		Where:      "rid_parentmastertaskitem is null",
	}

	// Create a child relation that should be extended
	childPreload := common.PreloadOption{
		Relation: "MAL.DEF",
	}

	mockQuery := &mockSelectQuery{
		operations: []string{},
	}

	allPreloads := []common.PreloadOption{recursivePreload, childPreload}

	// Apply both preloads - the child preload should be extended when the recursive one processes
	result := handler.applyPreloadWithRecursion(mockQuery, recursivePreload, allPreloads, nil, 0)

	// Also need to apply the child preload separately (as would happen in normal flow)
	result = handler.applyPreloadWithRecursion(result, childPreload, allPreloads, nil, 0)

	mock := result.(*mockSelectQuery)

	// Check that the child relation was extended to recursive levels
	// We should see:
	// - MAL (with WHERE)
	// - MAL.DEF
	// - MAL.MAL_RID_PARENTMASTERTASKITEM (without WHERE)
	// - MAL.MAL_RID_PARENTMASTERTASKITEM.DEF (extended by recursive logic)
	foundMALDEF := false
	foundRecursiveMAL := false
	foundMALMALDEF := false

	for _, op := range mock.operations {
		if op == "PreloadRelation:MAL.DEF" {
			foundMALDEF = true
		}
		if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
			foundRecursiveMAL = true
		}
		if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM.DEF" {
			foundMALMALDEF = true
		}
	}

	if !foundMALDEF {
		t.Errorf("Expected child preload 'MAL.DEF' to be applied. Operations: %v", mock.operations)
	}

	if !foundRecursiveMAL {
		t.Errorf("Expected recursive preload 'MAL.MAL_RID_PARENTMASTERTASKITEM' to be created. Operations: %v", mock.operations)
	}

	if !foundMALMALDEF {
		t.Errorf("Expected child preload to be extended to 'MAL.MAL_RID_PARENTMASTERTASKITEM.DEF' at recursive level. Operations: %v", mock.operations)
	}
}

// TestRecursivePreloadGeneratesCorrectRelationName tests that the recursive
// preload generates the correct FK-based relation name using RelatedKey
func TestRecursivePreloadGeneratesCorrectRelationName(t *testing.T) {
	handler := &Handler{}

	// Test case 1: With RelatedKey - should generate FK-based name
	t.Run("WithRelatedKey", func(t *testing.T) {
		preload := common.PreloadOption{
			Relation:   "MAL",
			Recursive:  true,
			RelatedKey: "rid_parentmastertaskitem",
		}

		mockQuery := &mockSelectQuery{operations: []string{}}
		allPreloads := []common.PreloadOption{preload}
		result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 0)

		mock := result.(*mockSelectQuery)

		// Should generate MAL.MAL_RID_PARENTMASTERTASKITEM
		foundCorrectRelation := false
		foundIncorrectRelation := false

		for _, op := range mock.operations {
			if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundCorrectRelation = true
			}
			if op == "PreloadRelation:MAL.MAL" {
				foundIncorrectRelation = true
			}
		}

		if !foundCorrectRelation {
			t.Errorf("Expected 'MAL.MAL_RID_PARENTMASTERTASKITEM' relation, operations: %v", mock.operations)
		}

		if foundIncorrectRelation {
			t.Error("Should NOT generate 'MAL.MAL' relation when RelatedKey is specified")
		}
	})

	// Test case 2: Without RelatedKey - should fallback to old behavior
	t.Run("WithoutRelatedKey", func(t *testing.T) {
		preload := common.PreloadOption{
			Relation:  "MAL",
			Recursive: true,
			// No RelatedKey
		}

		mockQuery := &mockSelectQuery{operations: []string{}}
		allPreloads := []common.PreloadOption{preload}
		result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 0)

		mock := result.(*mockSelectQuery)

		// Should fallback to MAL.MAL
		foundFallback := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MAL.MAL" {
				foundFallback = true
			}
		}

		if !foundFallback {
			t.Errorf("Expected fallback 'MAL.MAL' relation when no RelatedKey, operations: %v", mock.operations)
		}
	})

	// Test case 3: Depth limit of 8
	t.Run("DepthLimit", func(t *testing.T) {
		preload := common.PreloadOption{
			Relation:   "MAL",
			Recursive:  true,
			RelatedKey: "rid_parentmastertaskitem",
		}

		mockQuery := &mockSelectQuery{operations: []string{}}
		allPreloads := []common.PreloadOption{preload}

		// Start at depth 7 - should create one more level
		result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 7)
		mock := result.(*mockSelectQuery)

		foundDepth8 := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundDepth8 = true
			}
		}

		if !foundDepth8 {
			t.Error("Expected to create recursive level at depth 8")
		}

		// Start at depth 8 - should NOT create another level
		mockQuery2 := &mockSelectQuery{operations: []string{}}
		result2 := handler.applyPreloadWithRecursion(mockQuery2, preload, allPreloads, nil, 8)
		mock2 := result2.(*mockSelectQuery)

		foundDepth9 := false
		for _, op := range mock2.operations {
			if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundDepth9 = true
			}
		}

		if foundDepth9 {
			t.Error("Should NOT create recursive level beyond depth 8")
		}
	})
}

// mockSelectQuery implements common.SelectQuery for testing
type mockSelectQuery struct {
	operations []string
}

func (m *mockSelectQuery) Model(model interface{}) common.SelectQuery {
	m.operations = append(m.operations, "Model")
	return m
}

func (m *mockSelectQuery) Table(table string) common.SelectQuery {
	m.operations = append(m.operations, "Table:"+table)
	return m
}

func (m *mockSelectQuery) Column(columns ...string) common.SelectQuery {
	for _, col := range columns {
		m.operations = append(m.operations, "Column:"+col)
	}
	return m
}

func (m *mockSelectQuery) ColumnExpr(query string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "ColumnExpr:"+query)
	return m
}

func (m *mockSelectQuery) Where(query string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "Where:"+query)
	return m
}

func (m *mockSelectQuery) WhereOr(query string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "WhereOr:"+query)
	return m
}

func (m *mockSelectQuery) WhereIn(column string, values interface{}) common.SelectQuery {
	m.operations = append(m.operations, "WhereIn:"+column)
	return m
}

func (m *mockSelectQuery) Order(order string) common.SelectQuery {
	m.operations = append(m.operations, "Order:"+order)
	return m
}

func (m *mockSelectQuery) OrderExpr(order string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "OrderExpr:"+order)
	return m
}

func (m *mockSelectQuery) Limit(limit int) common.SelectQuery {
	m.operations = append(m.operations, "Limit")
	return m
}

func (m *mockSelectQuery) Offset(offset int) common.SelectQuery {
	m.operations = append(m.operations, "Offset")
	return m
}

func (m *mockSelectQuery) Join(join string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "Join:"+join)
	return m
}

func (m *mockSelectQuery) LeftJoin(join string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "LeftJoin:"+join)
	return m
}

func (m *mockSelectQuery) Group(columns string) common.SelectQuery {
	m.operations = append(m.operations, "Group")
	return m
}

func (m *mockSelectQuery) Having(query string, args ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "Having:"+query)
	return m
}

func (m *mockSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
	m.operations = append(m.operations, "Preload:"+relation)
	return m
}

func (m *mockSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	m.operations = append(m.operations, "PreloadRelation:"+relation)
	// Apply the preload modifiers
	for _, fn := range apply {
		fn(m)
	}
	return m
}

func (m *mockSelectQuery) JoinRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	m.operations = append(m.operations, "JoinRelation:"+relation)
	return m
}

func (m *mockSelectQuery) Scan(ctx context.Context, dest interface{}) error {
	m.operations = append(m.operations, "Scan")
	return nil
}

func (m *mockSelectQuery) ScanModel(ctx context.Context) error {
	m.operations = append(m.operations, "ScanModel")
	return nil
}

func (m *mockSelectQuery) Count(ctx context.Context) (int, error) {
	m.operations = append(m.operations, "Count")
	return 0, nil
}

func (m *mockSelectQuery) Exists(ctx context.Context) (bool, error) {
	m.operations = append(m.operations, "Exists")
	return false, nil
}

func (m *mockSelectQuery) GetUnderlyingQuery() interface{} {
	return nil
}

func (m *mockSelectQuery) GetModel() interface{} {
	return nil
}
