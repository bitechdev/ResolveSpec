package common

import "github.com/bitechdev/ResolveSpec/pkg/config"

// hardeningProvider returns the active hardening switches. Tests may replace it.
var hardeningProvider = func() config.HardeningConfig {
	cfg, err := config.GetConfigManager().GetConfig()
	if err != nil || cfg == nil {
		// Fail secure: hardening on when config is unavailable.
		return config.HardeningConfig{CORSStrictOrigins: true, SortStrict: true, SQLStrict: true}
	}
	return cfg.Hardening
}

// Hardening returns the security hardening toggles (config section "hardening").
func Hardening() config.HardeningConfig { return hardeningProvider() }
