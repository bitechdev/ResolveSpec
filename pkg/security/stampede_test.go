package security

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConcurrentColdLoadsShareOneProviderCall(t *testing.T) {
	p := &slowProvider{delay: 100 * time.Millisecond}
	sl, _ := NewSecurityList(p)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = sl.LoadColumnSecurity(context.Background(), 1, "s", "t", false) }()
		go func() { defer wg.Done(); _, _ = sl.LoadRowSecurity(context.Background(), 1, "s", "t", false) }()
	}
	wg.Wait()
	if got := p.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2 (one column, one row)", got)
	}
}

func TestActivityThrottle(t *testing.T) {
	var th activityThrottle
	now := time.Now()
	if !th.allow("a", now) {
		t.Fatal("first call must be allowed")
	}
	if th.allow("a", now.Add(sessionActivityInterval/2)) {
		t.Fatal("call inside interval must be skipped")
	}
	if !th.allow("b", now) {
		t.Fatal("other token must be allowed")
	}
	if !th.allow("a", now.Add(sessionActivityInterval)) {
		t.Fatal("call after interval must be allowed")
	}
}
