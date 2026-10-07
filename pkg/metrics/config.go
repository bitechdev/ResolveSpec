package metrics

import "net/http"

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

	// HTTPMaxPaths caps the number of distinct values of the "path" label on HTTP
	// metrics. Paths beyond the cap are reported as "other". Paths are already
	// normalized (route pattern, or dynamic segments replaced with ":id").
	// Default: 1024. Set to a negative value to disable the cap.
	HTTPMaxPaths int `mapstructure:"http_max_paths"`

	// HTTPPathNormalizer optionally maps a request to its "path" label (e.g. the
	// matched route template of your router). Return "" to fall back to the
	// default behaviour (ServeMux pattern, then generic ID normalization).
	HTTPPathNormalizer func(*http.Request) string `mapstructure:"-"`

	// PushgatewayURL is the URL of the Prometheus Pushgateway (optional)
	// If set, metrics will be pushed to this gateway instead of only being scraped
	// Example: "http://pushgateway:9091"
	PushgatewayURL string `mapstructure:"pushgateway_url"`

	// PushgatewayJobName is the job name to use when pushing metrics to Pushgateway
	// Default: "resolvespec"
	PushgatewayJobName string `mapstructure:"pushgateway_job_name"`

	// PushgatewayInterval is the interval at which to push metrics to Pushgateway
	// Only used if PushgatewayURL is set. If 0, automatic pushing is disabled.
	// Default: 0 (no automatic pushing)
	PushgatewayInterval int `mapstructure:"pushgateway_interval"`

	// PushEndpointURL is a custom HTTP endpoint that metrics are POSTed to
	// (independent of Pushgateway). Example: "https://collector.example.com/metrics"
	PushEndpointURL string `mapstructure:"push_endpoint_url"`

	// PushEndpointFormat is the request body format: "text" (Prometheus text
	// exposition, Content-Type text/plain; version=0.0.4) or "json".
	// Default: "text"
	PushEndpointFormat string `mapstructure:"push_endpoint_format"`

	// PushEndpointHeaders are extra headers sent with each POST (e.g. Authorization).
	PushEndpointHeaders map[string]string `mapstructure:"push_endpoint_headers"`

	// PushEndpointInterval is the interval in seconds for automatic POSTs.
	// If 0, automatic posting is disabled (PushToEndpoint can still be called manually).
	PushEndpointInterval int `mapstructure:"push_endpoint_interval"`

	// PushEndpointTimeout is the per-request timeout in seconds. Default: 10
	PushEndpointTimeout int `mapstructure:"push_endpoint_timeout"`

	// PushEndpointResetOnSuccess clears local counters and histograms after the
	// endpoint answers with a 2xx status. Default: false.
	PushEndpointResetOnSuccess bool `mapstructure:"push_endpoint_reset_on_success"`

	// PushgatewayResetOnPush clears the local counters and histograms after each
	// successful push (automatic or via PushAndReset), so each push carries only
	// the activity since the previous one. Default: false.
	PushgatewayResetOnPush bool `mapstructure:"pushgateway_reset_on_push"`
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
		HTTPMaxPaths:   defaultHTTPMaxPaths,
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
	if c.PushEndpointURL != "" {
		if c.PushEndpointFormat == "" {
			c.PushEndpointFormat = "text"
		}
		if c.PushEndpointTimeout <= 0 {
			c.PushEndpointTimeout = 10
		}
	}
	if c.HTTPMaxPaths == 0 {
		c.HTTPMaxPaths = defaultHTTPMaxPaths
	}
	// Set default job name if pushgateway is configured but job name is empty
	if c.PushgatewayURL != "" && c.PushgatewayJobName == "" {
		c.PushgatewayJobName = "resolvespec"
	}
}
