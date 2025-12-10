package errortracking

import (
	"context"
)

// Severity represents the severity level of an error
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
	SeverityDebug   Severity = "debug"
)

// Provider defines the interface for error tracking providers
type Provider interface {
	// CaptureError captures an error with the given severity and additional context
	CaptureError(ctx context.Context, err error, severity Severity, extra map[string]interface{})

	// CaptureMessage captures a message with the given severity and additional context
	CaptureMessage(ctx context.Context, message string, severity Severity, extra map[string]interface{})

	// CapturePanic captures a panic with stack trace
	CapturePanic(ctx context.Context, recovered interface{}, stackTrace []byte, extra map[string]interface{})

	// Flush waits for all events to be sent (useful for graceful shutdown)
	Flush(timeout int) bool

	// Close closes the provider and releases resources
	Close() error
}
