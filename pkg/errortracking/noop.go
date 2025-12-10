package errortracking

import "context"

// NoOpProvider is a no-op implementation of the Provider interface
// Used when error tracking is disabled
type NoOpProvider struct{}

// NewNoOpProvider creates a new NoOp provider
func NewNoOpProvider() *NoOpProvider {
	return &NoOpProvider{}
}

// CaptureError does nothing
func (n *NoOpProvider) CaptureError(ctx context.Context, err error, severity Severity, extra map[string]interface{}) {
	// No-op
}

// CaptureMessage does nothing
func (n *NoOpProvider) CaptureMessage(ctx context.Context, message string, severity Severity, extra map[string]interface{}) {
	// No-op
}

// CapturePanic does nothing
func (n *NoOpProvider) CapturePanic(ctx context.Context, recovered interface{}, stackTrace []byte, extra map[string]interface{}) {
	// No-op
}

// Flush does nothing and returns true
func (n *NoOpProvider) Flush(timeout int) bool {
	return true
}

// Close does nothing
func (n *NoOpProvider) Close() error {
	return nil
}
