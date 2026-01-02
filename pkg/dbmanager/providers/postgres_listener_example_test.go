package providers_test

import (
	"context"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/dbmanager"
	"github.com/bitechdev/ResolveSpec/pkg/dbmanager/providers"
)

// ExamplePostgresListener_basic demonstrates basic LISTEN/NOTIFY usage
func ExamplePostgresListener_basic() {
	// Create a connection config
	cfg := &dbmanager.ConnectionConfig{
		Name:           "example",
		Type:           dbmanager.DatabaseTypePostgreSQL,
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		Password:       "password",
		Database:       "testdb",
		ConnectTimeout: 10 * time.Second,
		EnableLogging:  true,
	}

	// Create and connect PostgreSQL provider
	provider := providers.NewPostgresProvider()
	ctx := context.Background()

	if err := provider.Connect(ctx, cfg); err != nil {
		panic(fmt.Sprintf("Failed to connect: %v", err))
	}
	defer provider.Close()

	// Get listener
	listener, err := provider.GetListener(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get listener: %v", err))
	}

	// Subscribe to a channel with a handler
	err = listener.Listen("user_events", func(channel, payload string) {
		fmt.Printf("Received notification on %s: %s\n", channel, payload)
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}

	// Send a notification
	err = listener.Notify(ctx, "user_events", `{"event":"user_created","user_id":123}`)
	if err != nil {
		panic(fmt.Sprintf("Failed to notify: %v", err))
	}

	// Wait for notification to be processed
	time.Sleep(100 * time.Millisecond)

	// Unsubscribe from the channel
	if err := listener.Unlisten("user_events"); err != nil {
		panic(fmt.Sprintf("Failed to unlisten: %v", err))
	}
}

// ExamplePostgresListener_multipleChannels demonstrates listening to multiple channels
func ExamplePostgresListener_multipleChannels() {
	cfg := &dbmanager.ConnectionConfig{
		Name:           "example",
		Type:           dbmanager.DatabaseTypePostgreSQL,
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		Password:       "password",
		Database:       "testdb",
		ConnectTimeout: 10 * time.Second,
		EnableLogging:  false,
	}

	provider := providers.NewPostgresProvider()
	ctx := context.Background()

	if err := provider.Connect(ctx, cfg); err != nil {
		panic(fmt.Sprintf("Failed to connect: %v", err))
	}
	defer provider.Close()

	listener, err := provider.GetListener(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get listener: %v", err))
	}

	// Listen to multiple channels
	channels := []string{"orders", "payments", "notifications"}
	for _, ch := range channels {
		channel := ch // Capture for closure
		err := listener.Listen(channel, func(ch, payload string) {
			fmt.Printf("[%s] %s\n", ch, payload)
		})
		if err != nil {
			panic(fmt.Sprintf("Failed to listen on %s: %v", channel, err))
		}
	}

	// Send notifications to different channels
	listener.Notify(ctx, "orders", "New order #12345")
	listener.Notify(ctx, "payments", "Payment received $99.99")
	listener.Notify(ctx, "notifications", "Welcome email sent")

	// Wait for notifications
	time.Sleep(200 * time.Millisecond)

	// Check active channels
	activeChannels := listener.Channels()
	fmt.Printf("Listening to %d channels: %v\n", len(activeChannels), activeChannels)
}

// ExamplePostgresListener_withDBManager demonstrates usage with DBManager
func ExamplePostgresListener_withDBManager() {
	// This example shows how to use the listener with the full DBManager

	// Assume we have a DBManager instance and get a connection
	// conn, _ := dbMgr.Get("primary")

	// Get the underlying provider (this would need to be exposed via the Connection interface)
	// For now, this is a conceptual example

	ctx := context.Background()

	// Create provider directly for demonstration
	cfg := &dbmanager.ConnectionConfig{
		Name:           "primary",
		Type:           dbmanager.DatabaseTypePostgreSQL,
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		Password:       "password",
		Database:       "myapp",
		ConnectTimeout: 10 * time.Second,
	}

	provider := providers.NewPostgresProvider()
	if err := provider.Connect(ctx, cfg); err != nil {
		panic(err)
	}
	defer provider.Close()

	// Get listener
	listener, err := provider.GetListener(ctx)
	if err != nil {
		panic(err)
	}

	// Subscribe to application events
	listener.Listen("cache_invalidation", func(channel, payload string) {
		fmt.Printf("Cache invalidation request: %s\n", payload)
		// Handle cache invalidation logic here
	})

	listener.Listen("config_reload", func(channel, payload string) {
		fmt.Printf("Configuration reload request: %s\n", payload)
		// Handle configuration reload logic here
	})

	// Simulate receiving notifications
	listener.Notify(ctx, "cache_invalidation", "user:123")
	listener.Notify(ctx, "config_reload", "database")

	time.Sleep(100 * time.Millisecond)
}

// ExamplePostgresListener_errorHandling demonstrates error handling and reconnection
func ExamplePostgresListener_errorHandling() {
	cfg := &dbmanager.ConnectionConfig{
		Name:           "example",
		Type:           dbmanager.DatabaseTypePostgreSQL,
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		Password:       "password",
		Database:       "testdb",
		ConnectTimeout: 10 * time.Second,
		EnableLogging:  true,
	}

	provider := providers.NewPostgresProvider()
	ctx := context.Background()

	if err := provider.Connect(ctx, cfg); err != nil {
		panic(fmt.Sprintf("Failed to connect: %v", err))
	}
	defer provider.Close()

	listener, err := provider.GetListener(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get listener: %v", err))
	}

	// The listener automatically reconnects if the connection is lost
	// Subscribe with error handling in the callback
	err = listener.Listen("critical_events", func(channel, payload string) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Handler panic recovered: %v\n", r)
			}
		}()

		// Process the event
		fmt.Printf("Processing critical event: %s\n", payload)

		// If processing fails, the panic will be caught by the defer above
		// The listener will continue to function normally
	})

	if err != nil {
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}

	// Check if listener is connected
	if listener.IsConnected() {
		fmt.Println("Listener is connected and ready")
	}

	// Send a notification
	listener.Notify(ctx, "critical_events", "system_alert")

	time.Sleep(100 * time.Millisecond)
}
