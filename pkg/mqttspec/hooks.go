package mqttspec

import (
	"github.com/bitechdev/ResolveSpec/pkg/websocketspec"
)

// Hook types - aliases to websocketspec for lifecycle hook consistency
type (
	// HookType defines the type of lifecycle hook
	HookType = websocketspec.HookType

	// HookFunc is a function that executes during a lifecycle hook
	HookFunc = websocketspec.HookFunc

	// HookContext contains all context for hook execution
	// Note: For MQTT, the Client is stored in Metadata["mqtt_client"]
	HookContext = websocketspec.HookContext

	// HookRegistry manages all registered hooks
	HookRegistry = websocketspec.HookRegistry
)

// Hook type constants - all 12 lifecycle hooks
const (
	// CRUD operation hooks
	BeforeRead   = websocketspec.BeforeRead
	AfterRead    = websocketspec.AfterRead
	BeforeCreate = websocketspec.BeforeCreate
	AfterCreate  = websocketspec.AfterCreate
	BeforeUpdate = websocketspec.BeforeUpdate
	AfterUpdate  = websocketspec.AfterUpdate
	BeforeDelete = websocketspec.BeforeDelete
	AfterDelete  = websocketspec.AfterDelete

	// Subscription hooks
	BeforeSubscribe   = websocketspec.BeforeSubscribe
	AfterSubscribe    = websocketspec.AfterSubscribe
	BeforeUnsubscribe = websocketspec.BeforeUnsubscribe
	AfterUnsubscribe  = websocketspec.AfterUnsubscribe

	// Connection hooks
	BeforeConnect    = websocketspec.BeforeConnect
	AfterConnect     = websocketspec.AfterConnect
	BeforeDisconnect = websocketspec.BeforeDisconnect
	AfterDisconnect  = websocketspec.AfterDisconnect
)

// NewHookRegistry creates a new hook registry
func NewHookRegistry() *HookRegistry {
	return websocketspec.NewHookRegistry()
}
