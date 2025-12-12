package eventbroker

import (
	"encoding/json"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// CRUDHookConfig configures which CRUD operations should trigger events
type CRUDHookConfig struct {
	EnableCreate bool
	EnableRead   bool
	EnableUpdate bool
	EnableDelete bool
}

// DefaultCRUDHookConfig returns default configuration (all enabled)
func DefaultCRUDHookConfig() *CRUDHookConfig {
	return &CRUDHookConfig{
		EnableCreate: true,
		EnableRead:   false, // Typically disabled for performance
		EnableUpdate: true,
		EnableDelete: true,
	}
}

// RegisterCRUDHooks registers event hooks for CRUD operations
// This integrates with the restheadspec.HookRegistry to automatically
// capture database events
func RegisterCRUDHooks(broker Broker, hookRegistry *restheadspec.HookRegistry, config *CRUDHookConfig) error {
	if broker == nil {
		return fmt.Errorf("broker cannot be nil")
	}
	if hookRegistry == nil {
		return fmt.Errorf("hookRegistry cannot be nil")
	}
	if config == nil {
		config = DefaultCRUDHookConfig()
	}

	// Create hook handler factory
	createHookHandler := func(operation string) restheadspec.HookFunc {
		return func(hookCtx *restheadspec.HookContext) error {
			// Get user context from Go context
			userCtx, ok := security.GetUserContext(hookCtx.Context)
			if !ok || userCtx == nil {
				logger.Debug("No user context found in hook")
				userCtx = &security.UserContext{} // Empty user context
			}

			// Create event
			event := NewEvent(EventSourceDatabase, EventType(hookCtx.Schema, hookCtx.Entity, operation))
			event.InstanceID = broker.InstanceID()
			event.UserID = userCtx.UserID
			event.SessionID = userCtx.SessionID
			event.Schema = hookCtx.Schema
			event.Entity = hookCtx.Entity
			event.Operation = operation

			// Set payload based on operation
			var payload interface{}
			switch operation {
			case "create":
				payload = hookCtx.Result
			case "read":
				payload = hookCtx.Result
			case "update":
				payload = map[string]interface{}{
					"id":   hookCtx.ID,
					"data": hookCtx.Data,
				}
			case "delete":
				payload = map[string]interface{}{
					"id": hookCtx.ID,
				}
			}

			if payload != nil {
				if err := event.SetPayload(payload); err != nil {
					logger.Error("Failed to set event payload: %v", err)
					payload = map[string]interface{}{"error": "failed to serialize payload"}
					event.Payload, _ = json.Marshal(payload)
				}
			}

			// Add metadata
			if userCtx.UserName != "" {
				event.Metadata["user_name"] = userCtx.UserName
			}
			if userCtx.Email != "" {
				event.Metadata["user_email"] = userCtx.Email
			}
			if len(userCtx.Roles) > 0 {
				event.Metadata["user_roles"] = userCtx.Roles
			}
			event.Metadata["table_name"] = hookCtx.TableName

			// Publish asynchronously to not block CRUD operation
			if err := broker.PublishAsync(hookCtx.Context, event); err != nil {
				logger.Error("Failed to publish %s event for %s.%s: %v",
					operation, hookCtx.Schema, hookCtx.Entity, err)
				// Don't fail the CRUD operation if event publishing fails
				return nil
			}

			logger.Debug("Published %s event for %s.%s (ID: %s)",
				operation, hookCtx.Schema, hookCtx.Entity, event.ID)
			return nil
		}
	}

	// Register hooks based on configuration
	if config.EnableCreate {
		hookRegistry.Register(restheadspec.AfterCreate, createHookHandler("create"))
		logger.Info("Registered event hook for CREATE operations")
	}

	if config.EnableRead {
		hookRegistry.Register(restheadspec.AfterRead, createHookHandler("read"))
		logger.Info("Registered event hook for READ operations")
	}

	if config.EnableUpdate {
		hookRegistry.Register(restheadspec.AfterUpdate, createHookHandler("update"))
		logger.Info("Registered event hook for UPDATE operations")
	}

	if config.EnableDelete {
		hookRegistry.Register(restheadspec.AfterDelete, createHookHandler("delete"))
		logger.Info("Registered event hook for DELETE operations")
	}

	return nil
}
