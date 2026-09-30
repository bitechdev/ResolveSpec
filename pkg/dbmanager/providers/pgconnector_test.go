package providers

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestConnectorGenerationInvalidatesConns(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	c := newPGConnector(cfg)
	conn := &pgConn{owner: c, gen: c.generation.Load()}
	if conn.stale() {
		t.Fatal("fresh connection reported stale")
	}
	c.swap(cfg)
	if !conn.stale() {
		t.Fatal("connection from an older generation must be stale")
	}
	if err := conn.ResetSession(t.Context()); err != driver.ErrBadConn {
		t.Fatalf("ResetSession on stale conn = %v, want ErrBadConn", err)
	}
}

func TestDialFuncHasTimeout(t *testing.T) {
	if newDialFunc(2*time.Second) == nil {
		t.Fatal("nil dial func")
	}
}
