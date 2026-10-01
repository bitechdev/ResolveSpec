// Package procedure is the stored-procedure backend of lookup: it calls the
// resolvespec_* functions (names from lookup.ProcNames) and keeps their
// p_success / p_error / p_data contract. Error texts match the ones the security
// package returned before the extraction.
package procedure

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

// Runner runs a database operation, reconnecting once when the *sql.DB has been closed.
type Runner interface {
	Run(run func(*sql.DB) error) error
}

// RunFunc adapts a function to Runner. The security package passes its own
// reconnecting helper this way.
type RunFunc func(run func(*sql.DB) error) error

// Run implements Runner.
func (f RunFunc) Run(run func(*sql.DB) error) error { return f(run) }

// DB is a standalone Runner over a *sql.DB with an optional reconnect factory.
type DB struct {
	mu          sync.RWMutex
	db          *sql.DB
	factory     func() (*sql.DB, error)
	onReconnect func()
}

// NewDB wraps db. factory (optional) is called to obtain a fresh handle when the current
// one is closed; onReconnect (optional) runs after a successful reconnect, e.g. to reset
// cached procedure probes.
func NewDB(db *sql.DB, factory func() (*sql.DB, error), onReconnect func()) *DB {
	return &DB{db: db, factory: factory, onReconnect: onReconnect}
}

// Get returns the current handle.
func (d *DB) Get() *sql.DB {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db
}

func (d *DB) reconnect() error {
	if d.factory == nil {
		return fmt.Errorf("no db factory configured for reconnect")
	}
	newDB, err := d.factory()
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.db = newDB
	d.mu.Unlock()
	if d.onReconnect != nil {
		d.onReconnect()
	}
	return nil
}

// Run implements Runner.
func (d *DB) Run(run func(*sql.DB) error) error {
	db := d.Get()
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	err := run(db)
	if IsClosed(err) {
		if reconnErr := d.reconnect(); reconnErr == nil {
			err = run(d.Get())
		}
	}
	return err
}

// IsClosed reports whether err indicates the *sql.DB has been closed.
func IsClosed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "sql: database is closed")
}

// failure builds the error for p_success = false: the procedure's own message when it
// returned one, otherwise def.
func failure(errMsg sql.NullString, def string) error {
	if errMsg.Valid {
		return fmt.Errorf("%s", errMsg.String)
	}
	return fmt.Errorf("%s", def)
}
