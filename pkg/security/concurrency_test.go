package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// slowProvider embeds a nil SecurityProvider; only the two load methods are used.
type slowProvider struct {
	SecurityProvider
	calls   atomic.Int32
	active  atomic.Int32
	maxSeen atomic.Int32
	delay   time.Duration
}

func (p *slowProvider) enter() {
	p.calls.Add(1)
	n := p.active.Add(1)
	for {
		m := p.maxSeen.Load()
		if n <= m || p.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(p.delay)
	p.active.Add(-1)
}

func (p *slowProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	p.enter()
	return []ColumnSecurity{{Schema: schema, Tablename: table}}, nil
}

func (p *slowProvider) GetRowSecurity(ctx context.Context, ref any, schema, table string) (RowSecurity, error) {
	p.enter()
	return RowSecurity{Schema: schema, Tablename: table}, nil
}

func TestLoadDoesNotHoldLockAcrossProvider(t *testing.T) {
	p := &slowProvider{delay: 100 * time.Millisecond}
	sl, _ := NewSecurityList(p)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); _ = sl.LoadColumnSecurity(context.Background(), i, "s", "t", false) }(i)
		go func(i int) { defer wg.Done(); _, _ = sl.LoadRowSecurity(context.Background(), i, "s", "t", false) }(i)
	}
	wg.Wait()
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("loads serialised: %v", el)
	}
	if p.maxSeen.Load() < 2 {
		t.Fatal("provider calls never overlapped")
	}
}

func TestLoadCachesAndHonoursOverwrite(t *testing.T) {
	p := &slowProvider{}
	sl, _ := NewSecurityList(p)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = sl.LoadColumnSecurity(ctx, 1, "s", "t", false)
		_, _ = sl.LoadRowSecurity(ctx, 1, "s", "t", false)
	}
	if got := p.calls.Load(); got != 2 {
		t.Fatalf("expected 2 provider calls (cached), got %d", got)
	}
	_ = sl.LoadColumnSecurity(ctx, 1, "s", "t", true)
	_, _ = sl.LoadRowSecurity(ctx, 1, "s", "t", true)
	if got := p.calls.Load(); got != 4 {
		t.Fatalf("overwrite should reload: got %d calls", got)
	}
}

func TestLoadExpiryAndPrune(t *testing.T) {
	p := &slowProvider{}
	sl, _ := NewSecurityList(p)
	ctx := context.Background()
	_ = sl.LoadColumnSecurity(ctx, 1, "s", "t", false)

	sl.ColumnSecurityMutex.Lock()
	sl.colSecExpiry["s.t@1"] = time.Now().Add(-time.Second)
	sl.ColumnSecurityMutex.Unlock()
	_ = sl.LoadColumnSecurity(ctx, 1, "s", "t", false)
	if p.calls.Load() != 2 {
		t.Fatal("expired entry should reload")
	}

	sl.ColumnSecurityMutex.Lock()
	sl.colSecExpiry["s.old@9"] = time.Now().Add(-time.Hour)
	sl.ColumnSecurity["s.old@9"] = nil
	sl.lastColPrune = time.Time{}
	sl.ColumnSecurityMutex.Unlock()
	_ = sl.LoadColumnSecurity(ctx, 2, "s", "t", false)
	sl.ColumnSecurityMutex.RLock()
	_, ok := sl.ColumnSecurity["s.old@9"]
	sl.ColumnSecurityMutex.RUnlock()
	if ok {
		t.Fatal("stale entry not pruned")
	}
}

func TestAuthenticateRejectsTooManyTokens(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := NewDatabaseAuthenticator(db)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "a,b,c,d,e,f,g,h")
	if _, err := a.Authenticate(r); err == nil || err.Error() != "too many authorization tokens" {
		t.Fatalf("got %v", err)
	}
}

func TestOAuth2CleanupStopsOnClose(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()
	a := NewDatabaseAuthenticator(db).WithOAuth2(OAuth2Config{ClientID: "x", ProviderName: "p"})
	p := a.oauth2Providers["p"]
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	_ = a.Close() // idempotent
	select {
	case <-p.stopCh:
	default:
		t.Fatal("stop channel not closed")
	}
}

func TestSplitTagDropsEmpty(t *testing.T) {
	got := splitTag("a,,b,c,", ',')
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestColumnSecurityPanicFailsClosed(t *testing.T) {
	type Rec struct {
		JSONCol string `json:"json_col" bun:"json_col"`
	}
	sl, _ := NewSecurityList(&slowProvider{})
	sl.ColumnSecurity["public.t@1"] = []ColumnSecurity{{
		Schema: "public", Tablename: "t", Path: []string{"JSONCol"}, Accesstype: "mask", UserID: 1,
	}}
	// A struct boxed in an interface is not addressable, so SetString panics.
	recs := []any{Rec{JSONCol: "secret"}}

	out, err := sl.ApplyColumnSecurity(reflect.ValueOf(recs), reflect.TypeOf(Rec{}), 1, "public", "t")
	if err == nil {
		t.Fatalf("panic must be returned as an error, got out=%v", out)
	}
	if errors.Is(err, ErrNoColumnSecurity) {
		t.Fatal("a panic must not look like 'no rules'")
	}
}

func TestNoRulesIsNotAnError(t *testing.T) {
	sl, _ := NewSecurityList(&slowProvider{})
	if _, err := sl.GetRowSecurityTemplate(1, "s", "t"); !errors.Is(err, ErrNoRowSecurity) {
		t.Fatalf("got %v", err)
	}
	if _, err := sl.ApplyColumnSecurity(reflect.ValueOf([]int{}), reflect.TypeOf(0), 1, "s", "t"); !errors.Is(err, ErrNoColumnSecurity) {
		t.Fatalf("got %v", err)
	}
}

func TestApplyColumnSecurityHookFailsClosedOnPanic(t *testing.T) {
	type Rec struct {
		JSONCol string `bun:"json_col"`
	}
	sl, _ := NewSecurityList(&slowProvider{})
	sl.ColumnSecurity["public.t@1"] = []ColumnSecurity{{
		Schema: "public", Tablename: "t", Path: []string{"JSONCol"}, Accesstype: "mask", UserID: 1,
	}}
	secCtx := &mockSecurityContext{
		ctx: context.Background(), userID: 1, hasUser: true, schema: "public", entity: "t",
		model: &Rec{}, result: []any{Rec{JSONCol: "secret"}},
	}
	if err := ApplyColumnSecurity(secCtx, sl); err == nil {
		t.Fatal("a panic during masking must fail the request, not return unmasked data")
	}
}
