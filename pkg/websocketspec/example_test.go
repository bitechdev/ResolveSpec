package websocketspec_test

import (
	"fmt"
	"log"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/websocketspec"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// User model example
type User struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

// Post model example
type Post struct {
	ID      uint   `json:"id" gorm:"primaryKey"`
	Title   string `json:"title"`
	Content string `json:"content"`
	UserID  uint   `json:"user_id"`
	User    *User  `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

// Example_basicSetup demonstrates basic WebSocketSpec setup
func Example_basicSetup() {
	// Connect to database
	db, err := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	// Create WebSocket handler
	handler := websocketspec.NewHandlerWithGORM(db)

	// Register models
	handler.Registry().RegisterModel("public.users", &User{})
	handler.Registry().RegisterModel("public.posts", &Post{})

	// Setup WebSocket endpoint
	http.HandleFunc("/ws", handler.HandleWebSocket)

	// Start server
	log.Println("WebSocket server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

// Example_withHooks demonstrates using lifecycle hooks
func Example_withHooks() {
	db, _ := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})
	handler := websocketspec.NewHandlerWithGORM(db)

	// Register models
	handler.Registry().RegisterModel("public.users", &User{})

	// Add authentication hook
	handler.Hooks().Register(websocketspec.BeforeConnect, func(ctx *websocketspec.HookContext) error {
		// Validate authentication token
		// (In real implementation, extract from query params or headers)
		userID := uint(123) // From token

		// Store in connection metadata
		ctx.Connection.SetMetadata("user_id", userID)
		log.Printf("User %d connected", userID)

		return nil
	})

	// Add authorization hook for read operations
	handler.Hooks().RegisterBefore(websocketspec.OperationRead, func(ctx *websocketspec.HookContext) error {
		userID, ok := ctx.Connection.GetMetadata("user_id")
		if !ok {
			return fmt.Errorf("unauthorized: not authenticated")
		}

		log.Printf("User %v reading %s.%s", userID, ctx.Schema, ctx.Entity)

		// Add filter to only show user's own records
		if ctx.Entity == "posts" {
			// ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
			// 	Column:   "user_id",
			// 	Operator: "eq",
			// 	Value:    userID,
			// })
		}

		return nil
	})

	// Add logging hook after create
	handler.Hooks().RegisterAfter(websocketspec.OperationCreate, func(ctx *websocketspec.HookContext) error {
		userID, _ := ctx.Connection.GetMetadata("user_id")
		log.Printf("User %v created record in %s.%s", userID, ctx.Schema, ctx.Entity)
		return nil
	})

	// Add validation hook before create
	handler.Hooks().RegisterBefore(websocketspec.OperationCreate, func(ctx *websocketspec.HookContext) error {
		// Validate required fields
		if data, ok := ctx.Data.(map[string]interface{}); ok {
			if ctx.Entity == "users" {
				if email, exists := data["email"]; !exists || email == "" {
					return fmt.Errorf("validation error: email is required")
				}
				if name, exists := data["name"]; !exists || name == "" {
					return fmt.Errorf("validation error: name is required")
				}
			}
		}
		return nil
	})

	// Add limit hook for subscriptions
	handler.Hooks().Register(websocketspec.BeforeSubscribe, func(ctx *websocketspec.HookContext) error {
		// Limit subscriptions per connection
		maxSubscriptions := 10
		// Note: In a real implementation, you would count subscriptions using the connection's methods
		// currentCount := len(ctx.Connection.subscriptions) // subscriptions is private

		// For demonstration purposes, we'll just log
		log.Printf("Creating subscription (max: %d)", maxSubscriptions)
		return nil
	})

	http.HandleFunc("/ws", handler.HandleWebSocket)
	log.Println("Server with hooks starting on :8080")
	http.ListenAndServe(":8080", nil)
}

// Example_monitoring demonstrates monitoring connections and subscriptions
func Example_monitoring() {
	db, _ := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})
	handler := websocketspec.NewHandlerWithGORM(db)

	handler.Registry().RegisterModel("public.users", &User{})

	// Add connection tracking
	handler.Hooks().Register(websocketspec.AfterConnect, func(ctx *websocketspec.HookContext) error {
		count := handler.GetConnectionCount()
		log.Printf("Client connected. Total connections: %d", count)
		return nil
	})

	handler.Hooks().Register(websocketspec.AfterDisconnect, func(ctx *websocketspec.HookContext) error {
		count := handler.GetConnectionCount()
		log.Printf("Client disconnected. Total connections: %d", count)
		return nil
	})

	// Add subscription tracking
	handler.Hooks().Register(websocketspec.AfterSubscribe, func(ctx *websocketspec.HookContext) error {
		count := handler.GetSubscriptionCount()
		log.Printf("New subscription. Total subscriptions: %d", count)
		return nil
	})

	// Monitoring endpoint
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Active Connections: %d\n", handler.GetConnectionCount())
		fmt.Fprintf(w, "Active Subscriptions: %d\n", handler.GetSubscriptionCount())
	})

	http.HandleFunc("/ws", handler.HandleWebSocket)
	log.Println("Server with monitoring starting on :8080")
	http.ListenAndServe(":8080", nil)
}

// Example_clientSide shows client-side usage example
func Example_clientSide() {
	// This is JavaScript code for documentation purposes
	jsCode := `
// JavaScript WebSocket Client Example

const ws = new WebSocket("ws://localhost:8080/ws");

ws.onopen = () => {
    console.log("Connected to WebSocket");

    // Read users
    ws.send(JSON.stringify({
        id: "msg-1",
        type: "request",
        operation: "read",
        schema: "public",
        entity: "users",
        options: {
            filters: [{column: "status", operator: "eq", value: "active"}],
            limit: 10
        }
    }));

    // Subscribe to user changes
    ws.send(JSON.stringify({
        id: "sub-1",
        type: "subscription",
        operation: "subscribe",
        schema: "public",
        entity: "users",
        options: {
            filters: [{column: "status", operator: "eq", value: "active"}]
        }
    }));
};

ws.onmessage = (event) => {
    const message = JSON.parse(event.data);

    if (message.type === "response") {
        if (message.success) {
            console.log("Response:", message.data);
        } else {
            console.error("Error:", message.error);
        }
    } else if (message.type === "notification") {
        console.log("Notification:", message.operation, message.data);
    }
};

ws.onerror = (error) => {
    console.error("WebSocket error:", error);
};

ws.onclose = () => {
    console.log("WebSocket connection closed");
    // Implement reconnection logic here
};
`

	fmt.Println(jsCode)
}
