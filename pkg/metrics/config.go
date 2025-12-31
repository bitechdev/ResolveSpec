package metrics

// Config holds configuration for the metrics provider
type Config struct {
	// Enabled determines whether metrics collection is enabled
	Enabled bool `mapstructure:"enabled"`

	// Provider specifies which metrics provider to use (prometheus, noop)
	Provider string `mapstructure:"provider"`

	// Namespace is an optional prefix for all metric names
	Namespace string `mapstructure:"namespace"`

	// HTTPRequestBuckets defines histogram buckets for HTTP request duration (in seconds)
	// Default: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
	HTTPRequestBuckets []float64 `mapstructure:"http_request_buckets"`

	// DBQueryBuckets defines histogram buckets for database query duration (in seconds)
	// Default: [0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5]
	DBQueryBuckets []float64 `mapstructure:"db_query_buckets"`
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Enabled:  true,
		Provider: "prometheus",
		// HTTP requests typically take longer than DB queries
		HTTPRequestBuckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		// DB queries are usually faster
		DBQueryBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}
}

// ApplyDefaults fills in any missing values with defaults
func (c *Config) ApplyDefaults() {
	if c.Provider == "" {
		c.Provider = "prometheus"
	}
	if len(c.HTTPRequestBuckets) == 0 {
		c.HTTPRequestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	}
	if len(c.DBQueryBuckets) == 0 {
		c.DBQueryBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	}
}
