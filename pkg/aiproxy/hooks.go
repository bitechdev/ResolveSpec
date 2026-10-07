package aiproxy

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// HookType defines when a hook runs.
type HookType string

const (
	// BeforeProxy runs after auth, rate limit and request inspection, before the upstream call.
	BeforeProxy HookType = "before_proxy"
	// AfterProxy runs once the response finished (or failed). Errors are logged only.
	AfterProxy HookType = "after_proxy"
)

// Usage is the token usage reported by an OpenAI upstream.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// HookContext is passed to hooks.
type HookContext struct {
	Context     context.Context
	Request     *http.Request // BeforeProxy may modify headers/query; the key is injected later
	UserContext *security.UserContext

	Upstream string
	Kind     Kind
	Model    string   // OpenAI: requested model
	Tools    []string // MCP: tools named by tools/call

	// AfterProxy only
	StatusCode int
	Duration   time.Duration
	Usage      Usage // OpenAI only; needs stream_options.include_usage for streams
	Error      error

	// BeforeProxy only
	Abort        bool
	AbortMessage string
	AbortCode    int // default 403
}

// HookFunc is a hook. A returned error aborts a BeforeProxy request.
type HookFunc func(*HookContext) error

// HookRegistry holds hooks per type.
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[HookType][]HookFunc
}

// NewHookRegistry creates an empty registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{hooks: make(map[HookType][]HookFunc)}
}

// Register adds a hook.
func (r *HookRegistry) Register(t HookType, h HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[t] = append(r.hooks[t], h)
}

// Execute runs the hooks in order and stops at the first error or abort.
func (r *HookRegistry) Execute(t HookType, ctx *HookContext) error {
	r.mu.RLock()
	list := append([]HookFunc(nil), r.hooks[t]...)
	r.mu.RUnlock()

	for i, h := range list {
		if err := h(ctx); err != nil {
			return fmt.Errorf("aiproxy hook %d for %s failed: %w", i+1, t, err)
		}
		if ctx.Abort {
			return fmt.Errorf("aborted by hook: %s", ctx.AbortMessage)
		}
	}
	return nil
}

func (r *HookRegistry) executeAfter(ctx *HookContext) {
	if err := r.Execute(AfterProxy, ctx); err != nil {
		logger.Warn("aiproxy: %v", err)
	}
}
