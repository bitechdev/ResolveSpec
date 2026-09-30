package logger

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	errortracking "github.com/bitechdev/ResolveSpec/pkg/errortracking"
)

type fakeTracker struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeTracker) CaptureError(context.Context, error, errortracking.Severity, map[string]interface{}) {
}
func (f *fakeTracker) CaptureMessage(_ context.Context, m string, _ errortracking.Severity, _ map[string]interface{}) {
	f.mu.Lock()
	f.msgs = append(f.msgs, m)
	f.mu.Unlock()
}
func (f *fakeTracker) CapturePanic(context.Context, interface{}, []byte, map[string]interface{}) {}
func (f *fakeTracker) Flush(int) bool                                                            { return true }
func (f *fakeTracker) Close() error                                                              { return nil }

func TestStdlibFallbackSafe(t *testing.T) {
	swapLogger(nil)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	Info("100%s done\nFAKE line", "")
	Info("%d%% %s", 5, context.Background())
	out := buf.String()
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("log injection: %q", out)
	}
}

func TestContextStrippedFromInfo(t *testing.T) {
	swapLogger(nil)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)
	Info("saved %s", "widget", context.WithValue(context.Background(), "k", "secret"))
	if strings.Contains(buf.String(), "EXTRA") || strings.Contains(buf.String(), "secret") {
		t.Fatalf("context leaked: %q", buf.String())
	}
}

func TestRedact(t *testing.T) {
	in := "connect postgres://admin:hunter2@db:5432/x failed password=abc123 Authorization: Bearer eyJ.abc-d"
	out := redact(in)
	for _, leak := range []string{"hunter2", "abc123", "eyJ.abc-d"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked in %q", leak, out)
		}
	}
}

func TestTrackerRateLimitAndRedaction(t *testing.T) {
	swapLogger(zap.NewNop().Sugar())
	ft := &fakeTracker{}
	InitErrorTracking(ft)
	defer InitErrorTracking(nil)

	for i := 0; i < 100; i++ {
		Error("same template %d password=x", i)
	}
	if len(ft.msgs) != 1 {
		t.Fatalf("dedup failed: %d events", len(ft.msgs))
	}
	if strings.Contains(ft.msgs[0], "password=x") {
		t.Fatalf("not redacted: %q", ft.msgs[0])
	}
}

func TestConcurrentUpdateAndLog(t *testing.T) {
	InitErrorTracking(&fakeTracker{})
	defer InitErrorTracking(nil)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cfg := zap.NewProductionConfig()
				cfg.OutputPaths = []string{"stderr"}
				_ = UpdateLoggerE(&cfg)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Error("e %d", j)
				_ = CloseErrorTracking()
			}
		}()
	}
	wg.Wait()
	_ = Sync()
}

func TestCatchPanicRethrow(t *testing.T) {
	swapLogger(zap.NewNop().Sugar())
	defer func() {
		if recover() == nil {
			t.Fatal("expected re-panic")
		}
	}()
	func() {
		defer CatchPanicRethrow("x")()
		panic("boom")
	}()
}

func TestCatchPanicSwallows(t *testing.T) {
	swapLogger(zap.NewNop().Sugar())
	func() {
		defer CatchPanic("x")()
		panic("boom")
	}()
}
