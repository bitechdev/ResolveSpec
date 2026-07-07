package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// QueryMode selects how a provider talks to the database: via the configured
// resolvespec_* stored procedure (ModeProcedure), via portable Go/SQL logic
// (ModeDirect), or auto-detected per connection (ModeAuto, the default).
type QueryMode int

const (
	// ModeAuto probes whether the configured stored procedure exists on a
	// Postgres connection and uses it if so, otherwise falls back to Direct mode.
	ModeAuto QueryMode = iota
	// ModeProcedure always calls the configured resolvespec_* stored procedure.
	ModeProcedure
	// ModeDirect always uses the portable Go/SQL implementation, never the
	// stored procedure.
	ModeDirect
)

// dbCapability probes and caches whether a given *sql.DB is Postgres and
// whether specific stored procedures exist on it. One instance is shared by
// a provider (DatabaseAuthenticator, DatabaseTwoFactorProvider, etc.) across
// all of its operations.
type dbCapability struct {
	funcExists sync.Map // procName (string) -> exists (bool)
}

// newDBCapability creates a new, empty capability cache.
func newDBCapability() *dbCapability {
	return &dbCapability{}
}

// reset clears all cached probe results. Call after reconnecting to a
// (possibly different) database.
func (c *dbCapability) reset() {
	c.funcExists.Range(func(key, _ any) bool {
		c.funcExists.Delete(key)
		return true
	})
}

// probeFunctionExists checks, via a Postgres-specific system catalog query,
// whether a function named procName exists. Any error (wrong dialect,
// placeholder syntax rejected, relation missing, etc.) is treated as "does
// not exist" rather than propagated - the probe must never be able to panic
// or block resolution of the query mode.
func probeFunctionExists(ctx context.Context, db *sql.DB, procName string) bool {
	if db == nil {
		return false
	}
	var exists bool
	defer func() {
		// Guard against any unexpected panic from a misbehaving driver.
		_ = recover()
	}()
	row := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = $1 LIMIT 1)`, procName)
	if err := row.Scan(&exists); err != nil {
		return false
	}
	return exists
}

// ShouldUseProcedure decides whether the stored-procedure code path should be
// used for procName given mode. ModeProcedure/ModeDirect are unconditional.
//
// ModeAuto resolves as follows:
//   - A recognized portable-only driver (SQLite, MySQL) always uses Direct
//     mode; no query is issued.
//   - A recognized Postgres driver (lib/pq, pgx) probes pg_proc for procName
//     and uses the stored procedure only if it actually exists there.
//   - Any other/unrecognized driver (including test doubles such as
//     sqlmock) cannot be safely dialect-probed without risking an
//     unexpected query against a strictly-ordered mock, so it defaults to
//     the stored-procedure path, preserving pre-existing behavior for
//     callers that configure Postgres-flavored access without an
//     identifiable driver type.
func (c *dbCapability) ShouldUseProcedure(ctx context.Context, mode QueryMode, db *sql.DB, procName string) bool {
	switch mode {
	case ModeProcedure:
		return true
	case ModeDirect:
		return false
	default:
		if cached, ok := c.funcExists.Load(procName); ok {
			return cached.(bool)
		}

		var exists bool
		switch {
		case driverIsPortableOnly(db):
			exists = false
		case driverIsPostgres(db):
			exists = probeFunctionExists(ctx, db, procName)
		default:
			exists = true
		}

		c.funcExists.Store(procName, exists)
		return exists
	}
}

// driverIsPostgres reports whether db's underlying driver looks like a
// Postgres driver (lib/pq or pgx), based on the driver's Go type name.
func driverIsPostgres(db *sql.DB) bool {
	if db == nil {
		return false
	}
	t := strings.ToLower(fmt.Sprintf("%T", db.Driver()))
	return strings.Contains(t, "pq.") || strings.Contains(t, "pgx") || strings.Contains(t, "postgres")
}

// driverIsPortableOnly reports whether db's underlying driver is a dialect
// that never has the resolvespec_* Postgres functions available (SQLite,
// MySQL), so ModeAuto can skip probing entirely and go straight to Direct.
func driverIsPortableOnly(db *sql.DB) bool {
	if db == nil {
		return false
	}
	t := strings.ToLower(fmt.Sprintf("%T", db.Driver()))
	return strings.Contains(t, "sqlite") || strings.Contains(t, "mysql")
}

// rewritePlaceholders converts a query written with "?" placeholders
// (SQLite/MySQL style) to Postgres "$1", "$2", ... style when db's driver is
// Postgres. All Direct-mode SQL in this package is written with "?" and
// passed through this helper before execution so the same query source works
// against SQLite, MySQL, and (in the rare fallback case) Postgres without the
// resolvespec_* functions installed.
func rewritePlaceholders(db *sql.DB, query string) string {
	if !driverIsPostgres(db) {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$")
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// generateSessionToken produces a session token in the same shape the
// plpgsql stored procedures generate: "sess_" + hex(32 random bytes) + "_" +
// unix timestamp, so downstream code that parses/displays tokens is
// unaffected by which mode created them.
func generateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("sess_%s_%d", hex.EncodeToString(buf), time.Now().Unix()), nil
}
