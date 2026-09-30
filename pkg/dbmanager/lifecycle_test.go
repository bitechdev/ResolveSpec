package dbmanager

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"
)

func sqliteManagerConfig() ManagerConfig {
	return ManagerConfig{
		DefaultConnection: "test",
		Connections: map[string]ConnectionConfig{
			"test": {Name: "test", Type: DatabaseTypeSQLite, FilePath: ":memory:"},
		},
		HealthCheckInterval: time.Hour,
	}
}

func TestManagerConnectCloseCycleTwice(t *testing.T) {
	mgr, err := NewManager(sqliteManagerConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := mgr.Connect(ctx); err != nil {
			t.Fatalf("cycle %d connect: %v", i, err)
		}
		cm := mgr.(*connectionManager)
		cm.healthMu.Lock()
		running := cm.healthTicker != nil
		cm.healthMu.Unlock()
		if !running {
			t.Fatalf("cycle %d: health checker not running", i)
		}
		if err := mgr.Close(); err != nil {
			t.Fatalf("cycle %d close: %v", i, err)
		}
	}
	// A further Close must not panic.
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerConnectIsIdempotent(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Stats().TotalConnections; got != 1 {
		t.Fatalf("expected 1 connection, got %d", got)
	}
}

func TestConcurrentReconnectIsAtomic(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := conn.Reconnect(ctx); err != nil {
				t.Errorf("reconnect: %v", err)
			}
		}()
	}
	wg.Wait()

	db, err := conn.Native()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("pool unusable after concurrent reconnects: %v", err)
	}
}

func TestAdapterFactoryDoesNotClosePool(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()
	sc := conn.(*sqlConnection)

	held, _ := sc.Native()
	if _, err := sc.reopenNativeForAdapter(); err != nil {
		t.Fatal(err)
	}
	if err := held.PingContext(ctx); err != nil {
		t.Fatalf("existing handle broken by adapter factory: %v", err)
	}
}

func TestHealthCheckDoesNotBlockAccessors(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _ := mgr.GetDefault()
	sc := conn.(*sqlConnection)

	// Simulate a health check in flight: it holds lifecycleMu (read) only.
	sc.lifecycleMu.RLock()
	defer sc.lifecycleMu.RUnlock()

	done := make(chan struct{})
	go func() {
		_, _ = sc.Bun()
		_, _ = sc.GORM()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("accessors blocked while health check in flight")
	}
}

func TestCloseAlwaysMarksDisconnected(t *testing.T) {
	mgr, _ := NewManager(sqliteManagerConfig())
	ctx := context.Background()
	if err := mgr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	conn, _ := mgr.GetDefault()
	sc := conn.(*sqlConnection)
	_, _ = sc.Bun()
	_ = mgr.Close()

	if _, err := sc.Bun(); err == nil {
		t.Fatal("Bun() should fail after Close")
	}
	if _, err := sc.GORM(); err == nil {
		t.Fatal("GORM() should fail after Close")
	}
}

func TestReconnectOnExistingDBKeepsCallersPool(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("existing", DatabaseTypeSQLite, db)
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := conn.Reconnect(ctx); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("caller's pool was closed by Reconnect: %v", err)
	}
}

func TestCloseOnExistingDBLeavesCallersPoolOpen(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("existing", DatabaseTypeSQLite, db)
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Bun(); err != nil { // bun.DB.Close would close the pool
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("caller's pool was closed: %v", err)
	}
}
