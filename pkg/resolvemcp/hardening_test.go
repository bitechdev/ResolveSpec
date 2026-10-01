package resolvemcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestHookRegistryConcurrentUse(t *testing.T) {
	r := NewHookRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); r.Register(BeforeRead, func(*HookContext) error { return nil }) }()
		go func() { defer wg.Done(); _ = r.Execute(BeforeRead, &HookContext{}) }()
		go func() { defer wg.Done(); _ = r.HasHooks(BeforeRead); r.Clear(AfterRead) }()
	}
	wg.Wait()
}

// Update and delete report a missing row with the same error, so ids cannot be probed.
func TestNotFoundErrorsAreUniform(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	empty := sqlmock.NewRows([]string{"id", "name"})
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(empty)
	mock.ExpectRollback()
	_, errU := h.executeUpdate(ctx, "public", "items", "9", map[string]interface{}{"name": "x"})
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	mock.ExpectRollback()
	_, errD := h.executeDelete(ctx, "public", "items", "9")
	if errU == nil || errD == nil || errU.Error() != errD.Error() {
		t.Fatalf("update %v / delete %v must be the same error", errU, errD)
	}
}

func TestSSEHostAllowlistAndPoolCap(t *testing.T) {
	h, _, _ := newTxHarness(t)
	h.config.AllowedHosts = []string{"mcp.example.com"}
	d := &dynamicSSEHandler{h: h}

	r := httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
	r.Host = "evil.example.net"
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("foreign host: status %d, want 400", w.Code)
	}

	r = httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
	r.Host = "mcp.example.com"
	r.Header.Set("X-Forwarded-Proto", "javascript")
	w = httptest.NewRecorder()
	d.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad proto: status %d, want 400", w.Code)
	}

	h.config.AllowedHosts = nil
	for i := 0; i < maxSSEPool+5; i++ {
		r = httptest.NewRequest(http.MethodPost, "/mcp/message?sessionId=x", nil)
		r.Host = fmt.Sprintf("h%d.example.com", i)
		d.ServeHTTP(httptest.NewRecorder(), r)
	}
	if len(d.pool) > maxSSEPool {
		t.Errorf("pool grew to %d, cap is %d", len(d.pool), maxSSEPool)
	}
	r = httptest.NewRequest(http.MethodPost, "/mcp/message", nil)
	r.Host = "one-more.example.com"
	w = httptest.NewRecorder()
	d.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("full pool: status %d, want 503", w.Code)
	}
}
