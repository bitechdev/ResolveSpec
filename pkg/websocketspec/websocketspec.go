// Package websocketspec provides a WebSocket-based API specification for real-time
// CRUD operations with bidirectional communication and subscription support.
//
// # Key Features
//
//   - Real-time bidirectional communication over WebSocket
//   - CRUD operations (Create, Read, Update, Delete)
//   - Real-time subscriptions with filtering
//   - Lifecycle hooks for all operations
//   - Database-agnostic: Works with GORM and Bun ORM through adapters
//   - Automatic change notifications to subscribers
//   - Connection and subscription management
//
// # Message Protocol
//
// WebSocketSpec uses JSON messages for communication:
//
//	{
//	  "id": "unique-message-id",
//	  "type": "request|response|notification|subscription",
//	  "operation": "read|create|update|delete|subscribe|unsubscribe",
//	  "schema": "public",
//	  "entity": "users",
//	  "data": {...},
//	  "options": {
//	    "filters": [...],
//	    "columns": [...],
//	    "preload": [...],
//	    "sort": [...],
//	    "limit": 10
//	  }
//	}
//
// # Usage Example
//
//	// Create handler with GORM
//	handler := websocketspec.NewHandlerWithGORM(db)
//
//	// Register models
//	handler.Registry.RegisterModel("public.users", &User{})
//
//	// Setup WebSocket endpoint
//	http.HandleFunc("/ws", handler.HandleWebSocket)
//
//	// Start server
//	http.ListenAndServe(":8080", nil)
//
// # Client Example
//
//	// Connect to WebSocket
//	ws := new WebSocket("ws://localhost:8080/ws")
//
//	// Send read request
//	ws.send(JSON.stringify({
//	  id: "msg-1",
//	  type: "request",
//	  operation: "read",
//	  entity: "users",
//	  options: {
//	    filters: [{column: "status", operator: "eq", value: "active"}],
//	    limit: 10
//	  }
//	}))
//
//	// Subscribe to changes
//	ws.send(JSON.stringify({
//	  id: "msg-2",
//	  type: "subscription",
//	  operation: "subscribe",
//	  entity: "users",
//	  options: {
//	    filters: [{column: "status", operator: "eq", value: "active"}]
//	  }
//	}))
package websocketspec

import (
	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/uptrace/bun"
	"gorm.io/gorm"
)

// NewHandlerWithGORM creates a new Handler with GORM adapter
func NewHandlerWithGORM(db *gorm.DB) *Handler {
	gormAdapter := database.NewGormAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandler(gormAdapter, registry)
}

// NewHandlerWithBun creates a new Handler with Bun adapter
func NewHandlerWithBun(db *bun.DB) *Handler {
	bunAdapter := database.NewBunAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandler(bunAdapter, registry)
}

// NewHandlerWithDatabase creates a new Handler with a custom database adapter
func NewHandlerWithDatabase(db common.Database, registry common.ModelRegistry) *Handler {
	return NewHandler(db, registry)
}

// Example usage functions for documentation:

// ExampleWithGORM shows how to use WebSocketSpec with GORM
func ExampleWithGORM(db *gorm.DB) {
	// Create handler using GORM
	handler := NewHandlerWithGORM(db)

	// Register models
	handler.Registry().RegisterModel("public.users", &struct{}{})

	// Register hooks (optional)
	handler.Hooks().RegisterBefore(OperationRead, func(ctx *HookContext) error {
		// Add custom logic before read operations
		return nil
	})

	// Setup WebSocket endpoint
	// http.HandleFunc("/ws", handler.HandleWebSocket)

	// Start server
	// http.ListenAndServe(":8080", nil)
}

// ExampleWithBun shows how to use WebSocketSpec with Bun ORM
func ExampleWithBun(bunDB *bun.DB) {
	// Create handler using Bun
	handler := NewHandlerWithBun(bunDB)

	// Register models
	handler.Registry().RegisterModel("public.users", &struct{}{})

	// Setup WebSocket endpoint
	// http.HandleFunc("/ws", handler.HandleWebSocket)
}

// ExampleWithHooks shows how to use lifecycle hooks
func ExampleWithHooks(db *gorm.DB) {
	handler := NewHandlerWithGORM(db)

	// Register a before-read hook for authorization
	handler.Hooks().RegisterBefore(OperationRead, func(ctx *HookContext) error {
		// Check if user has permission to read this entity
		// return fmt.Errorf("unauthorized") if not allowed
		return nil
	})

	// Register an after-create hook for logging
	handler.Hooks().RegisterAfter(OperationCreate, func(ctx *HookContext) error {
		// Log the created record
		// logger.Info("Created record: %v", ctx.Result)
		return nil
	})

	// Register a before-subscribe hook to limit subscriptions
	handler.Hooks().Register(BeforeSubscribe, func(ctx *HookContext) error {
		// Limit number of subscriptions per connection
		// if len(ctx.Connection.subscriptions) >= 10 {
		//     return fmt.Errorf("maximum subscriptions reached")
		// }
		return nil
	})
}

// ExampleWithSubscriptions shows subscription usage
func ExampleWithSubscriptions() {
	// Client-side JavaScript example:
	/*
		const ws = new WebSocket("ws://localhost:8080/ws");

		// Subscribe to user changes
		ws.send(JSON.stringify({
			id: "sub-1",
			type: "subscription",
			operation: "subscribe",
			schema: "public",
			entity: "users",
			options: {
				filters: [
					{column: "status", operator: "eq", value: "active"}
				]
			}
		}));

		// Handle notifications
		ws.onmessage = (event) => {
			const msg = JSON.parse(event.data);
			if (msg.type === "notification") {
				console.log("User changed:", msg.data);
				console.log("Operation:", msg.operation); // create, update, or delete
			}
		};

		// Unsubscribe
		ws.send(JSON.stringify({
			id: "unsub-1",
			type: "subscription",
			operation: "unsubscribe",
			subscription_id: "sub-abc123"
		}));
	*/
}

// ExampleCRUDOperations shows basic CRUD operations
func ExampleCRUDOperations() {
	// Client-side JavaScript example:
	/*
		const ws = new WebSocket("ws://localhost:8080/ws");

		// CREATE - Create a new user
		ws.send(JSON.stringify({
			id: "create-1",
			type: "request",
			operation: "create",
			schema: "public",
			entity: "users",
			data: {
				name: "John Doe",
				email: "john@example.com",
				status: "active"
			}
		}));

		// READ - Get all active users
		ws.send(JSON.stringify({
			id: "read-1",
			type: "request",
			operation: "read",
			schema: "public",
			entity: "users",
			options: {
				filters: [{column: "status", operator: "eq", value: "active"}],
				columns: ["id", "name", "email"],
				sort: [{column: "name", direction: "asc"}],
				limit: 10
			}
		}));

		// READ BY ID - Get a specific user
		ws.send(JSON.stringify({
			id: "read-2",
			type: "request",
			operation: "read",
			schema: "public",
			entity: "users",
			record_id: "123"
		}));

		// UPDATE - Update a user
		ws.send(JSON.stringify({
			id: "update-1",
			type: "request",
			operation: "update",
			schema: "public",
			entity: "users",
			record_id: "123",
			data: {
				name: "John Updated",
				email: "john.updated@example.com"
			}
		}));

		// DELETE - Delete a user
		ws.send(JSON.stringify({
			id: "delete-1",
			type: "request",
			operation: "delete",
			schema: "public",
			entity: "users",
			record_id: "123"
		}));

		// Handle responses
		ws.onmessage = (event) => {
			const response = JSON.parse(event.data);
			if (response.type === "response") {
				if (response.success) {
					console.log("Operation successful:", response.data);
				} else {
					console.error("Operation failed:", response.error);
				}
			}
		};
	*/
}

// ExampleAuthentication shows how to implement authentication
func ExampleAuthentication() {
	// Server-side example with authentication hook:
	/*
		handler := NewHandlerWithGORM(db)

		// Register before-connect hook for authentication
		handler.Hooks().Register(BeforeConnect, func(ctx *HookContext) error {
			// Extract token from query params or headers
			r := ctx.Connection.ws.UnderlyingConn().RemoteAddr()

			// Validate token
			// token := extractToken(r)
			// user, err := validateToken(token)
			// if err != nil {
			//     return fmt.Errorf("authentication failed: %w", err)
			// }

			// Store user info in connection metadata
			// ctx.Connection.SetMetadata("user", user)
			// ctx.Connection.SetMetadata("user_id", user.ID)

			return nil
		})

		// Use connection metadata in other hooks
		handler.Hooks().RegisterBefore(OperationRead, func(ctx *HookContext) error {
			// Get user from connection metadata
			// userID, _ := ctx.Connection.GetMetadata("user_id")

			// Add filter to only show user's own records
			// if ctx.Entity == "orders" {
			//     ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
			//         Column:   "user_id",
			//         Operator: "eq",
			//         Value:    userID,
			//     })
			// }

			return nil
		})
	*/
}
