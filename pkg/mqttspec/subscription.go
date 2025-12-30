package mqttspec

import (
	"github.com/bitechdev/ResolveSpec/pkg/websocketspec"
)

// Subscription types - aliases to websocketspec for subscription management
type (
	// Subscription represents an active subscription to entity changes
	// The key difference for MQTT: notifications are delivered via MQTT publish
	// to spec/{client_id}/notify/{subscription_id} instead of WebSocket send
	Subscription = websocketspec.Subscription

	// SubscriptionManager manages all active subscriptions
	SubscriptionManager = websocketspec.SubscriptionManager
)

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager() *SubscriptionManager {
	return websocketspec.NewSubscriptionManager()
}
