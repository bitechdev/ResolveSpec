package dbmanager

import (
	"context"
	"github.com/bitechdev/ResolveSpec/pkg/dbmanager/providers"
	"os"
	"testing"
	"time"
)

func TestLivePostgresRefreshKeepsHandles(t *testing.T) {
	if os.Getenv("PG_LIVE") == "" {
		t.Skip("PG_LIVE not set")
	}
	mgr, err := NewManager(ManagerConfig{
		DefaultConnection: "pg",
		Connections: map[string]ConnectionConfig{"pg": {
			Name: "pg", Type: DatabaseTypePostgreSQL, Host: "127.0.0.1", Port: 54329,
			User: "postgres", Database: "postgres", QueryTimeout: 30 * time.Second,
		}},
		HealthCheckInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()
	held, _ := conn.Bun()
	gormDB, _ := conn.GORM()

	var pid1, pid2 int
	var st string
	if err := held.DB.QueryRow("select pg_backend_pid(), current_setting('statement_timeout')").Scan(&pid1, &st); err != nil {
		t.Fatal(err)
	}
	if st != "30s" {
		t.Errorf("statement_timeout = %q", st)
	}
	if err := conn.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := held.DB.QueryRow("select pg_backend_pid()").Scan(&pid2); err != nil {
		t.Fatalf("held bun handle broken after reconnect: %v", err)
	}
	if pid1 == pid2 {
		t.Error("expected a new backend after reconnect")
	}
	var n int
	if err := gormDB.Raw("select 1").Scan(&n).Error; err != nil || n != 1 {
		t.Fatalf("held gorm handle broken: %v", err)
	}
}

func TestLiveListenerListenNotify(t *testing.T) {
	if os.Getenv("PG_LIVE") == "" {
		t.Skip("PG_LIVE not set")
	}
	cc := ConnectionConfig{Name: "pg", Type: DatabaseTypePostgreSQL, Host: "127.0.0.1", Port: 54329,
		User: "postgres", Database: "postgres", ConnectTimeout: 5 * time.Second}
	p := providers.NewPostgresProvider()
	ctx := context.Background()
	if err := p.Connect(ctx, &cc); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	l, err := p.GetListener(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	for _, ch := range []string{"a", "b"} {
		if err := l.Listen(ch, func(c, payload string) { got <- c + ":" + payload }); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := l.Notify(ctx, "a", "x"); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	select {
	case v := <-got:
		if v != "a:x" {
			t.Fatalf("got %q", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no notification")
	}
}
