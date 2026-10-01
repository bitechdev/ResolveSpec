package backends

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

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// Container tests start a throwaway database server with podman or docker (whichever is
// installed, podman first) and run the conformance suite against it. They pull an image, so
// they only run when RESOLVESPEC_TEST_CONTAINERS=1 and not with -short. The container is
// removed when the test ends.

const containerPassword = "Resolve_Spec_1"

func containerRuntime(t *testing.T) string {
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

func run(t *testing.T, timeout time.Duration, name string, args ...string) string {
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

// startContainer runs image publishing containerPort on a random localhost port and returns
// the host port. The container is force-removed on cleanup.
func startContainer(t *testing.T, rt, image, containerPort string, env map[string]string) string {
	t.Helper()
	args := []string{"run", "-d", "--rm", "-p", "127.0.0.1::" + containerPort}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, image)
	id := run(t, 10*time.Minute, rt, args...) // first run may pull the image
	t.Cleanup(func() { _ = exec.Command(rt, "rm", "-f", id).Run() })

	// "127.0.0.1:49153" (docker may print one line per address family)
	out := run(t, 30*time.Second, rt, "port", id, containerPort)
	line := strings.Fields(out)[len(strings.Fields(out))-1]
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "127.0.0.1") || strings.Contains(l, " 127.0.0.1:") {
			line = l[strings.LastIndex(l, " ")+1:]
			break
		}
	}
	_, port, err := net.SplitHostPort(line)
	if err != nil {
		t.Fatalf("cannot parse published port %q: %v", out, err)
	}
	return port
}

// waitReady retries until the server accepts queries or the deadline passes.
func waitReady(t *testing.T, driver, dsn string, d time.Duration) *sql.DB {
	t.Helper()
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		db, err := sql.Open(driver, dsn)
		if err == nil {
			if last = db.Ping(); last == nil {
				t.Cleanup(func() { _ = db.Close() })
				return db
			}
			_ = db.Close()
		} else {
			last = err
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("database did not become ready within %s: %v", d, last)
	return nil
}

func TestConformancePostgresContainer(t *testing.T) {
	rt := containerRuntime(t)
	port := startContainer(t, rt, "docker.io/library/postgres:16-alpine", "5432", map[string]string{"POSTGRES_PASSWORD": containerPassword})
	dsn := func(db string) string {
		return fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%s/%s?sslmode=disable", containerPassword, port, db)
	}
	admin := waitReady(t, "pgx", dsn("postgres"), 90*time.Second)
	// The official image restarts once during init: make sure the second start is the one we use.
	time.Sleep(2 * time.Second)
	admin = waitReady(t, "pgx", dsn("postgres"), 60*time.Second)
	for _, name := range []string{"cf_proc", "cf_direct"} {
		if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("procedure", func(t *testing.T) {
		db, err := sql.Open("pgx", dsn("cf_proc"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		for _, f := range []string{"../database_schema.sql", "../keystore_schema.sql"} {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(b)); err != nil {
				t.Fatalf("apply %s: %v", f, err)
			}
		}
		runConformance(t, db, "postgres", lookup.Config{Mode: lookup.ModeProcedure}, true)
	})
	t.Run("direct", func(t *testing.T) {
		runOnServer(t, "pgx", dsn("cf_direct"), "postgres", lookup.Config{Mode: lookup.ModeDirect}, true)
	})
}

func TestConformanceMSSQLContainer(t *testing.T) {
	rt := containerRuntime(t)
	port := startContainer(t, rt, "mcr.microsoft.com/mssql/server:2022-latest", "1433", map[string]string{
		"ACCEPT_EULA": "Y", "MSSQL_SA_PASSWORD": containerPassword,
	})
	dsn := func(db string) string {
		return fmt.Sprintf("sqlserver://sa:%s@127.0.0.1:%s?database=%s&encrypt=disable", containerPassword, port, db)
	}
	admin := waitReady(t, "sqlserver", dsn("master"), 3*time.Minute)
	if _, err := admin.Exec("CREATE DATABASE cf_direct"); err != nil {
		t.Fatal(err)
	}
	runOnServer(t, "sqlserver", dsn("cf_direct"), "mssql", lookup.Config{}, true)
}
