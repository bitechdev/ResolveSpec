package aiproxy

import (
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/metrics"
)

// Outcome classifies a handled request.
type Outcome string

const (
	OutcomeOK            Outcome = "ok"             // upstream answered < 400
	OutcomeUpstreamError Outcome = "upstream_error" // upstream answered >= 400 or failed
	OutcomeDenied        Outcome = "denied"         // refused by auth, roles, allowlist, size or a hook
	OutcomeRateLimited   Outcome = "rate_limited"
)

// AuditRecord is written once per handled request. It holds no bodies and no keys.
type AuditRecord struct {
	Time     time.Time
	UserID   int
	User     string
	RemoteID string
	Upstream string
	Kind     Kind
	Method   string
	Path     string // client path, no query string
	Model    string
	Tools    []string
	Status   int
	Outcome  Outcome
	Reason   string // why a request was refused
	Duration time.Duration
	Usage    Usage
	Error    string
}

// AuditSink receives audit records. It runs on the request path, so keep it fast
// (queue slow work such as database writes).
type AuditSink interface {
	Record(AuditRecord)
}

// LogAuditSink writes records through pkg/logger. It is the default sink.
type LogAuditSink struct{}

// Record implements AuditSink.
func (LogAuditSink) Record(r AuditRecord) {
	logger.Info("aiproxy audit: user=%s(%d) upstream=%s kind=%s %s %s model=%q tools=%v status=%d outcome=%s reason=%q duration=%s tokens=%d/%d/%d err=%q",
		r.User, r.UserID, r.Upstream, r.Kind, r.Method, r.Path, r.Model, r.Tools, r.Status, r.Outcome, r.Reason, r.Duration,
		r.Usage.PromptTokens, r.Usage.CompletionTokens, r.Usage.TotalTokens, r.Error)
}

// NopAuditSink discards records (use it to switch the default logging off).
type NopAuditSink struct{}

// Record implements AuditSink.
func (NopAuditSink) Record(AuditRecord) {}

// maxModelLabels bounds the distinct (upstream, model) label pairs, since the
// model comes from the client.
const maxModelLabels = 400

type modelLabels struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (m *modelLabels) label(upstream, model string) string {
	if model == "" {
		return ""
	}
	key := upstream + "|" + model
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.seen[key]; ok {
		return model
	}
	if m.seen == nil {
		m.seen = make(map[string]struct{})
	}
	if len(m.seen) >= maxModelLabels {
		return "other"
	}
	m.seen[key] = struct{}{}
	return model
}

// record emits the audit record and the metrics for a finished or refused request.
func (p *Proxy) record(t *target, hc *HookContext, status int, outcome Outcome, reason string, err error) {
	rec := AuditRecord{
		Time:     time.Now(),
		Upstream: hc.Upstream,
		Kind:     hc.Kind,
		Model:    hc.Model,
		Tools:    hc.Tools,
		Status:   status,
		Outcome:  outcome,
		Reason:   reason,
		Duration: hc.Duration,
		Usage:    hc.Usage,
	}
	if hc.Request != nil {
		rec.Method = hc.Request.Method
		rec.Path = hc.Request.URL.Path
	}
	if u := hc.UserContext; u != nil {
		rec.UserID, rec.User, rec.RemoteID = u.UserID, u.UserName, u.RemoteID
	}
	if err != nil {
		rec.Error = err.Error()
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("aiproxy: audit sink panic: %v", r)
			}
		}()
		p.cfg.Audit.Record(rec)
	}()

	if m, ok := metrics.GetProvider().(metrics.AIProxyRecorder); ok && m != nil {
		model := ""
		if outcome == OutcomeOK || outcome == OutcomeUpstreamError {
			model = p.labels.label(hc.Upstream, hc.Model)
		}
		m.RecordAIProxy(hc.Upstream, string(hc.Kind), model, statusClass(status), string(outcome), hc.Duration,
			hc.Usage.PromptTokens, hc.Usage.CompletionTokens)
	}
}

// statusClass maps a status code to a low-cardinality label (2xx, 4xx, ...).
func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "unknown"
	}
	return string(rune('0'+status/100)) + "xx"
}
