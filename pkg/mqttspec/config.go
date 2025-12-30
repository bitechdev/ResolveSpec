package mqttspec

import (
	"crypto/tls"
	"time"
)

// BrokerMode specifies how to connect to MQTT
type BrokerMode string

const (
	// BrokerModeEmbedded runs Mochi MQTT broker in-process
	BrokerModeEmbedded BrokerMode = "embedded"
	// BrokerModeExternal connects to external MQTT broker as client
	BrokerModeExternal BrokerMode = "external"
)

// Config holds all mqttspec configuration
type Config struct {
	// BrokerMode determines whether to use embedded or external broker
	BrokerMode BrokerMode

	// Broker configuration for embedded mode
	Broker BrokerConfig

	// ExternalBroker configuration for external client mode
	ExternalBroker ExternalBrokerConfig

	// Topics configuration
	Topics TopicConfig

	// QoS configuration for different message types
	QoS QoSConfig

	// Auth configuration
	Auth AuthConfig

	// Timeouts for various operations
	Timeouts TimeoutConfig
}

// BrokerConfig configures the embedded Mochi MQTT broker
type BrokerConfig struct {
	// Host to bind to (default: "localhost")
	Host string

	// Port to listen on (default: 1883)
	Port int

	// EnableWebSocket enables WebSocket support
	EnableWebSocket bool

	// WSPort is the WebSocket port (default: 8883)
	WSPort int

	// MaxConnections limits concurrent client connections
	MaxConnections int

	// KeepAlive is the client keepalive interval
	KeepAlive time.Duration

	// EnableAuth enables username/password authentication
	EnableAuth bool
}

// ExternalBrokerConfig for connecting as a client to external broker
type ExternalBrokerConfig struct {
	// BrokerURL is the broker address (e.g., tcp://host:port or ssl://host:port)
	BrokerURL string

	// ClientID is a unique identifier for this handler instance
	ClientID string

	// Username for MQTT authentication
	Username string

	// Password for MQTT authentication
	Password string

	// CleanSession flag (default: true)
	CleanSession bool

	// KeepAlive interval (default: 60s)
	KeepAlive time.Duration

	// ConnectTimeout for initial connection (default: 30s)
	ConnectTimeout time.Duration

	// ReconnectDelay between reconnection attempts (default: 5s)
	ReconnectDelay time.Duration

	// MaxReconnect attempts (0 = unlimited, default: 0)
	MaxReconnect int

	// TLSConfig for SSL/TLS connections
	TLSConfig *tls.Config
}

// TopicConfig defines the MQTT topic structure
type TopicConfig struct {
	// Prefix for all topics (default: "spec")
	// Topics will be: {Prefix}/{client_id}/request|response|notify/{sub_id}
	Prefix string
}

// QoSConfig defines quality of service levels for different message types
type QoSConfig struct {
	// Request messages QoS (default: 1 - at-least-once)
	Request byte

	// Response messages QoS (default: 1 - at-least-once)
	Response byte

	// Notification messages QoS (default: 1 - at-least-once)
	Notification byte
}

// AuthConfig for MQTT-level authentication
type AuthConfig struct {
	// ValidateCredentials is called to validate username/password for embedded broker
	// Return true if credentials are valid, false otherwise
	ValidateCredentials func(username, password string) bool
}

// TimeoutConfig defines timeouts for various operations
type TimeoutConfig struct {
	// Connect timeout for MQTT connection (default: 30s)
	Connect time.Duration

	// Publish timeout for publishing messages (default: 5s)
	Publish time.Duration

	// Disconnect timeout for graceful shutdown (default: 10s)
	Disconnect time.Duration
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		BrokerMode: BrokerModeEmbedded,
		Broker: BrokerConfig{
			Host:            "localhost",
			Port:            1883,
			EnableWebSocket: false,
			WSPort:          8883,
			MaxConnections:  1000,
			KeepAlive:       60 * time.Second,
			EnableAuth:      false,
		},
		ExternalBroker: ExternalBrokerConfig{
			BrokerURL:      "",
			ClientID:       "",
			Username:       "",
			Password:       "",
			CleanSession:   true,
			KeepAlive:      60 * time.Second,
			ConnectTimeout: 30 * time.Second,
			ReconnectDelay: 5 * time.Second,
			MaxReconnect:   0, // Unlimited
		},
		Topics: TopicConfig{
			Prefix: "spec",
		},
		QoS: QoSConfig{
			Request:      1, // At-least-once
			Response:     1, // At-least-once
			Notification: 1, // At-least-once
		},
		Auth: AuthConfig{
			ValidateCredentials: nil,
		},
		Timeouts: TimeoutConfig{
			Connect:    30 * time.Second,
			Publish:    5 * time.Second,
			Disconnect: 10 * time.Second,
		},
	}
}
