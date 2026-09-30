package providers

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	// tcpKeepAlive is how often keepalive probes are sent on idle connections.
	tcpKeepAlive = 30 * time.Second
	// tcpUserTimeout bounds how long written data may stay unacknowledged before
	// the kernel drops the socket. Without it a query on a silently dead peer
	// waits for tcp_retries2 (about 15 minutes).
	tcpUserTimeout = 30 * time.Second
	// resetSessionTimeout bounds the liveness ping database/sql triggers when a
	// pooled connection is reused, which otherwise runs on the request context.
	resetSessionTimeout = 5 * time.Second
)

// pgConnector is a driver.Connector whose connections can be retired without
// closing the *sql.DB. Reconnecting bumps a generation; connections created
// under an older generation report themselves invalid and database/sql
// discards them and dials new ones. Every handle wrapping the *sql.DB keeps
// working across a reconnect.
type pgConnector struct {
	inner      atomic.Pointer[connectorState]
	generation atomic.Uint64
}

type connectorState struct {
	connector driver.Connector
	gen       uint64
}

func newPGConnector(cfg *pgx.ConnConfig) *pgConnector {
	c := &pgConnector{}
	c.swap(cfg)
	return c
}

// swap installs a new connection config under a fresh generation.
func (c *pgConnector) swap(cfg *pgx.ConnConfig) {
	gen := c.generation.Add(1)
	c.inner.Store(&connectorState{connector: stdlib.GetConnector(*cfg), gen: gen})
}

func (c *pgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	st := c.inner.Load()
	conn, err := st.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	sc, ok := conn.(*stdlib.Conn)
	if !ok {
		conn.Close() //nolint:gosec // G104: best-effort call, error intentionally ignored
		return nil, fmt.Errorf("unexpected pgx driver connection type %T", conn)
	}
	return &pgConn{Conn: sc, owner: c, gen: st.gen}, nil
}

func (c *pgConnector) Driver() driver.Driver {
	return stdlib.GetDefaultDriver()
}

// pgConn embeds *stdlib.Conn, so every optional driver interface (context
// queries, Pinger, NamedValueChecker, ...) is promoted unchanged.
type pgConn struct {
	*stdlib.Conn
	owner *pgConnector
	gen   uint64
}

func (c *pgConn) stale() bool { return c.gen != c.owner.generation.Load() }

// IsValid implements driver.Validator: stale or closed connections are dropped
// when returned to the pool.
func (c *pgConn) IsValid() bool {
	return !c.stale() && !c.Conn.Conn().IsClosed()
}

// ResetSession runs when a pooled connection is reused. It discards stale
// connections and bounds pgx's liveness ping so a dead socket fails in seconds
// rather than blocking on the caller's context.
func (c *pgConn) ResetSession(ctx context.Context) error {
	if c.stale() {
		return driver.ErrBadConn
	}
	ctx, cancel := context.WithTimeout(ctx, resetSessionTimeout)
	defer cancel()
	return c.Conn.ResetSession(ctx)
}

// newDialFunc returns a pgconn dial function with TCP keepalive and, where the
// platform supports it, TCP_USER_TIMEOUT.
func newDialFunc(connectTimeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout:   connectTimeout,
		KeepAlive: tcpKeepAlive,
		Control:   setTCPUserTimeout(tcpUserTimeout),
	}
	return d.DialContext
}

// buildPGXConfig parses the DSN and applies client-side hardening: bounded
// dialing, TCP timeouts, and statement_timeout, which is set as a runtime
// parameter so it also applies to caller-supplied DSNs.
func buildPGXConfig(cfg ConnectionConfig) (*pgx.ConnConfig, error) {
	dsn, err := cfg.BuildDSN()
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	cc, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection config: %w", err)
	}

	cc.DialFunc = newDialFunc(cfg.GetConnectTimeout())
	// Also applies to caller-supplied DSNs that do not set application_name.
	if name := cfg.GetApplicationName(); name != "" {
		if _, set := cc.RuntimeParams["application_name"]; !set {
			cc.RuntimeParams["application_name"] = name
		}
	}
	if cfg.GetQueryTimeout() > 0 {
		if _, set := cc.RuntimeParams["statement_timeout"]; !set {
			cc.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", cfg.GetQueryTimeout().Milliseconds())
		}
	}
	return cc, nil
}
