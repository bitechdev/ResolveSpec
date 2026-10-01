// Package backends assembles a lookup.Provider: it builds the procedure and direct stores for
// one database and routes every operation to one of them according to lookup.Config.
// It lives apart from package lookup because both backends import lookup.
package backends

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/dbtrace"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/direct"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/procedure"
)

// Options are the settings that are not naming or mode.
type Options struct {
	// DBFactory is called to obtain a fresh *sql.DB when the current one has been closed.
	// Nil disables reconnecting.
	DBFactory func() (*sql.DB, error)
	// UpgradePasswordHash rewrites a legacy cleartext password as bcrypt after a successful
	// direct-mode login. Off by default.
	UpgradePasswordHash bool
	// NoGroupTables skips the group membership table when loading direct-mode policy rules.
	NoGroupTables bool
}

// New builds a Provider for db. cfg is merged with the defaults and validated; the dialect
// is cfg.Dialect or detected from the driver. Every operation's mode is resolved up front so
// an impossible combination (procedure mode on a non-Postgres dialect) fails here, not on the
// first request.
func New(db *sql.DB, cfg lookup.Config, opts Options) (*lookup.Provider, error) {
	if db == nil {
		return nil, fmt.Errorf("backends: nil database")
	}
	res, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}
	d, err := cfg.ResolveDialect(db)
	if err != nil {
		return nil, err
	}
	for _, op := range lookup.AllOps() {
		if _, err := res.EffectiveMode(op, d.Name()); err != nil {
			return nil, err
		}
	}

	c := &chooser{cfg: res, dialect: d.Name(), procs: res.Procs}
	run := procedure.NewDB(db, opts.DBFactory, c.resetProbes)
	c.db = run

	base, err := direct.NewBase(run, d, res.Schema)
	if err != nil {
		return nil, err
	}
	p := procedure.NewPasskey(run, res.Procs)
	return &lookup.Provider{
		Auth: &authRouter{c: c,
			proc:   procedure.NewAuth(run, res.Procs),
			direct: direct.NewAuth(base, direct.AuthOptions{UpgradePasswordHash: opts.UpgradePasswordHash})},
		Keys: &keysRouter{c: c,
			proc:   procedure.NewKeys(run, res.Procs),
			direct: direct.NewKeys(base)},
		OAuthClient: &oauthClientRouter{c: c,
			proc:   procedure.NewOAuthClients(run, res.Procs),
			direct: direct.NewOAuthClients(base)},
		OAuthUser: &oauthUserRouter{c: c,
			proc:   procedure.NewOAuthUsers(run, res.Procs),
			direct: direct.NewOAuthUsers(base)},
		OAuthGrant: &oauthGrantRouter{c: c,
			proc:   procedure.NewOAuthGrants(run, res.Procs),
			direct: direct.NewOAuthGrants(base)},
		Passkey: &passkeyRouter{c: c, proc: p, direct: direct.NewPasskey(base)},
		TOTP: &totpRouter{c: c,
			proc:   procedure.NewTOTP(run, res.Procs),
			direct: direct.NewTOTP(base)},
		Policy: &policyRouter{c: c,
			proc:   procedure.NewPolicy(run, res.Procs),
			direct: direct.NewPolicy(base, direct.PolicyOptions{NoGroups: opts.NoGroupTables})},
	}, nil
}

// Failed returns a Provider whose every operation returns err. Constructors that cannot
// return an error use it so a bad configuration fails closed on first use.
func Failed(err error) *lookup.Provider {
	c := &chooser{fail: err}
	return &lookup.Provider{
		Auth:        &authRouter{c: c},
		Keys:        &keysRouter{c: c},
		OAuthClient: &oauthClientRouter{c: c},
		OAuthUser:   &oauthUserRouter{c: c},
		OAuthGrant:  &oauthGrantRouter{c: c},
		Passkey:     &passkeyRouter{c: c},
		TOTP:        &totpRouter{c: c},
		Policy:      &policyRouter{c: c},
	}
}

// chooser decides per operation whether the procedure or the direct store runs.
type chooser struct {
	cfg     *lookup.Resolved
	dialect string
	procs   lookup.ProcNames
	db      *procedure.DB
	probes  sync.Map // proc name -> bool
	fail    error    // set by Failed: every operation returns it
}

func (c *chooser) resetProbes() {
	c.probes.Range(func(k, _ any) bool { c.probes.Delete(k); return true })
}

// useProc reports whether op should call the stored procedure proc. In auto mode on Postgres
// the catalog is probed once per procedure (cached until a reconnect).
func (c *chooser) useProc(ctx context.Context, op lookup.Op, proc string) (bool, error) {
	if c.fail != nil {
		return false, c.fail
	}
	m, err := c.cfg.EffectiveMode(op, c.dialect)
	if err != nil {
		return false, err
	}
	switch m {
	case lookup.ModeProcedure:
		return true, nil
	case lookup.ModeAuto:
		if v, ok := c.probes.Load(proc); ok {
			return v.(bool), nil
		}
		exists := probeProcedure(ctx, c.db.Get(), proc)
		c.probes.Store(proc, exists)
		return exists, nil
	}
	return false, nil
}

// probeProcedure asks the Postgres catalog whether a function exists. Any failure counts as
// "does not exist" so the probe can never block an operation.
func probeProcedure(ctx context.Context, db *sql.DB, proc string) (exists bool) {
	if db == nil {
		return false
	}
	defer func() { _ = recover() }()
	dbtrace.Raw(ctx, "probe.pg_proc")
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = $1 LIMIT 1)`, proc).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// pick returns the store that should serve op.
func pick[T any](c *chooser, ctx context.Context, op lookup.Op, proc string, p, d T) (T, error) {
	use, err := c.useProc(ctx, op, proc)
	if err != nil {
		var zero T
		return zero, err
	}
	if use {
		return p, nil
	}
	return d, nil
}
