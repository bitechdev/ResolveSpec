package errortracking

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

// SentryProvider implements the Provider interface using Sentry
type SentryProvider struct {
	hub *sentry.Hub
}

// SentryConfig holds the configuration for Sentry
type SentryConfig struct {
	DSN              string
	Environment      string
	Release          string
	Debug            bool
	SampleRate       float64
	TracesSampleRate float64
}

// NewSentryProvider creates a new Sentry provider
func NewSentryProvider(config SentryConfig) (*SentryProvider, error) {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              config.DSN,
		Environment:      config.Environment,
		Release:          config.Release,
		Debug:            config.Debug,
		AttachStacktrace: true,
		SampleRate:       config.SampleRate,
		TracesSampleRate: config.TracesSampleRate,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Sentry: %w", err)
	}

	return &SentryProvider{
		hub: sentry.CurrentHub(),
	}, nil
}

// CaptureError captures an error with the given severity and additional context
func (s *SentryProvider) CaptureError(ctx context.Context, err error, severity Severity, extra map[string]interface{}) {
	if err == nil {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = s.hub
	}

	event := sentry.NewEvent()
	event.Level = s.convertSeverity(severity)
	event.Message = err.Error()
	event.Exception = []sentry.Exception{
		{
			Value:      err.Error(),
			Type:       fmt.Sprintf("%T", err),
			Stacktrace: sentry.ExtractStacktrace(err),
		},
	}

	if extra != nil {
		event.Extra = extra
	}

	hub.CaptureEvent(event)
}

// CaptureMessage captures a message with the given severity and additional context
func (s *SentryProvider) CaptureMessage(ctx context.Context, message string, severity Severity, extra map[string]interface{}) {
	if message == "" {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = s.hub
	}

	event := sentry.NewEvent()
	event.Level = s.convertSeverity(severity)
	event.Message = message

	if extra != nil {
		event.Extra = extra
	}

	hub.CaptureEvent(event)
}

// CapturePanic captures a panic with stack trace
func (s *SentryProvider) CapturePanic(ctx context.Context, recovered interface{}, stackTrace []byte, extra map[string]interface{}) {
	if recovered == nil {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = s.hub
	}

	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Message = fmt.Sprintf("Panic: %v", recovered)
	event.Exception = []sentry.Exception{
		{
			Value: fmt.Sprintf("%v", recovered),
			Type:  "panic",
		},
	}

	if extra != nil {
		event.Extra = extra
	}

	if stackTrace != nil {
		event.Extra["stack_trace"] = string(stackTrace)
	}

	hub.CaptureEvent(event)
}

// Flush waits for all events to be sent (useful for graceful shutdown)
func (s *SentryProvider) Flush(timeout int) bool {
	return sentry.Flush(time.Duration(timeout) * time.Second)
}

// Close closes the provider and releases resources
func (s *SentryProvider) Close() error {
	sentry.Flush(2 * time.Second)
	return nil
}

// convertSeverity converts our Severity to Sentry's Level
func (s *SentryProvider) convertSeverity(severity Severity) sentry.Level {
	switch severity {
	case SeverityError:
		return sentry.LevelError
	case SeverityWarning:
		return sentry.LevelWarning
	case SeverityInfo:
		return sentry.LevelInfo
	case SeverityDebug:
		return sentry.LevelDebug
	default:
		return sentry.LevelError
	}
}
