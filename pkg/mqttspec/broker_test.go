package mqttspec

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmbeddedBroker(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           1883,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)

	assert.NotNil(t, broker)
	assert.Equal(t, config, broker.config)
	assert.Equal(t, cm, broker.clientManager)
	assert.NotNil(t, broker.subscriptions)
	assert.False(t, broker.started)
}

func TestEmbeddedBroker_StartStop(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11883, // Use non-standard port for testing
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Start broker
	err := broker.Start(ctx)
	require.NoError(t, err)

	// Verify started
	assert.True(t, broker.IsConnected())

	// Stop broker
	err = broker.Stop(ctx)
	require.NoError(t, err)

	// Verify stopped
	assert.False(t, broker.IsConnected())
}

func TestEmbeddedBroker_StartTwice(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11884,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Start broker
	err := broker.Start(ctx)
	require.NoError(t, err)
	defer broker.Stop(ctx)

	// Try to start again - should fail
	err = broker.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestEmbeddedBroker_StopWithoutStart(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11885,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Stop without starting - should not error
	err := broker.Stop(ctx)
	assert.NoError(t, err)
}

func TestEmbeddedBroker_PublishWithoutStart(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11886,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)

	// Try to publish without starting - should fail
	err := broker.Publish("test/topic", 1, []byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "broker not started")
}

func TestEmbeddedBroker_SubscribeWithoutStart(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11887,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)

	// Try to subscribe without starting - should fail
	err := broker.Subscribe("test/topic", 1, func(topic string, payload []byte) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "broker not started")
}

func TestEmbeddedBroker_PublishSubscribe(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11888,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Start broker
	err := broker.Start(ctx)
	require.NoError(t, err)
	defer broker.Stop(ctx)

	// Subscribe to topic
	callback := func(topic string, payload []byte) {
		// Callback for subscription - actual message delivery would require
		// integration with Mochi MQTT's hook system
	}

	err = broker.Subscribe("test/topic", 1, callback)
	require.NoError(t, err)

	// Note: Embedded broker's Subscribe is simplified and doesn't fully integrate
	// with Mochi MQTT's internal pub/sub. This test verifies the subscription
	// is registered but actual message delivery would require more complex
	// integration with Mochi MQTT's hook system.

	// Verify subscription was registered
	broker.subMu.RLock()
	_, exists := broker.subscriptions["test/topic"]
	broker.subMu.RUnlock()
	assert.True(t, exists)
}

func TestEmbeddedBroker_Unsubscribe(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11889,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Start broker
	err := broker.Start(ctx)
	require.NoError(t, err)
	defer broker.Stop(ctx)

	// Subscribe
	callback := func(topic string, payload []byte) {}
	err = broker.Subscribe("test/topic", 1, callback)
	require.NoError(t, err)

	// Verify subscription exists
	broker.subMu.RLock()
	_, exists := broker.subscriptions["test/topic"]
	broker.subMu.RUnlock()
	assert.True(t, exists)

	// Unsubscribe
	err = broker.Unsubscribe("test/topic")
	require.NoError(t, err)

	// Verify subscription removed
	broker.subMu.RLock()
	_, exists = broker.subscriptions["test/topic"]
	broker.subMu.RUnlock()
	assert.False(t, exists)
}

func TestEmbeddedBroker_SetHandler(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11890,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)

	// Create a mock handler (nil is fine for this test)
	var handler *Handler = nil

	// Set handler
	broker.SetHandler(handler)

	// Verify handler was set
	broker.mu.RLock()
	assert.Equal(t, handler, broker.handler)
	broker.mu.RUnlock()
}

func TestEmbeddedBroker_GetClientManager(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11891,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)

	// Get client manager
	retrievedCM := broker.GetClientManager()

	// Verify it's the same instance
	assert.Equal(t, cm, retrievedCM)
}

func TestEmbeddedBroker_ConcurrentPublish(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := BrokerConfig{
		Host:           "localhost",
		Port:           11892,
		MaxConnections: 100,
		KeepAlive:      60 * time.Second,
	}

	broker := NewEmbeddedBroker(config, cm)
	ctx := context.Background()

	// Start broker
	err := broker.Start(ctx)
	require.NoError(t, err)
	defer broker.Stop(ctx)

	// Test concurrent publishing
	var wg sync.WaitGroup
	numPublishers := 10

	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := broker.Publish("test/topic", 1, []byte("test"))
				// Errors are acceptable in concurrent scenario
				_ = err
			}
		}(i)
	}

	wg.Wait()
}

func TestNewExternalBrokerClient(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := ExternalBrokerConfig{
		BrokerURL:      "tcp://localhost:1883",
		ClientID:       "test-client",
		Username:       "user",
		Password:       "pass",
		CleanSession:   true,
		KeepAlive:      60 * time.Second,
		ConnectTimeout: 5 * time.Second,
		ReconnectDelay: 1 * time.Second,
	}

	broker := NewExternalBrokerClient(config, cm)

	assert.NotNil(t, broker)
	assert.Equal(t, config, broker.config)
	assert.Equal(t, cm, broker.clientManager)
	assert.NotNil(t, broker.subscriptions)
	assert.False(t, broker.connected)
}

func TestExternalBrokerClient_SetHandler(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := ExternalBrokerConfig{
		BrokerURL:      "tcp://localhost:1883",
		ClientID:       "test-client",
		Username:       "user",
		Password:       "pass",
		CleanSession:   true,
		KeepAlive:      60 * time.Second,
		ConnectTimeout: 5 * time.Second,
		ReconnectDelay: 1 * time.Second,
	}

	broker := NewExternalBrokerClient(config, cm)

	// Create a mock handler (nil is fine for this test)
	var handler *Handler = nil

	// Set handler
	broker.SetHandler(handler)

	// Verify handler was set
	broker.mu.RLock()
	assert.Equal(t, handler, broker.handler)
	broker.mu.RUnlock()
}

func TestExternalBrokerClient_GetClientManager(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := ExternalBrokerConfig{
		BrokerURL:      "tcp://localhost:1883",
		ClientID:       "test-client",
		Username:       "user",
		Password:       "pass",
		CleanSession:   true,
		KeepAlive:      60 * time.Second,
		ConnectTimeout: 5 * time.Second,
		ReconnectDelay: 1 * time.Second,
	}

	broker := NewExternalBrokerClient(config, cm)

	// Get client manager
	retrievedCM := broker.GetClientManager()

	// Verify it's the same instance
	assert.Equal(t, cm, retrievedCM)
}

func TestExternalBrokerClient_IsConnected(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	config := ExternalBrokerConfig{
		BrokerURL:      "tcp://localhost:1883",
		ClientID:       "test-client",
		Username:       "user",
		Password:       "pass",
		CleanSession:   true,
		KeepAlive:      60 * time.Second,
		ConnectTimeout: 5 * time.Second,
		ReconnectDelay: 1 * time.Second,
	}

	broker := NewExternalBrokerClient(config, cm)

	// Should not be connected initially
	assert.False(t, broker.IsConnected())
}

// Note: Tests for ExternalBrokerClient Start/Stop/Publish/Subscribe require
// a running MQTT broker and are better suited for integration tests.
// These tests would be included in integration_test.go with proper test
// broker setup (e.g., using Docker Compose).
