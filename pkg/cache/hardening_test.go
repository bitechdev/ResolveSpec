package cache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type failSetProvider struct{ *MemoryProvider }

func (f failSetProvider) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("backend down")
}

func TestGetOrSetSurvivesCacheWriteFailure(t *testing.T) {
	c := NewCache(failSetProvider{NewMemoryProvider(nil)})
	var out string
	err := c.GetOrSet(context.Background(), "k", &out, time.Minute, func() (interface{}, error) { return "v", nil })
	if err != nil || out != "v" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestNotFoundDoesNotLeakKey(t *testing.T) {
	c := NewCache(NewMemoryProvider(nil))
	err := c.Get(context.Background(), "auth:session:SECRET", new(string))
	if !errors.Is(err, ErrNotFound) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestMemoryTagIndexCleanedOnAllRemovals(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryProvider(&Options{MaxSize: 2})
	defer m.Close()
	for i := 0; i < 50; i++ {
		_ = m.SetWithTags(ctx, fmt.Sprintf("k%d", i), []byte("x"), time.Minute, []string{"t"})
	}
	m.mu.RLock()
	n := len(m.tagToKeys["t"])
	m.mu.RUnlock()
	if n > 2 {
		t.Fatalf("tag index leaked: %d members with MaxSize 2", n)
	}
	_ = m.Clear(ctx)
	if len(m.tagToKeys) != 0 {
		t.Fatal("Clear did not reset tag index")
	}
}

func TestMemoryClosedNoPanic(t *testing.T) {
	m := NewMemoryProvider(nil)
	_ = m.Close()
	if err := m.Set(context.Background(), "k", []byte("v"), 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if _, ok := m.Get(context.Background(), "k"); ok {
		t.Fatal("hit after close")
	}
}

func TestMemoryCopiesAndDefaults(t *testing.T) {
	ctx := context.Background()
	opts := &Options{}
	m := NewMemoryProvider(opts)
	defer m.Close()
	if opts.MaxSize != 0 || m.options.MaxSize != defaultMemoryMaxSize {
		t.Fatal("options not copied/defaulted")
	}
	buf := []byte("abc")
	_ = m.Set(ctx, "k", buf, time.Minute)
	buf[0] = 'X'
	got, _ := m.Get(ctx, "k")
	if string(got) != "abc" {
		t.Fatalf("stored slice aliased caller: %q", got)
	}
	got[0] = 'Y'
	if again, _ := m.Get(ctx, "k"); string(again) != "abc" {
		t.Fatal("returned slice aliases stored value")
	}
}

func TestMemoryJanitorRemovesExpired(t *testing.T) {
	m := NewMemoryProvider(&Options{CleanupInterval: 10 * time.Millisecond})
	defer m.Close()
	_ = m.Set(context.Background(), "k", []byte("v"), 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	m.mu.RLock()
	n := len(m.items)
	m.mu.RUnlock()
	if n != 0 {
		t.Fatalf("expired item still stored: %d", n)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryProvider(&Options{MaxSize: 50})
	defer m.Close()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				k := fmt.Sprintf("k%d", i%80)
				_ = m.SetWithTags(ctx, k, []byte("v"), time.Millisecond, []string{"t"})
				m.Get(ctx, k)
				if i%50 == 0 {
					_ = m.DeleteByTag(ctx, "t")
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestGetDefaultCacheConcurrent(t *testing.T) {
	SetDefaultCache(nil)
	var wg sync.WaitGroup
	res := make([]*Cache, 16)
	for i := range res {
		wg.Add(1)
		go func(i int) { defer wg.Done(); res[i] = GetDefaultCache() }(i)
	}
	wg.Wait()
	for _, c := range res {
		if c != res[0] {
			t.Fatal("different default caches returned")
		}
	}
}

func TestMemcacheKeyAndExpiry(t *testing.T) {
	if k := memcacheKey(strings.Repeat("a", 300)); len(k) > 250 || !legalMemcacheKey(k) {
		t.Fatalf("bad key %q", k)
	}
	if k := memcacheKey("has space"); !legalMemcacheKey(k) {
		t.Fatal("illegal key not normalised")
	}
	if memcacheKey("a") != "k:a" {
		t.Fatal("unexpected prefix")
	}
	if got := memcacheExpiry(30*24*time.Hour + time.Hour); got < int32(time.Now().Unix()) {
		t.Fatalf("expected absolute timestamp, got %d", got)
	}
	if memcacheExpiry(-time.Second) != 0 || memcacheExpiry(time.Minute) != 60 {
		t.Fatal("bad relative expiry")
	}
}
