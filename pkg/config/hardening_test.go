package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestManagerConcurrentSetGet(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				m.Set("x.y", j)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = m.Get("x.y")
				_, _ = m.GetConfig()
			}
		}()
	}
	wg.Wait()
}

func TestNewManagerDoesNotReplaceGlobal(t *testing.T) {
	g := GetConfigManager()
	_ = NewManager()
	if GetConfigManager() != g {
		t.Fatal("NewManager replaced the global manager")
	}
}

func TestSaveConfigPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.yaml")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewManager().SaveConfig(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestPathsSetNilAndJoinConfined(t *testing.T) {
	var pc PathsConfig
	pc.Set("data", "data")
	if got, _ := pc.Get("data"); got != "data" {
		t.Fatalf("got %q", got)
	}
	if _, err := pc.Join("data", "../../etc/passwd"); err == nil {
		t.Fatal("expected traversal error")
	}
	if p, err := pc.Join("data", "a", "b"); err != nil || p != filepath.Join("data", "a", "b") {
		t.Fatalf("got %q, %v", p, err)
	}
}

func TestConfigValidate(t *testing.T) {
	cfg, err := NewManager().GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
	cfg.EventBroker.Enabled, cfg.EventBroker.WorkerCount = true, 0
	if cfg.Validate() == nil {
		t.Fatal("expected worker_count error")
	}
}
