package dbmanager

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestLiveServerRestart(t *testing.T) {
	dir := os.Getenv("PG_RESTART_DIR")
	if dir == "" {
		t.Skip("PG_RESTART_DIR not set")
	}
	mgr, _ := NewManager(ManagerConfig{
		DefaultConnection: "pg",
		Connections: map[string]ConnectionConfig{"pg": {
			Name: "pg", Type: DatabaseTypePostgreSQL, Host: "127.0.0.1", Port: 54329,
			User: "postgres", Database: "postgres", ConnectTimeout: 2 * time.Second,
		}},
		HealthCheckInterval: 500 * time.Millisecond,
	})
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()
	db, _ := conn.Bun()
	gdb, _ := conn.GORM()
	query := func() error { var n int; return db.DB.QueryRow("select 1").Scan(&n) }
	if err := query(); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) {
		// Output must not be piped: the daemonised server would hold the pipe open.
		if err := exec.Command("pg_ctl", append([]string{"-D", dir}, args...)...).Run(); err != nil {
			t.Fatalf("pg_ctl %v: %v", args, err)
		}
	}
	run("-m", "immediate", "-w", "stop") // crash-style shutdown
	time.Sleep(1500 * time.Millisecond)  // health checks fail meanwhile
	if err := query(); err == nil {
		t.Fatal("expected failure while server is down")
	}
	run("-l", dir+"/restart.log", "-o", "-p 54329 -k "+dir+" -c listen_addresses=127.0.0.1", "-w", "start")

	var last error
	for i := 0; i < 20; i++ {
		if last = query(); last == nil {
			break
		}
		t.Logf("attempt %d after restart: %v", i, last)
		time.Sleep(200 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("held bun handle never recovered: %v", last)
	}
	var n int
	if err := gdb.Raw("select 1").Scan(&n).Error; err != nil {
		t.Fatalf("held gorm handle: %v", err)
	}
	time.Sleep(time.Second)
	if err := conn.HealthCheck(ctx); err != nil {
		t.Fatalf("health check after restart: %v", err)
	}
}
