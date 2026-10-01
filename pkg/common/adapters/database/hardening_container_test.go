package database

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// These tests start a throwaway PostgreSQL server with podman or docker (whichever is
// installed, podman first) and run the client-SQL hardening against a real database. They
// pull an image, so they only run when RESOLVESPEC_TEST_CONTAINERS=1 and not with -short.
// The container is removed when the test ends.

const hardeningPGPassword = "Resolve_Spec_1"

type hardeningItem struct {
	bun.BaseModel `bun:"table:items,alias:items"`
	ID            int    `bun:"id"`
	Tenant        int    `bun:"tenant"`
	Name          string `bun:"name"`
}

func hardeningRuntime(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("container tests are skipped with -short")
	}
	if os.Getenv("RESOLVESPEC_TEST_CONTAINERS") != "1" {
		t.Skip("set RESOLVESPEC_TEST_CONTAINERS=1 to run tests that start a podman/docker container")
	}
	for _, rt := range []string{"podman", "docker"} {
		if p, err := exec.LookPath(rt); err == nil {
			return p
		}
	}
	t.Skip("neither podman nor docker found in PATH")
	return ""
}

func hardeningRun(t *testing.T, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, errb.String())
	}
	return strings.TrimSpace(out.String())
}

// startHardeningPostgres runs postgres on a random localhost port and returns a ready *sql.DB.
func startHardeningPostgres(t *testing.T, rt string) *sql.DB {
	t.Helper()
	id := hardeningRun(t, 10*time.Minute, rt, "run", "-d", "--rm", "-p", "127.0.0.1::5432",
		"-e", "POSTGRES_PASSWORD="+hardeningPGPassword, "docker.io/library/postgres:16-alpine") // first run may pull
	t.Cleanup(func() { _ = exec.Command(rt, "rm", "-f", id).Run() })

	out := hardeningRun(t, 30*time.Second, rt, "port", id, "5432")
	line := strings.Fields(out)[len(strings.Fields(out))-1]
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "127.0.0.1:") {
			line = l[strings.LastIndex(l, " ")+1:]
			break
		}
	}
	_, port, err := net.SplitHostPort(line)
	if err != nil {
		t.Fatalf("cannot parse published port %q: %v", out, err)
	}
	dsn := fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%s/postgres?sslmode=disable", hardeningPGPassword, port)

	// The official image restarts once during init: wait, pause, wait again.
	wait := func(d time.Duration) *sql.DB {
		deadline := time.Now().Add(d)
		var last error
		for time.Now().Before(deadline) {
			db, err := sql.Open("pgx", dsn)
			if err == nil {
				if last = db.Ping(); last == nil {
					return db
				}
				_ = db.Close()
			} else {
				last = err
			}
			time.Sleep(time.Second)
		}
		t.Fatalf("postgres not ready within %s: %v", d, last)
		return nil
	}
	_ = wait(90 * time.Second).Close()
	time.Sleep(2 * time.Second)
	db := wait(60 * time.Second)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// setHardeningConfig overrides the global hardening switches for the test.
func setHardeningConfig(t *testing.T, h config.HardeningConfig) {
	t.Helper()
	m := config.GetConfigManager()
	cfg, err := m.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	old := cfg.Hardening
	cfg.Hardening = h
	if err := m.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cfg.Hardening = old
		_ = m.SetConfig(cfg)
	})
}

// clientWhere mirrors the restheadspec handler pipeline for x-custom-sql-w.
func clientWhere(raw string) string {
	w := common.AddTablePrefixToColumns(raw, "items")
	w = common.SanitizeWhereClause(w, "items")
	return common.EnsureOuterParentheses(w)
}

func TestHardeningAgainstPostgresContainer(t *testing.T) {
	rt := hardeningRuntime(t)
	sqldb := startHardeningPostgres(t, rt)
	if _, err := sqldb.Exec(`
		CREATE TABLE items (id int PRIMARY KEY, tenant int, name text);
		INSERT INTO items VALUES (1,5,'mine-a'),(2,5,'mine-b'),(3,6,'other-a'),(4,6,'awaiting update approval');`); err != nil {
		t.Fatal(err)
	}
	bdb := bun.NewDB(sqldb, pgdialect.New())
	adapter := NewBunAdapter(bdb)
	ctx := context.Background()

	// list runs the handler-shaped query: client x-custom-sql-w, then the server tenant filter.
	list := func(t *testing.T, where string) []hardeningItem {
		t.Helper()
		var rows []hardeningItem
		q := adapter.NewSelect().Model(&rows)
		if w := clientWhere(where); w != "" {
			q = q.Where(w)
		}
		q = q.Where("items.tenant = ?", 5)
		if err := q.Scan(ctx, &rows); err != nil {
			t.Fatalf("query failed for %q: %v", where, err)
		}
		return rows
	}

	t.Run("strict", func(t *testing.T) {
		setHardeningConfig(t, config.HardeningConfig{CORSStrictOrigins: true, SortStrict: true, SQLStrict: true})

		t.Run("legitimate filters keep working", func(t *testing.T) {
			if got := list(t, "name = 'mine-a'"); len(got) != 1 || got[0].ID != 1 {
				t.Errorf("simple filter: %v", got)
			}
			if got := list(t, "id in (select id from items where tenant = 5)"); len(got) != 2 {
				t.Errorf("subquery filter: %v", got)
			}
		})

		t.Run("parenthesis escape cannot leave the tenant", func(t *testing.T) {
			if got := list(t, "1=1)) OR ((1=1"); len(got) != 0 {
				t.Errorf("escape not rejected closed, got rows: %v", got)
			}
		})

		t.Run("hostile fragments fail closed", func(t *testing.T) {
			for _, w := range []string{
				"id = 1 and pg_sleep(10) is not null",
				"id = 1 or (select count(*) from pg_shadow) > 0",
				"id = 1; delete/**/from items",
			} {
				start := time.Now()
				if got := list(t, w); len(got) != 0 {
					t.Errorf("%q returned rows: %v", w, got)
				}
				if time.Since(start) > 5*time.Second {
					t.Errorf("%q was executed (took %s)", w, time.Since(start))
				}
			}
			var n int
			if err := sqldb.QueryRow("SELECT count(*) FROM items").Scan(&n); err != nil || n != 4 {
				t.Errorf("items table modified: count=%d err=%v", n, err)
			}
		})

		t.Run("x-custom-sql-or stays inside the tenant", func(t *testing.T) {
			var rows []hardeningItem
			q := adapter.NewSelect().Model(&rows)
			orClause := common.EnsureOuterParentheses(common.SanitizeWhereClause("items.name = 'other-a'", "items"))
			q = q.(common.WhereGrouper).WhereGroup(func(g common.SelectQuery) common.SelectQuery {
				return g.Where("items.name = ?", "mine-a").WhereOr(orClause)
			})
			q = q.Where("items.tenant = ?", 5)
			if err := q.Scan(ctx, &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].ID != 1 {
				t.Errorf("OR clause leaked outside tenant filter: %v", rows)
			}
		})
	})

	t.Run("switch off restores legacy behaviour", func(t *testing.T) {
		setHardeningConfig(t, config.HardeningConfig{})
		// Proves the strict assertions above are meaningful: without hardening the same
		// escape returns the other tenant's rows.
		if got := list(t, "1=1)) OR ((1=1"); len(got) < 3 {
			t.Errorf("expected the legacy escape to leak rows, got %v", got)
		}
	})
}
