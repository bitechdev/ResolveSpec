package errortracking

import (
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// NewProviderFromConfig creates an error tracking provider based on the configuration
func NewProviderFromConfig(cfg config.ErrorTrackingConfig) (Provider, error) {
	if !cfg.Enabled {
		return NewNoOpProvider(), nil
	}

	switch cfg.Provider {
	case "sentry":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("sentry DSN is required when error tracking is enabled")
		}
		return NewSentryProvider(SentryConfig{
			DSN:              cfg.DSN,
			Environment:      cfg.Environment,
			Release:          cfg.Release,
			Debug:            cfg.Debug,
			SampleRate:       cfg.SampleRate,
			TracesSampleRate: cfg.TracesSampleRate,
		})
	case "noop", "":
		return NewNoOpProvider(), nil
	default:
		return nil, fmt.Errorf("unknown error tracking provider: %s", cfg.Provider)
	}
}
