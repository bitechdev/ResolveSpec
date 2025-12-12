package websocketspec

import (
	"context"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// HookType represents the type of lifecycle hook
type HookType string

const (
	// BeforeRead is called before a read operation
	BeforeRead HookType = "before_read"
	// AfterRead is called after a read operation
	AfterRead HookType = "after_read"

	// BeforeCreate is called before a create operation
	BeforeCreate HookType = "before_create"
	// AfterCreate is called after a create operation
	AfterCreate HookType = "after_create"

	// BeforeUpdate is called before an update operation
	BeforeUpdate HookType = "before_update"
	// AfterUpdate is called after an update operation
	AfterUpdate HookType = "after_update"

	// BeforeDelete is called before a delete operation
	BeforeDelete HookType = "before_delete"
	// AfterDelete is called after a delete operation
	AfterDelete HookType = "after_delete"

	// BeforeSubscribe is called before creating a subscription
	BeforeSubscribe HookType = "before_subscribe"
	// AfterSubscribe is called after creating a subscription
	AfterSubscribe HookType = "after_subscribe"

	// BeforeUnsubscribe is called before removing a subscription
	BeforeUnsubscribe HookType = "before_unsubscribe"
	// AfterUnsubscribe is called after removing a subscription
	AfterUnsubscribe HookType = "after_unsubscribe"

	// BeforeConnect is called when a new connection is established
	BeforeConnect HookType = "before_connect"
	// AfterConnect is called after a connection is established
	AfterConnect HookType = "after_connect"

	// BeforeDisconnect is called before a connection is closed
	BeforeDisconnect HookType = "before_disconnect"
	// AfterDisconnect is called after a connection is closed
	AfterDisconnect HookType = "after_disconnect"
)

// HookContext contains context information for hook execution
type HookContext struct {
	// Context is the request context
	Context context.Context

	// Handler provides access to the handler, database, and registry
	Handler *Handler

	// Connection is the WebSocket connection
	Connection *Connection

	// Message is the original message
	Message *Message

	// Schema is the database schema
	Schema string

	// Entity is the table/model name
	Entity string

	// TableName is the actual database table name
	TableName string

	// Model is the registered model instance
	Model interface{}

	// ModelPtr is a pointer to the model for queries
	ModelPtr interface{}

	// Options contains the parsed request options
	Options *common.RequestOptions

	// ID is the record ID for single-record operations
	ID string

	// Data is the request data (for create/update operations)
	Data interface{}

	// Result is the operation result (for after hooks)
	Result interface{}

	// Subscription is the subscription being created/removed
	Subscription *Subscription

	// Error is any error that occurred (for after hooks)
	Error error

	// Metadata is additional context data
	Metadata map[string]interface{}
}

// HookFunc is a function that processes a hook
type HookFunc func(*HookContext) error

// HookRegistry manages lifecycle hooks
type HookRegistry struct {
	hooks map[HookType][]HookFunc
}

// NewHookRegistry creates a new hook registry
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

// Register registers a hook function for a specific hook type
func (hr *HookRegistry) Register(hookType HookType, fn HookFunc) {
	hr.hooks[hookType] = append(hr.hooks[hookType], fn)
}

// RegisterBefore registers a hook that runs before an operation
// Convenience method for BeforeRead, BeforeCreate, BeforeUpdate, BeforeDelete
func (hr *HookRegistry) RegisterBefore(operation OperationType, fn HookFunc) {
	switch operation {
	case OperationRead:
		hr.Register(BeforeRead, fn)
	case OperationCreate:
		hr.Register(BeforeCreate, fn)
	case OperationUpdate:
		hr.Register(BeforeUpdate, fn)
	case OperationDelete:
		hr.Register(BeforeDelete, fn)
	case OperationSubscribe:
		hr.Register(BeforeSubscribe, fn)
	case OperationUnsubscribe:
		hr.Register(BeforeUnsubscribe, fn)
	}
}

// RegisterAfter registers a hook that runs after an operation
// Convenience method for AfterRead, AfterCreate, AfterUpdate, AfterDelete
func (hr *HookRegistry) RegisterAfter(operation OperationType, fn HookFunc) {
	switch operation {
	case OperationRead:
		hr.Register(AfterRead, fn)
	case OperationCreate:
		hr.Register(AfterCreate, fn)
	case OperationUpdate:
		hr.Register(AfterUpdate, fn)
	case OperationDelete:
		hr.Register(AfterDelete, fn)
	case OperationSubscribe:
		hr.Register(AfterSubscribe, fn)
	case OperationUnsubscribe:
		hr.Register(AfterUnsubscribe, fn)
	}
}

// Execute runs all hooks for a specific type
func (hr *HookRegistry) Execute(hookType HookType, ctx *HookContext) error {
	hooks, exists := hr.hooks[hookType]
	if !exists {
		return nil
	}

	for _, hook := range hooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}

	return nil
}

// HasHooks checks if any hooks are registered for a hook type
func (hr *HookRegistry) HasHooks(hookType HookType) bool {
	hooks, exists := hr.hooks[hookType]
	return exists && len(hooks) > 0
}

// Clear removes all hooks of a specific type
func (hr *HookRegistry) Clear(hookType HookType) {
	delete(hr.hooks, hookType)
}

// ClearAll removes all registered hooks
func (hr *HookRegistry) ClearAll() {
	hr.hooks = make(map[HookType][]HookFunc)
}
