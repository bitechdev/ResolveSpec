package logger

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	errortracking "github.com/bitechdev/ResolveSpec/pkg/errortracking"
)

// Logger is the active logger. It is kept exported for compatibility, but
// inside this package it must only be accessed through getLogger/setLogger.
var Logger *zap.SugaredLogger
var errorTracker errortracking.Provider

// stateMu guards Logger and errorTracker, which may be replaced while other
// goroutines are logging.
var stateMu sync.RWMutex

func getLogger() *zap.SugaredLogger {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return Logger
}

func swapLogger(l *zap.SugaredLogger) *zap.SugaredLogger {
	stateMu.Lock()
	defer stateMu.Unlock()
	old := Logger
	Logger = l
	return old
}

func getErrorTracker() errortracking.Provider {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return errorTracker
}

// pid is constant for the life of the process; cache it off the hot path.
var pid = os.Getpid()

// maxStackBytes caps the stack trace captured for a recovered panic.
const maxStackBytes = 16 << 10

func captureStack() []byte {
	buf := make([]byte, maxStackBytes)
	return buf[:runtime.Stack(buf, false)]
}

// Patterns scrubbed from messages before they leave the process for the
// error tracker. The local log is left untouched.
var redactPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/@:]+:[^\s/@]+@`), "${1}[REDACTED]@"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key)(\s*[=:]\s*)('[^']*'|"[^"]*"|[^\s,;&]+)`), "${1}${2}[REDACTED]"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=\-]+`), "${1} [REDACTED]"},
}

func redact(s string) string {
	for _, p := range redactPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// sanitizeForStdlog escapes CR/LF and other control characters so untrusted
// values cannot forge additional log lines on the stdlib fallback path.
func sanitizeForStdlog(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Error tracker fan-out limiting: a global token bucket plus per-template
// dedup, so attacker-triggerable errors cannot burn quota or flood the queue.
const (
	trackerBurst        = 50
	trackerRefillPerSec = 20.0
	trackerDedupWindow  = time.Second
	trackerMaxKeys      = 1024
)

var limiter = struct {
	sync.Mutex
	tokens float64
	last   time.Time
	seen   map[string]time.Time
}{tokens: trackerBurst, seen: map[string]time.Time{}}

func allowTracker(key string) bool {
	now := time.Now()
	limiter.Lock()
	defer limiter.Unlock()

	if !limiter.last.IsZero() {
		limiter.tokens += now.Sub(limiter.last).Seconds() * trackerRefillPerSec
		if limiter.tokens > trackerBurst {
			limiter.tokens = trackerBurst
		}
	}
	limiter.last = now

	if t, ok := limiter.seen[key]; ok && now.Sub(t) < trackerDedupWindow {
		return false
	}
	if limiter.tokens < 1 {
		return false
	}
	if len(limiter.seen) >= trackerMaxKeys {
		limiter.seen = map[string]time.Time{}
	}
	limiter.seen[key] = now
	limiter.tokens--
	return true
}

func Init(dev bool) {

	if dev {
		cfg := zap.NewDevelopmentConfig()
		UpdateLogger(&cfg)
	} else {
		cfg := zap.NewProductionConfig()
		UpdateLogger(&cfg)
	}

}

func UpdateLoggerPath(path string, dev bool) {
	defaultConfig := zap.NewProductionConfig()
	if dev {
		defaultConfig = zap.NewDevelopmentConfig()
	}
	defaultConfig.OutputPaths = []string{path}
	UpdateLogger(&defaultConfig)
}

// UpdateLogger rebuilds the logger from config. On failure the previous logger
// stays in place; use UpdateLoggerE to get the error.
func UpdateLogger(config *zap.Config) {
	if err := UpdateLoggerE(config); err != nil {
		log.Printf("logger: failed to build logger, keeping previous: %s", sanitizeForStdlog(err.Error()))
	}
}

// UpdateLoggerE is UpdateLogger but returns the build error.
func UpdateLoggerE(config *zap.Config) error {
	defaultConfig := zap.NewProductionConfig()
	defaultConfig.OutputPaths = []string{"resolvespec.log"}
	if config == nil {
		config = &defaultConfig
	}

	logger, err := config.Build()
	if err != nil {
		return err
	}

	old := swapLogger(logger.Sugar())
	if old != nil {
		_ = old.Sync()
	}
	Info("ResolveSpec Logger initialized")
	return nil
}

// Sync flushes buffered log entries. Call it on shutdown.
func Sync() error {
	if lg := getLogger(); lg != nil {
		return lg.Sync()
	}
	return nil
}

// InitErrorTracking initializes the error tracking provider
func InitErrorTracking(provider errortracking.Provider) {
	stateMu.Lock()
	errorTracker = provider
	stateMu.Unlock()
	if provider != nil {
		Info("Error tracking initialized")
	}
}

// GetErrorTracker returns the current error tracking provider
func GetErrorTracker() errortracking.Provider {
	return getErrorTracker()
}

// CloseErrorTracking flushes and closes the error tracking provider
func CloseErrorTracking() error {
	if tracker := getErrorTracker(); tracker != nil {
		tracker.Flush(5)
		return tracker.Close()
	}
	return nil
}

// extractContext attempts to find a context.Context in the given arguments.
// It returns the found context (or context.Background() if not found) and
// the remaining arguments without the context.
func extractContext(args ...interface{}) (ctx context.Context, filteredArgs []interface{}) {
	ctx = context.Background()
	var newArgs []interface{}
	found := false

	for _, arg := range args {
		if c, ok := arg.(context.Context); ok {
			if !found {
				ctx = c
				found = true
			}
			// Ignore any additional context.Context arguments after the first one.
			continue
		}
		newArgs = append(newArgs, arg)
	}
	return ctx, newArgs
}

func Info(template string, args ...interface{}) {
	_, args = extractContext(args...)
	message := fmt.Sprintf(template, args...)
	if lg := getLogger(); lg != nil {
		lg.Infow(message, "process_id", pid)
		return
	}
	log.Printf("%s", sanitizeForStdlog(message))
}

func Debug(template string, args ...interface{}) {
	_, args = extractContext(args...)
	message := fmt.Sprintf(template, args...)
	if lg := getLogger(); lg != nil {
		lg.Debugw(message, "process_id", pid)
		return
	}
	log.Printf("%s", sanitizeForStdlog(message))
}

func Warn(template string, args ...interface{}) {
	logAndTrack(errortracking.SeverityWarning, template, args)
}

func Error(template string, args ...interface{}) {
	logAndTrack(errortracking.SeverityError, template, args)
}

func logAndTrack(sev errortracking.Severity, template string, args []interface{}) {
	ctx, remainingArgs := extractContext(args...)
	message := fmt.Sprintf(template, remainingArgs...)
	if lg := getLogger(); lg == nil {
		log.Printf("%s", sanitizeForStdlog(message))
	} else if sev == errortracking.SeverityWarning {
		lg.Warnw(message, "process_id", pid)
	} else {
		lg.Errorw(message, "process_id", pid)
	}

	tracker := getErrorTracker()
	if tracker == nil || !allowTracker(string(sev)+"|"+template) {
		return
	}
	tracker.CaptureMessage(ctx, redact(message), sev, map[string]interface{}{
		"process_id": pid,
	})
}

// CatchPanic - Handle panic
// Returns a function that should be deferred to catch panics
// Example usage: defer CatchPanicCallback("MyFunction", func(err any) { /* cleanup */ })()
func CatchPanicCallback(location string, cb func(err any), args ...interface{}) func() {
	ctx, _ := extractContext(args...)
	return func() {
		if err := recover(); err != nil {
			callstack := captureStack()
			lg := getLogger()
			tracker := getErrorTracker()

			if lg != nil {
				Error("Panic in %s : %v", location, err, ctx) // Pass context implicitly
			} else {
				fmt.Printf("%s:PANIC->%+v", location, err)
				debug.PrintStack()
			}

			// Send to error tracker
			if tracker != nil {
				tracker.CapturePanic(ctx, err, callstack, map[string]interface{}{
					"location":   location,
					"process_id": pid,
				})
			}

			if cb != nil {
				cb(err)
			}
		}
	}
}

// CatchPanic - Handle panic
// Returns a function that should be deferred to catch panics
// Example usage: defer CatchPanic("MyFunction")()
func CatchPanic(location string, args ...interface{}) func() {
	return CatchPanicCallback(location, nil, args...)
}

// CatchPanicRethrow returns a function to defer that logs and reports a panic
// and then re-panics. Use it inside enforcement/internal code where swallowing
// the panic would let the caller proceed as if the work had succeeded. The
// swallowing CatchPanic is for outermost request/goroutine boundaries only.
func CatchPanicRethrow(location string, args ...interface{}) func() {
	return func() {
		if r := recover(); r != nil {
			_ = HandlePanic(location, r, args...)
			panic(r)
		}
	}
}

// HandlePanic logs a panic and returns it as an error
// This should be called with the result of recover() from a deferred function
// Example usage:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        err = logger.HandlePanic("MethodName", r)
//	    }
//	}()
func HandlePanic(methodName string, r any, args ...interface{}) error {
	tracker := getErrorTracker()
	ctx, _ := extractContext(args...)
	stack := captureStack()
	Error("Panic in %s: %v\nStack trace:\n%s", methodName, r, string(stack), ctx) // Pass context implicitly

	// Send to error tracker
	if tracker != nil {
		tracker.CapturePanic(ctx, r, stack, map[string]interface{}{
			"method":     methodName,
			"process_id": pid,
		})
	}

	return fmt.Errorf("panic in %s: %v", methodName, r)
}
