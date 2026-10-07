package aiproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// UpstreamStore supplies upstream definitions for Reload.
type UpstreamStore interface {
	List(ctx context.Context) ([]Definition, error)
}

// Reload syncs the store-managed upstreams with the store.
//
//   - New definitions are added, changed ones replaced, unchanged ones left alone
//     (their rate-limit state and connections survive), missing ones removed.
//   - Upstreams registered in code are never replaced or removed; a stored definition
//     with the same name is skipped and reported.
//   - Invalid definitions are skipped and reported in the returned error; valid ones
//     are still applied. If the store itself fails, nothing changes.
func (p *Proxy) Reload(ctx context.Context, store UpstreamStore) error {
	defs, err := store.List(ctx)
	if err != nil {
		return fmt.Errorf("aiproxy: load upstreams: %w", err)
	}

	var errs []error
	next := make(map[string]*target, len(defs))
	var retired []*target

	p.mu.Lock()
	for i := range defs {
		d := defs[i]
		name := d.Upstream.Name
		if _, dup := next[name]; dup {
			errs = append(errs, fmt.Errorf("aiproxy: duplicate stored upstream %q", name))
			continue
		}
		cur := p.upstream[name]
		if cur != nil && !cur.managed {
			errs = append(errs, fmt.Errorf("aiproxy: stored upstream %q conflicts with a code-registered one", name))
			continue
		}
		if cur != nil && reflect.DeepEqual(cur.def, d) {
			next[name] = cur
			continue
		}
		t, err := p.build(d)
		if err != nil {
			errs = append(errs, err)
			if cur != nil { // keep serving the last good definition
				next[name] = cur
			}
			continue
		}
		t.managed = true
		next[name] = t
		if cur != nil {
			retired = append(retired, cur)
		}
	}
	for name, cur := range p.upstream {
		if !cur.managed {
			continue
		}
		if _, keep := next[name]; !keep {
			delete(p.upstream, name)
			retired = append(retired, cur)
		}
	}
	for name, t := range next {
		p.upstream[name] = t
	}
	p.mu.Unlock()

	for _, t := range retired {
		p.retire(t)
	}
	return errors.Join(errs...)
}

// AutoReload calls Reload now and then every interval until ctx ends. The first error
// is returned; later ones are logged.
func (p *Proxy) AutoReload(ctx context.Context, store UpstreamStore, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("aiproxy: reload interval must be > 0")
	}
	first := p.Reload(ctx, store)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := p.Reload(ctx, store); err != nil {
					logger.Warn("aiproxy: reload: %v", err)
				}
			}
		}
	}()
	return first
}

// DefaultProc is the stored procedure (function) ProcStore calls when no name is given.
const DefaultProc = "resolvespec_ai_proxies"

var procRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// ProcStore loads upstream definitions from a stored procedure (PostgreSQL), using the
// same contract as the security procedures:
//
//	SELECT p_success, p_error, p_data FROM resolvespec_ai_proxies()
//
// p_data is a JSON array; each element has: name, kind ("openai"|"mcp"), base_url, and
// optionally api_key, auth_header, auth_format, headers (object), allowed_roles (array),
// allowed (array: models or tools), rate_per_second, rate_burst, timeout_seconds,
// enabled (default true). See resolvespec_ai_proxies.sql.
type ProcStore struct {
	db   func() *sql.DB
	proc string
}

// NewProcStore calls proc through db. An empty proc means DefaultProc.
func NewProcStore(db *sql.DB, proc string) (*ProcStore, error) {
	if db == nil {
		return nil, errors.New("aiproxy: nil database")
	}
	return newProcStore(func() *sql.DB { return db }, proc)
}

// NewProcStoreFromDatabase is NewProcStore for an application's common.Database. The
// connection is fetched on every call, so adapter reconnects are followed.
func NewProcStoreFromDatabase(db common.Database, proc string) (*ProcStore, error) {
	if db == nil {
		return nil, errors.New("aiproxy: nil database")
	}
	p, ok := db.(common.SQLDBProvider)
	if !ok {
		return nil, fmt.Errorf("aiproxy: %T does not expose a *sql.DB", db)
	}
	return newProcStore(p.SQLDB, proc)
}

func newProcStore(db func() *sql.DB, proc string) (*ProcStore, error) {
	if proc == "" {
		proc = DefaultProc
	}
	if !procRe.MatchString(proc) {
		return nil, fmt.Errorf("aiproxy: invalid procedure name %q", proc)
	}
	return &ProcStore{db: db, proc: proc}, nil
}

type procRow struct {
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	BaseURL        string            `json:"base_url"`
	APIKey         string            `json:"api_key"`
	AuthHeader     string            `json:"auth_header"`
	AuthFormat     string            `json:"auth_format"`
	Headers        map[string]string `json:"headers"`
	AllowedRoles   []string          `json:"allowed_roles"`
	Allowed        []string          `json:"allowed"`
	RatePerSecond  float64           `json:"rate_per_second"`
	RateBurst      int               `json:"rate_burst"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Enabled        *bool             `json:"enabled"`
}

// List implements UpstreamStore. Disabled and undecodable entries are skipped.
func (s *ProcStore) List(ctx context.Context) ([]Definition, error) {
	db := s.db()
	if db == nil {
		return nil, errors.New("database connection is nil")
	}
	var (
		success bool
		errMsg  sql.NullString
		data    []byte
	)
	if err := db.QueryRowContext(ctx, `SELECT p_success, p_error, p_data FROM `+s.proc+`()`).Scan(&success, &errMsg, &data); err != nil {
		return nil, err
	}
	if !success {
		if errMsg.Valid {
			return nil, errors.New(errMsg.String)
		}
		return nil, errors.New("failed to load AI proxies")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse AI proxies: %w", err)
	}

	out := make([]Definition, 0, len(raw))
	for i, r := range raw {
		var row procRow
		if err := json.Unmarshal(r, &row); err != nil {
			logger.Warn("aiproxy: proxy entry %d: %v", i, err)
			continue
		}
		if row.Enabled != nil && !*row.Enabled {
			continue
		}
		d := Definition{Kind: Kind(row.Kind), Allowed: row.Allowed, Upstream: Upstream{
			Name: row.Name, BaseURL: row.BaseURL, APIKey: row.APIKey,
			AuthHeader: row.AuthHeader, AuthFormat: row.AuthFormat,
			Headers: row.Headers, AllowedRoles: row.AllowedRoles,
			Timeout: time.Duration(row.TimeoutSeconds) * time.Second,
		}}
		if row.RatePerSecond > 0 {
			d.Upstream.RateLimit = &RateLimit{PerSecond: row.RatePerSecond, Burst: row.RateBurst}
		}
		out = append(out, d)
	}
	return out, nil
}
