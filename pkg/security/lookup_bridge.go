package security

import (
	"database/sql"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/backends"
)

// lookupSource builds the lookup.Provider a security component uses, on first use, so the
// With* builders can still change the configuration after construction. An explicit Provider
// bypasses the build.
type lookupSource struct {
	db   *sql.DB
	cfg  lookup.Config
	opts backends.Options

	provider *lookup.Provider

	once  sync.Once
	built *lookup.Provider
}

func newLookupSource(db *sql.DB) *lookupSource { return &lookupSource{db: db} }

// get returns the provider. A bad configuration is logged once and yields a provider that
// returns the error from every call, so the component fails closed.
func (s *lookupSource) get() *lookup.Provider {
	if s.provider != nil {
		return s.provider
	}
	s.once.Do(func() { s.built = resolveLookup(s.db, s.cfg, s.opts) })
	return s.built
}

// resolveLookup builds a provider for db. When the dialect is not configured and cannot be
// detected from the driver it falls back to postgres, the stored-procedure default.
func resolveLookup(db *sql.DB, cfg lookup.Config, opts backends.Options) *lookup.Provider {
	if db != nil && cfg.Dialect == "" {
		if _, err := cfg.ResolveDialect(db); err != nil {
			cfg.Dialect = "postgres"
		}
	}
	p, err := backends.New(db, cfg, opts)
	if err != nil {
		logger.Error("security: lookup configuration invalid: %v", err)
		return backends.Failed(err)
	}
	return p
}
