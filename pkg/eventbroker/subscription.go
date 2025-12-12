package eventbroker

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// SubscriptionID uniquely identifies a subscription
type SubscriptionID string

// subscription represents a single subscription with its handler and pattern
type subscription struct {
	id      SubscriptionID
	pattern string
	handler EventHandler
}

// subscriptionManager manages event subscriptions and pattern matching
type subscriptionManager struct {
	mu            sync.RWMutex
	subscriptions map[SubscriptionID]*subscription
	nextID        atomic.Uint64
}

// newSubscriptionManager creates a new subscription manager
func newSubscriptionManager() *subscriptionManager {
	return &subscriptionManager{
		subscriptions: make(map[SubscriptionID]*subscription),
	}
}

// Subscribe adds a new subscription
func (sm *subscriptionManager) Subscribe(pattern string, handler EventHandler) (SubscriptionID, error) {
	if pattern == "" {
		return "", fmt.Errorf("pattern cannot be empty")
	}
	if handler == nil {
		return "", fmt.Errorf("handler cannot be nil")
	}

	id := SubscriptionID(fmt.Sprintf("sub-%d", sm.nextID.Add(1)))

	sm.mu.Lock()
	sm.subscriptions[id] = &subscription{
		id:      id,
		pattern: pattern,
		handler: handler,
	}
	sm.mu.Unlock()

	logger.Info("Subscribed to pattern '%s' with ID: %s", pattern, id)
	return id, nil
}

// Unsubscribe removes a subscription
func (sm *subscriptionManager) Unsubscribe(id SubscriptionID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.subscriptions[id]; !exists {
		return fmt.Errorf("subscription not found: %s", id)
	}

	delete(sm.subscriptions, id)
	logger.Info("Unsubscribed: %s", id)
	return nil
}

// GetMatching returns all handlers that match the event type
func (sm *subscriptionManager) GetMatching(eventType string) []EventHandler {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var handlers []EventHandler
	for _, sub := range sm.subscriptions {
		if matchPattern(sub.pattern, eventType) {
			handlers = append(handlers, sub.handler)
		}
	}

	return handlers
}

// Count returns the number of active subscriptions
func (sm *subscriptionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.subscriptions)
}

// Clear removes all subscriptions
func (sm *subscriptionManager) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subscriptions = make(map[SubscriptionID]*subscription)
	logger.Info("Cleared all subscriptions")
}

// matchPattern implements glob-style pattern matching for event types
// Patterns:
//   - "*" matches any single segment
//   - "a.b.c" matches exactly "a.b.c"
//   - "a.*.c" matches "a.anything.c"
//   - "a.b.*" matches any operation on a.b
//   - "*" matches everything
//
// Event type format: schema.entity.operation (e.g., "public.users.create")
func matchPattern(pattern, eventType string) bool {
	// Wildcard matches everything
	if pattern == "*" {
		return true
	}

	// Exact match
	if pattern == eventType {
		return true
	}

	// Split pattern and event type by dots
	patternParts := strings.Split(pattern, ".")
	eventParts := strings.Split(eventType, ".")

	// Different number of parts can only match if pattern has wildcards
	if len(patternParts) != len(eventParts) {
		return false
	}

	// Match each part
	for i := range patternParts {
		if patternParts[i] != "*" && patternParts[i] != eventParts[i] {
			return false
		}
	}

	return true
}
