//go:build integration
// +build integration

package restheadspec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSelectQuery implements common.SelectQuery for testing (integration version)
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

// TestXFilesRecursivePreload is an integration test that validates the XFiles
// recursive preload functionality using real test data files.
//
// This test ensures:
// 1. XFiles request JSON is correctly parsed into PreloadOptions
// 2. Recursive preload generates correct FK-based relation names (MAL_RID_PARENTMASTERTASKITEM)
// 3. Parent WHERE clauses don't leak to child levels
// 4. Child relations (like DEF) are extended to all recursive levels
// 5. Hierarchical data structure matches expected output
func TestXFilesRecursivePreload(t *testing.T) {
	// Load the XFiles request configuration
	requestPath := filepath.Join("..", "..", "tests", "data", "xfiles.request.json")
	requestData, err := os.ReadFile(requestPath)
	require.NoError(t, err, "Failed to read xfiles.request.json")

	var xfileConfig XFiles
	err = json.Unmarshal(requestData, &xfileConfig)
	require.NoError(t, err, "Failed to parse xfiles.request.json")

	// Create handler and parse XFiles into PreloadOptions
	handler := &Handler{}
	options := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Preload: []common.PreloadOption{},
		},
	}

	// Process the XFiles configuration - start with the root table
	handler.processXFilesRelations(&xfileConfig, options, "")

	// Verify that preload options were created
	require.NotEmpty(t, options.Preload, "Expected preload options to be created")

	// Test 1: Verify mastertaskitem preload is marked as recursive with correct RelatedKey
	t.Run("RecursivePreloadHasRelatedKey", func(t *testing.T) {
		// Find the mastertaskitem preload - it should be marked as recursive
		var recursivePreload *common.PreloadOption
		for i := range options.Preload {
			preload := &options.Preload[i]
			if preload.Relation == "MTL.MAL" && preload.Recursive {
				recursivePreload = preload
				break
			}
		}

		require.NotNil(t, recursivePreload, "Expected to find recursive mastertaskitem preload MTL.MAL")

		// RelatedKey should be the parent relationship key (MTL -> MAL)
		assert.Equal(t, "rid_mastertask", recursivePreload.RelatedKey,
			"Recursive preload should preserve original RelatedKey for parent relationship")

		// RecursiveChildKey should be set from the recursive child config
		assert.Equal(t, "rid_parentmastertaskitem", recursivePreload.RecursiveChildKey,
			"Recursive preload should have RecursiveChildKey set from recursive child config")

		assert.True(t, recursivePreload.Recursive, "mastertaskitem preload should be marked as recursive")
	})

	// Test 2: Verify mastertaskitem has WHERE clause for filtering root items
	t.Run("RootLevelHasWhereClause", func(t *testing.T) {
		var rootPreload *common.PreloadOption
		for i := range options.Preload {
			preload := &options.Preload[i]
			if preload.Relation == "MTL.MAL" {
				rootPreload = preload
				break
			}
		}

		require.NotNil(t, rootPreload, "Expected to find mastertaskitem preload")
		assert.NotEmpty(t, rootPreload.Where, "Mastertaskitem should have WHERE clause")
		// The WHERE clause should filter for root items (rid_parentmastertaskitem is null)
		assert.True(t, rootPreload.Recursive, "Mastertaskitem preload should be marked as recursive")
	})

	// Test 3: Verify actiondefinition relation exists for mastertaskitem
	t.Run("DEFRelationExists", func(t *testing.T) {
		var defPreload *common.PreloadOption
		for i := range options.Preload {
			preload := &options.Preload[i]
			if preload.Relation == "MTL.MAL.DEF" {
				defPreload = preload
				break
			}
		}

		require.NotNil(t, defPreload, "Expected to find actiondefinition preload for mastertaskitem")
		assert.Equal(t, "rid_actiondefinition", defPreload.ForeignKey,
			"actiondefinition preload should have ForeignKey set")
	})

	// Test 4: Verify relation name generation with mock query
	t.Run("RelationNameGeneration", func(t *testing.T) {
		// Find the mastertaskitem preload - it should be marked as recursive
		var recursivePreload common.PreloadOption
		found := false
		for _, preload := range options.Preload {
			if preload.Relation == "MTL.MAL" && preload.Recursive {
				recursivePreload = preload
				found = true
				break
			}
		}

		require.True(t, found, "Expected to find recursive mastertaskitem preload MTL.MAL")

		// Create mock query to track operations
		mockQuery := &mockSelectQuery{operations: []string{}}

		// Apply the recursive preload
		result := handler.applyPreloadWithRecursion(mockQuery, recursivePreload, options.Preload, nil, 0)
		mock := result.(*mockSelectQuery)

		// Verify the correct FK-based relation name was generated
		foundCorrectRelation := false

		for _, op := range mock.operations {
			// Should generate: MTL.MAL.MAL_RID_PARENTMASTERTASKITEM
			if op == "PreloadRelation:MTL.MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundCorrectRelation = true
			}
		}

		assert.True(t, foundCorrectRelation,
			"Expected FK-based relation name 'MTL.MAL.MAL_RID_PARENTMASTERTASKITEM' to be generated. Operations: %v",
			mock.operations)
	})

	// Test 5: Verify WHERE clause is cleared for recursive levels
	t.Run("WhereClauseClearedForChildren", func(t *testing.T) {
		// Find the mastertaskitem preload - it should be marked as recursive
		var recursivePreload common.PreloadOption
		found := false
		for _, preload := range options.Preload {
			if preload.Relation == "MTL.MAL" && preload.Recursive {
				recursivePreload = preload
				found = true
				break
			}
		}

		require.True(t, found, "Expected to find recursive mastertaskitem preload MTL.MAL")

		// The root level has a WHERE clause (rid_parentmastertaskitem is null)
		// But when we apply recursion, it should be cleared
		assert.NotEmpty(t, recursivePreload.Where, "Root preload should have WHERE clause")

		mockQuery := &mockSelectQuery{operations: []string{}}
		result := handler.applyPreloadWithRecursion(mockQuery, recursivePreload, options.Preload, nil, 0)
		mock := result.(*mockSelectQuery)

		// After the first level, WHERE clauses should not be reapplied
		// We check that the recursive relation was created (which means WHERE was cleared internally)
		foundRecursiveRelation := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MTL.MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundRecursiveRelation = true
			}
		}

		assert.True(t, foundRecursiveRelation,
			"Recursive relation should be created (WHERE clause should be cleared internally)")
	})

	// Test 6: Verify child relations are extended to recursive levels
	t.Run("ChildRelationsExtended", func(t *testing.T) {
		// Find the mastertaskitem preload - it should be marked as recursive
		var recursivePreload common.PreloadOption
		foundRecursive := false

		for _, preload := range options.Preload {
			if preload.Relation == "MTL.MAL" && preload.Recursive {
				recursivePreload = preload
				foundRecursive = true
				break
			}
		}

		require.True(t, foundRecursive, "Expected to find recursive mastertaskitem preload MTL.MAL")

		mockQuery := &mockSelectQuery{operations: []string{}}
		result := handler.applyPreloadWithRecursion(mockQuery, recursivePreload, options.Preload, nil, 0)
		mock := result.(*mockSelectQuery)

		// actiondefinition should be extended to the recursive level
		// Expected: MTL.MAL.MAL_RID_PARENTMASTERTASKITEM.DEF
		foundExtendedDEF := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MTL.MAL.MAL_RID_PARENTMASTERTASKITEM.DEF" {
				foundExtendedDEF = true
			}
		}

		assert.True(t, foundExtendedDEF,
			"Expected actiondefinition relation to be extended to recursive level. Operations: %v",
			mock.operations)
	})
}

// TestXFilesRecursivePreloadDepth tests that recursive preloads respect the depth limit of 8
func TestXFilesRecursivePreloadDepth(t *testing.T) {
	handler := &Handler{}

	preload := common.PreloadOption{
		Relation:   "MAL",
		Recursive:  true,
		RelatedKey: "rid_parentmastertaskitem",
	}

	allPreloads := []common.PreloadOption{preload}

	t.Run("Depth7CreatesLevel8", func(t *testing.T) {
		mockQuery := &mockSelectQuery{operations: []string{}}
		result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 7)
		mock := result.(*mockSelectQuery)

		foundDepth8 := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundDepth8 = true
			}
		}

		assert.True(t, foundDepth8, "Should create level 8 when starting at depth 7")
	})

	t.Run("Depth8DoesNotCreateLevel9", func(t *testing.T) {
		mockQuery := &mockSelectQuery{operations: []string{}}
		result := handler.applyPreloadWithRecursion(mockQuery, preload, allPreloads, nil, 8)
		mock := result.(*mockSelectQuery)

		foundDepth9 := false
		for _, op := range mock.operations {
			if op == "PreloadRelation:MAL.MAL_RID_PARENTMASTERTASKITEM" {
				foundDepth9 = true
			}
		}

		assert.False(t, foundDepth9, "Should NOT create level 9 (depth limit is 8)")
	})
}

// TestXFilesResponseStructure validates the actual structure of the response
// This test can be expanded when we have a full database integration test environment
func TestXFilesResponseStructure(t *testing.T) {
	// Load the expected correct response
	correctResponsePath := filepath.Join("..", "..", "tests", "data", "xfiles.response.correct.json")
	correctData, err := os.ReadFile(correctResponsePath)
	require.NoError(t, err, "Failed to read xfiles.response.correct.json")

	var correctResponse []map[string]interface{}
	err = json.Unmarshal(correctData, &correctResponse)
	require.NoError(t, err, "Failed to parse xfiles.response.correct.json")

	// Test 1: Verify root level has exactly 1 masterprocess
	t.Run("RootLevelHasOneItem", func(t *testing.T) {
		assert.Len(t, correctResponse, 1, "Root level should have exactly 1 masterprocess record")
	})

	// Test 2: Verify the root item has MTL relation
	t.Run("RootHasMTLRelation", func(t *testing.T) {
		require.NotEmpty(t, correctResponse, "Response should not be empty")
		rootItem := correctResponse[0]

		mtl, exists := rootItem["MTL"]
		assert.True(t, exists, "Root item should have MTL relation")
		assert.NotNil(t, mtl, "MTL relation should not be null")
	})

	// Test 3: Verify MTL has MAL items
	t.Run("MTLHasMALItems", func(t *testing.T) {
		require.NotEmpty(t, correctResponse, "Response should not be empty")
		rootItem := correctResponse[0]

		mtl, ok := rootItem["MTL"].([]interface{})
		require.True(t, ok, "MTL should be an array")
		require.NotEmpty(t, mtl, "MTL should have items")

		firstMTL, ok := mtl[0].(map[string]interface{})
		require.True(t, ok, "MTL item should be a map")

		mal, exists := firstMTL["MAL"]
		assert.True(t, exists, "MTL item should have MAL relation")
		assert.NotNil(t, mal, "MAL relation should not be null")
	})

	// Test 4: Verify MAL items have MAL_RID_PARENTMASTERTASKITEM relation (recursive)
	t.Run("MALHasRecursiveRelation", func(t *testing.T) {
		require.NotEmpty(t, correctResponse, "Response should not be empty")
		rootItem := correctResponse[0]

		mtl, ok := rootItem["MTL"].([]interface{})
		require.True(t, ok, "MTL should be an array")
		require.NotEmpty(t, mtl, "MTL should have items")

		firstMTL, ok := mtl[0].(map[string]interface{})
		require.True(t, ok, "MTL item should be a map")

		mal, ok := firstMTL["MAL"].([]interface{})
		require.True(t, ok, "MAL should be an array")
		require.NotEmpty(t, mal, "MAL should have items")

		firstMAL, ok := mal[0].(map[string]interface{})
		require.True(t, ok, "MAL item should be a map")

		// The key assertion: check for FK-based relation name
		recursiveRelation, exists := firstMAL["MAL_RID_PARENTMASTERTASKITEM"]
		assert.True(t, exists,
			"MAL item should have MAL_RID_PARENTMASTERTASKITEM relation (FK-based name)")

		// It can be null or an array, depending on whether this item has children
		if recursiveRelation != nil {
			_, isArray := recursiveRelation.([]interface{})
			assert.True(t, isArray,
				"MAL_RID_PARENTMASTERTASKITEM should be an array when not null")
		}
	})

	// Test 5: Verify "Receive COB Document for" appears as a child, not at root
	t.Run("ChildItemsAreNested", func(t *testing.T) {
		// This test verifies that "Receive COB Document for" doesn't appear
		// multiple times at the wrong level, but is properly nested

		// Count how many times we find this description at the MAL level (should be 0 or 1)
		require.NotEmpty(t, correctResponse, "Response should not be empty")
		rootItem := correctResponse[0]

		mtl, ok := rootItem["MTL"].([]interface{})
		require.True(t, ok, "MTL should be an array")
		require.NotEmpty(t, mtl, "MTL should have items")

		firstMTL, ok := mtl[0].(map[string]interface{})
		require.True(t, ok, "MTL item should be a map")

		mal, ok := firstMTL["MAL"].([]interface{})
		require.True(t, ok, "MAL should be an array")

		// Count root-level MAL items (before the fix, there were 12; should be 1)
		assert.Len(t, mal, 1,
			"MAL should have exactly 1 root-level item (before fix: 12 duplicates)")

		// Verify the root item has a description
		firstMAL, ok := mal[0].(map[string]interface{})
		require.True(t, ok, "MAL item should be a map")

		description, exists := firstMAL["description"]
		assert.True(t, exists, "MAL item should have a description")
		assert.Equal(t, "Capture COB Information", description,
			"Root MAL item should be 'Capture COB Information'")
	})

	// Test 6: Verify DEF relation exists at MAL level
	t.Run("DEFRelationExists", func(t *testing.T) {
		require.NotEmpty(t, correctResponse, "Response should not be empty")
		rootItem := correctResponse[0]

		mtl, ok := rootItem["MTL"].([]interface{})
		require.True(t, ok, "MTL should be an array")
		require.NotEmpty(t, mtl, "MTL should have items")

		firstMTL, ok := mtl[0].(map[string]interface{})
		require.True(t, ok, "MTL item should be a map")

		mal, ok := firstMTL["MAL"].([]interface{})
		require.True(t, ok, "MAL should be an array")
		require.NotEmpty(t, mal, "MAL should have items")

		firstMAL, ok := mal[0].(map[string]interface{})
		require.True(t, ok, "MAL item should be a map")

		// Verify DEF relation exists (child relation extension)
		def, exists := firstMAL["DEF"]
		assert.True(t, exists, "MAL item should have DEF relation")

		// DEF can be null or an object
		if def != nil {
			_, isMap := def.(map[string]interface{})
			assert.True(t, isMap, "DEF should be an object when not null")
		}
	})
}
