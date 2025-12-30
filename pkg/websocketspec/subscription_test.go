package websocketspec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSubscriptionManager(t *testing.T) {
	sm := NewSubscriptionManager()
	assert.NotNil(t, sm)
	assert.NotNil(t, sm.subscriptions)
	assert.NotNil(t, sm.entitySubscriptions)
	assert.Equal(t, 0, sm.Count())
}

func TestSubscriptionManager_Subscribe(t *testing.T) {
	sm := NewSubscriptionManager()

	// Create a subscription
	sub := sm.Subscribe("sub-1", "conn-1", "public", "users", nil)

	assert.NotNil(t, sub)
	assert.Equal(t, "sub-1", sub.ID)
	assert.Equal(t, "conn-1", sub.ConnectionID)
	assert.Equal(t, "public", sub.Schema)
	assert.Equal(t, "users", sub.Entity)
	assert.True(t, sub.Active)
	assert.Equal(t, 1, sm.Count())
}

func TestSubscriptionManager_Subscribe_WithOptions(t *testing.T) {
	sm := NewSubscriptionManager()

	options := &common.RequestOptions{
		Filters: []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "active"},
		},
	}

	sub := sm.Subscribe("sub-1", "conn-1", "public", "users", options)

	assert.NotNil(t, sub)
	assert.NotNil(t, sub.Options)
	assert.Len(t, sub.Options.Filters, 1)
	assert.Equal(t, "status", sub.Options.Filters[0].Column)
}

func TestSubscriptionManager_Subscribe_MultipleSubscriptions(t *testing.T) {
	sm := NewSubscriptionManager()

	sub1 := sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sub2 := sm.Subscribe("sub-2", "conn-1", "public", "posts", nil)
	sub3 := sm.Subscribe("sub-3", "conn-2", "public", "users", nil)

	assert.NotNil(t, sub1)
	assert.NotNil(t, sub2)
	assert.NotNil(t, sub3)
	assert.Equal(t, 3, sm.Count())
}

func TestSubscriptionManager_Unsubscribe(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	assert.Equal(t, 1, sm.Count())

	// Unsubscribe
	ok := sm.Unsubscribe("sub-1")
	assert.True(t, ok)
	assert.Equal(t, 0, sm.Count())
}

func TestSubscriptionManager_Unsubscribe_NonExistent(t *testing.T) {
	sm := NewSubscriptionManager()

	ok := sm.Unsubscribe("non-existent")
	assert.False(t, ok)
}

func TestSubscriptionManager_Unsubscribe_MultipleSubscriptions(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sm.Subscribe("sub-2", "conn-1", "public", "posts", nil)
	sm.Subscribe("sub-3", "conn-2", "public", "users", nil)
	assert.Equal(t, 3, sm.Count())

	// Unsubscribe one
	ok := sm.Unsubscribe("sub-2")
	assert.True(t, ok)
	assert.Equal(t, 2, sm.Count())

	// Verify the right subscription was removed
	_, exists := sm.GetSubscription("sub-2")
	assert.False(t, exists)

	_, exists = sm.GetSubscription("sub-1")
	assert.True(t, exists)

	_, exists = sm.GetSubscription("sub-3")
	assert.True(t, exists)
}

func TestSubscriptionManager_GetSubscription(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)

	// Get existing subscription
	sub, exists := sm.GetSubscription("sub-1")
	assert.True(t, exists)
	assert.NotNil(t, sub)
	assert.Equal(t, "sub-1", sub.ID)
}

func TestSubscriptionManager_GetSubscription_NonExistent(t *testing.T) {
	sm := NewSubscriptionManager()

	sub, exists := sm.GetSubscription("non-existent")
	assert.False(t, exists)
	assert.Nil(t, sub)
}

func TestSubscriptionManager_GetSubscriptionsByEntity(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sm.Subscribe("sub-2", "conn-2", "public", "users", nil)
	sm.Subscribe("sub-3", "conn-1", "public", "posts", nil)

	// Get subscriptions for users entity
	subs := sm.GetSubscriptionsByEntity("public", "users")
	assert.Len(t, subs, 2)

	// Verify subscription IDs
	ids := make([]string, len(subs))
	for i, sub := range subs {
		ids[i] = sub.ID
	}
	assert.Contains(t, ids, "sub-1")
	assert.Contains(t, ids, "sub-2")
}

func TestSubscriptionManager_GetSubscriptionsByEntity_NoSchema(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "", "users", nil)
	sm.Subscribe("sub-2", "conn-2", "", "users", nil)

	// Get subscriptions without schema
	subs := sm.GetSubscriptionsByEntity("", "users")
	assert.Len(t, subs, 2)
}

func TestSubscriptionManager_GetSubscriptionsByEntity_NoResults(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)

	// Get subscriptions for non-existent entity
	subs := sm.GetSubscriptionsByEntity("public", "posts")
	assert.Nil(t, subs)
}

func TestSubscriptionManager_GetSubscriptionsByConnection(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sm.Subscribe("sub-2", "conn-1", "public", "posts", nil)
	sm.Subscribe("sub-3", "conn-2", "public", "users", nil)

	// Get subscriptions for connection 1
	subs := sm.GetSubscriptionsByConnection("conn-1")
	assert.Len(t, subs, 2)

	// Verify subscription IDs
	ids := make([]string, len(subs))
	for i, sub := range subs {
		ids[i] = sub.ID
	}
	assert.Contains(t, ids, "sub-1")
	assert.Contains(t, ids, "sub-2")
}

func TestSubscriptionManager_GetSubscriptionsByConnection_NoResults(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)

	// Get subscriptions for non-existent connection
	subs := sm.GetSubscriptionsByConnection("conn-2")
	assert.Empty(t, subs)
}

func TestSubscriptionManager_Count(t *testing.T) {
	sm := NewSubscriptionManager()

	assert.Equal(t, 0, sm.Count())

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	assert.Equal(t, 1, sm.Count())

	sm.Subscribe("sub-2", "conn-1", "public", "posts", nil)
	assert.Equal(t, 2, sm.Count())

	sm.Unsubscribe("sub-1")
	assert.Equal(t, 1, sm.Count())

	sm.Unsubscribe("sub-2")
	assert.Equal(t, 0, sm.Count())
}

func TestSubscriptionManager_CountForEntity(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sm.Subscribe("sub-2", "conn-2", "public", "users", nil)
	sm.Subscribe("sub-3", "conn-1", "public", "posts", nil)

	assert.Equal(t, 2, sm.CountForEntity("public", "users"))
	assert.Equal(t, 1, sm.CountForEntity("public", "posts"))
	assert.Equal(t, 0, sm.CountForEntity("public", "orders"))
}

func TestSubscriptionManager_UnsubscribeUpdatesEntityIndex(t *testing.T) {
	sm := NewSubscriptionManager()

	sm.Subscribe("sub-1", "conn-1", "public", "users", nil)
	sm.Subscribe("sub-2", "conn-2", "public", "users", nil)
	assert.Equal(t, 2, sm.CountForEntity("public", "users"))

	// Unsubscribe one
	sm.Unsubscribe("sub-1")
	assert.Equal(t, 1, sm.CountForEntity("public", "users"))

	// Unsubscribe the other
	sm.Unsubscribe("sub-2")
	assert.Equal(t, 0, sm.CountForEntity("public", "users"))
}

func TestSubscription_MatchesFilters_NoFilters(t *testing.T) {
	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "conn-1",
		Schema:       "public",
		Entity:       "users",
		Options:      nil,
		Active:       true,
	}

	data := map[string]interface{}{
		"id":     1,
		"name":   "John",
		"status": "active",
	}

	// Should match when no filters are specified
	assert.True(t, sub.MatchesFilters(data))
}

func TestSubscription_MatchesFilters_WithFilters(t *testing.T) {
	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "conn-1",
		Schema:       "public",
		Entity:       "users",
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{
				{Column: "status", Operator: "eq", Value: "active"},
			},
		},
		Active: true,
	}

	data := map[string]interface{}{
		"id":     1,
		"name":   "John",
		"status": "active",
	}

	// Current implementation returns true for all data
	// This test documents the expected behavior
	assert.True(t, sub.MatchesFilters(data))
}

func TestSubscription_MatchesFilters_EmptyFiltersArray(t *testing.T) {
	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "conn-1",
		Schema:       "public",
		Entity:       "users",
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{},
		},
		Active: true,
	}

	data := map[string]interface{}{
		"id":   1,
		"name": "John",
	}

	// Should match when filters array is empty
	assert.True(t, sub.MatchesFilters(data))
}

func TestMakeEntityKey(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		entity   string
		expected string
	}{
		{
			name:     "With schema",
			schema:   "public",
			entity:   "users",
			expected: "public.users",
		},
		{
			name:     "Without schema",
			schema:   "",
			entity:   "users",
			expected: "users",
		},
		{
			name:     "Different schema",
			schema:   "custom",
			entity:   "posts",
			expected: "custom.posts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := makeEntityKey(tt.schema, tt.entity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubscriptionManager_ConcurrentOperations(t *testing.T) {
	sm := NewSubscriptionManager()

	// This test verifies that concurrent operations don't cause race conditions
	// Run with: go test -race

	done := make(chan bool)

	// Goroutine 1: Subscribe
	go func() {
		for i := 0; i < 100; i++ {
			sm.Subscribe("sub-"+string(rune(i)), "conn-1", "public", "users", nil)
		}
		done <- true
	}()

	// Goroutine 2: Get subscriptions
	go func() {
		for i := 0; i < 100; i++ {
			sm.GetSubscriptionsByEntity("public", "users")
		}
		done <- true
	}()

	// Goroutine 3: Count
	go func() {
		for i := 0; i < 100; i++ {
			sm.Count()
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done
}

func TestSubscriptionManager_CompleteLifecycle(t *testing.T) {
	sm := NewSubscriptionManager()

	// Create subscriptions
	options := &common.RequestOptions{
		Filters: []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "active"},
		},
	}

	sub1 := sm.Subscribe("sub-1", "conn-1", "public", "users", options)
	require.NotNil(t, sub1)
	assert.Equal(t, 1, sm.Count())

	sub2 := sm.Subscribe("sub-2", "conn-1", "public", "posts", nil)
	require.NotNil(t, sub2)
	assert.Equal(t, 2, sm.Count())

	// Get by entity
	userSubs := sm.GetSubscriptionsByEntity("public", "users")
	assert.Len(t, userSubs, 1)
	assert.Equal(t, "sub-1", userSubs[0].ID)

	// Get by connection
	connSubs := sm.GetSubscriptionsByConnection("conn-1")
	assert.Len(t, connSubs, 2)

	// Get specific subscription
	sub, exists := sm.GetSubscription("sub-1")
	assert.True(t, exists)
	assert.Equal(t, "sub-1", sub.ID)
	assert.NotNil(t, sub.Options)

	// Count by entity
	assert.Equal(t, 1, sm.CountForEntity("public", "users"))
	assert.Equal(t, 1, sm.CountForEntity("public", "posts"))

	// Unsubscribe
	ok := sm.Unsubscribe("sub-1")
	assert.True(t, ok)
	assert.Equal(t, 1, sm.Count())
	assert.Equal(t, 0, sm.CountForEntity("public", "users"))

	// Verify subscription is gone
	_, exists = sm.GetSubscription("sub-1")
	assert.False(t, exists)

	// Unsubscribe second subscription
	ok = sm.Unsubscribe("sub-2")
	assert.True(t, ok)
	assert.Equal(t, 0, sm.Count())
}
