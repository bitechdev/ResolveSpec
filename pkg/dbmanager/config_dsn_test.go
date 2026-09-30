package dbmanager

import (
	"net/url"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func TestPostgresDSNEscapesCredentials(t *testing.T) {
	cc := ConnectionConfig{
		Type: DatabaseTypePostgreSQL, Host: "db", Port: 5432, Database: "app",
		User: "u@x", Password: "p w'd sslmode=disable&x=y/?#",
	}
	dsn := cc.buildPostgresDSN()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("DSN is not a valid URL: %v", err)
	}
	if pw, _ := u.User.Password(); pw != cc.Password {
		t.Errorf("password did not round-trip: %q", pw)
	}
	if u.User.Username() != cc.User {
		t.Errorf("user did not round-trip: %q", u.User.Username())
	}
	if got := u.Query().Get("sslmode"); got != "prefer" {
		t.Errorf("sslmode = %q, want prefer (password must not inject parameters)", got)
	}
}

func TestMSSQLAndMongoDSNEscapeCredentials(t *testing.T) {
	cc := ConnectionConfig{Host: "h", Port: 1, Database: "d", User: "u", Password: "a@b:c/d?e&f"}
	for name, dsn := range map[string]string{"mssql": cc.buildMSSQLDSN(), "mongo": cc.buildMongoDSN()} {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pw, _ := u.User.Password(); pw != cc.Password {
			t.Errorf("%s: password did not round-trip: %q", name, pw)
		}
		if u.Host != "h:1" {
			t.Errorf("%s: host = %q", name, u.Host)
		}
	}
}

func TestSQLiteDSNUsesPragmas(t *testing.T) {
	cc := ConnectionConfig{FilePath: "/tmp/x.db", QueryTimeout: 3 * time.Second}
	dsn := cc.buildSQLiteDSN()
	if strings.Contains(dsn, "?_timeout=") {
		t.Errorf("unsupported _timeout parameter present: %s", dsn)
	}
	if !strings.Contains(dsn, "busy_timeout%283000%29") || !strings.Contains(dsn, "journal_mode%28WAL%29") {
		t.Errorf("expected busy_timeout and WAL pragmas in DSN: %s", dsn)
	}
}

func TestQueryTimeoutHonoredWithoutFloor(t *testing.T) {
	cc := ConnectionConfig{QueryTimeout: 30 * time.Second}
	cc.ApplyDefaults(&ManagerConfig{})
	if cc.QueryTimeout != 30*time.Second {
		t.Errorf("QueryTimeout = %v, want 30s", cc.QueryTimeout)
	}
}

func TestRetryPolicyInherited(t *testing.T) {
	g := ManagerConfig{RetryAttempts: 5, RetryDelay: time.Second, RetryMaxDelay: time.Minute}
	cc := ConnectionConfig{}
	cc.ApplyDefaults(&g)
	if cc.GetRetryAttempts() != 5 || cc.GetRetryMaxDelay() != time.Minute {
		t.Errorf("retry policy not inherited: %+v", cc)
	}
}

func TestSQLiteMemoryPoolPinned(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	if err := mgr.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()
	db, _ := conn.Native()
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1 for :memory:", got)
	}
	if got := conn.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("ConnectionStats.MaxOpenConnections = %d, want 1", got)
	}
	mgr.(*connectionManager).PublishMetrics()
	name := conn.Name()
	var m dto.Metric
	if err := connectionPoolSize.WithLabelValues(name, string(conn.Stats().Type), "max").Write(&m); err != nil {
		t.Fatal(err)
	}
	if got := m.GetGauge().GetValue(); got != 1 {
		t.Fatalf("pool_size{state=max} = %v, want 1", got)
	}
	if _, err := db.Exec("CREATE TABLE t(a int)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil {
			t.Fatalf("table missing on later use: %v", err)
		}
	}
}
