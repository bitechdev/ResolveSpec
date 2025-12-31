package metrics_test

import (
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/metrics"
)

// ExampleNewPrometheusProvider_default demonstrates using default configuration
func ExampleNewPrometheusProvider_default() {
	// Initialize with default configuration
	provider := metrics.NewPrometheusProvider(nil)
	metrics.SetProvider(provider)

	fmt.Println("Provider initialized with defaults")
	// Output: Provider initialized with defaults
}

// ExampleNewPrometheusProvider_custom demonstrates using custom configuration
func ExampleNewPrometheusProvider_custom() {
	// Create custom configuration
	config := &metrics.Config{
		Enabled:            true,
		Provider:           "prometheus",
		Namespace:          "myapp",
		HTTPRequestBuckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		DBQueryBuckets:     []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1},
	}

	// Initialize with custom configuration
	provider := metrics.NewPrometheusProvider(config)
	metrics.SetProvider(provider)

	fmt.Println("Provider initialized with custom config")
	// Output: Provider initialized with custom config
}

// ExampleDefaultConfig demonstrates getting default configuration
func ExampleDefaultConfig() {
	config := metrics.DefaultConfig()
	fmt.Printf("Default provider: %s\n", config.Provider)
	fmt.Printf("Default enabled: %v\n", config.Enabled)
	// Output:
	// Default provider: prometheus
	// Default enabled: true
}

// ExampleConfig_ApplyDefaults demonstrates applying defaults to partial config
func ExampleConfig_ApplyDefaults() {
	// Create partial configuration
	config := &metrics.Config{
		Namespace: "myapp",
		// Other fields will be filled with defaults
	}

	// Apply defaults
	config.ApplyDefaults()

	fmt.Printf("Provider: %s\n", config.Provider)
	fmt.Printf("Namespace: %s\n", config.Namespace)
	// Output:
	// Provider: prometheus
	// Namespace: myapp
}
