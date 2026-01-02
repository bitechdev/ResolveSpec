package server

import (
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// FromConfigInstanceToServerConfig converts a config.ServerInstanceConfig to server.Config
// The handler must be provided separately as it cannot be serialized
func FromConfigInstanceToServerConfig(sic *config.ServerInstanceConfig, handler http.Handler) Config {
	cfg := Config{
		Name:        sic.Name,
		Host:        sic.Host,
		Port:        sic.Port,
		Description: sic.Description,
		Handler:     handler,
		GZIP:        sic.GZIP,

		SSLCert:         sic.SSLCert,
		SSLKey:          sic.SSLKey,
		SelfSignedSSL:   sic.SelfSignedSSL,
		AutoTLS:         sic.AutoTLS,
		AutoTLSDomains:  sic.AutoTLSDomains,
		AutoTLSCacheDir: sic.AutoTLSCacheDir,
		AutoTLSEmail:    sic.AutoTLSEmail,
	}

	// Apply timeouts (use pointers to override, or use zero values for defaults)
	if sic.ShutdownTimeout != nil {
		cfg.ShutdownTimeout = *sic.ShutdownTimeout
	}
	if sic.DrainTimeout != nil {
		cfg.DrainTimeout = *sic.DrainTimeout
	}
	if sic.ReadTimeout != nil {
		cfg.ReadTimeout = *sic.ReadTimeout
	}
	if sic.WriteTimeout != nil {
		cfg.WriteTimeout = *sic.WriteTimeout
	}
	if sic.IdleTimeout != nil {
		cfg.IdleTimeout = *sic.IdleTimeout
	}

	return cfg
}
