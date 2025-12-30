package websocketspec

import (
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Subscription represents a subscription to entity changes
type Subscription struct {
	// ID is the unique subscription identifier
	ID string

	// ConnectionID is the ID of the connection that owns this subscription
	ConnectionID string

	// Schema is the database schema
	Schema string

	// Entity is the table/model name
	Entity string

	// Options contains filters and other query options
	Options *common.RequestOptions

	// Active indicates if the subscription is active
	Active bool
}

// SubscriptionManager manages all subscriptions
type SubscriptionManager struct {
	// subscriptions maps subscription ID to subscription
	subscriptions map[string]*Subscription

	// entitySubscriptions maps "schema.entity" to list of subscription IDs
	entitySubscriptions map[string][]string

	// mu protects the maps
	mu sync.RWMutex
}

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions:       make(map[string]*Subscription),
		entitySubscriptions: make(map[string][]string),
	}
}

// Subscribe creates a new subscription
func (sm *SubscriptionManager) Subscribe(id, connID, schema, entity string, options *common.RequestOptions) *Subscription {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sub := &Subscription{
		ID:           id,
		ConnectionID: connID,
		Schema:       schema,
		Entity:       entity,
		Options:      options,
		Active:       true,
	}

	// Store subscription
	sm.subscriptions[id] = sub

	// Index by entity
	key := makeEntityKey(schema, entity)
	sm.entitySubscriptions[key] = append(sm.entitySubscriptions[key], id)

	logger.Info("[WebSocketSpec] Subscription created: %s for %s.%s (conn: %s)", id, schema, entity, connID)
	return sub
}

// Unsubscribe removes a subscription
func (sm *SubscriptionManager) Unsubscribe(subID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sub, exists := sm.subscriptions[subID]
	if !exists {
		return false
	}

	// Remove from entity index
	key := makeEntityKey(sub.Schema, sub.Entity)
	if subs, ok := sm.entitySubscriptions[key]; ok {
		newSubs := make([]string, 0, len(subs)-1)
		for _, id := range subs {
			if id != subID {
				newSubs = append(newSubs, id)
			}
		}
		if len(newSubs) > 0 {
			sm.entitySubscriptions[key] = newSubs
		} else {
			delete(sm.entitySubscriptions, key)
		}
	}

	// Remove subscription
	delete(sm.subscriptions, subID)

	logger.Info("[WebSocketSpec] Subscription removed: %s", subID)
	return true
}

// GetSubscription retrieves a subscription by ID
func (sm *SubscriptionManager) GetSubscription(subID string) (*Subscription, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sub, ok := sm.subscriptions[subID]
	return sub, ok
}

// GetSubscriptionsByEntity retrieves all subscriptions for an entity
func (sm *SubscriptionManager) GetSubscriptionsByEntity(schema, entity string) []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	key := makeEntityKey(schema, entity)
	subIDs, ok := sm.entitySubscriptions[key]
	if !ok {
		return nil
	}

	result := make([]*Subscription, 0, len(subIDs))
	for _, subID := range subIDs {
		if sub, ok := sm.subscriptions[subID]; ok && sub.Active {
			result = append(result, sub)
		}
	}

	return result
}

// GetSubscriptionsByConnection retrieves all subscriptions for a connection
func (sm *SubscriptionManager) GetSubscriptionsByConnection(connID string) []*Subscription {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]*Subscription, 0)
	for _, sub := range sm.subscriptions {
		if sub.ConnectionID == connID && sub.Active {
			result = append(result, sub)
		}
	}

	return result
}

// Count returns the total number of active subscriptions
func (sm *SubscriptionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.subscriptions)
}

// CountForEntity returns the number of subscriptions for a specific entity
func (sm *SubscriptionManager) CountForEntity(schema, entity string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	key := makeEntityKey(schema, entity)
	return len(sm.entitySubscriptions[key])
}

// MatchesFilters checks if data matches the subscription's filters
func (s *Subscription) MatchesFilters(data interface{}) bool {
	// If no filters, match everything
	if s.Options == nil || len(s.Options.Filters) == 0 {
		return true
	}

	// TODO: Implement filter matching logic
	// For now, return true (send all notifications)
	// In a full implementation, you would:
	// 1. Convert data to a map
	// 2. Evaluate each filter against the data
	// 3. Return true only if all filters match

	return true
}

// makeEntityKey creates a key for entity indexing
func makeEntityKey(schema, entity string) string {
	if schema == "" {
		return entity
	}
	return schema + "." + entity
}
