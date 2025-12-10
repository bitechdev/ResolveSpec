package errortracking

import (
	"context"
	"errors"
	"testing"
)

func TestNoOpProvider(t *testing.T) {
	provider := NewNoOpProvider()

	// Test that all methods can be called without panicking
	t.Run("CaptureError", func(t *testing.T) {
		provider.CaptureError(context.Background(), errors.New("test error"), SeverityError, nil)
	})

	t.Run("CaptureMessage", func(t *testing.T) {
		provider.CaptureMessage(context.Background(), "test message", SeverityWarning, nil)
	})

	t.Run("CapturePanic", func(t *testing.T) {
		provider.CapturePanic(context.Background(), "panic!", []byte("stack trace"), nil)
	})

	t.Run("Flush", func(t *testing.T) {
		result := provider.Flush(5)
		if !result {
			t.Error("Expected Flush to return true")
		}
	})

	t.Run("Close", func(t *testing.T) {
		err := provider.Close()
		if err != nil {
			t.Errorf("Expected Close to return nil, got %v", err)
		}
	})
}

func TestSeverityLevels(t *testing.T) {
	tests := []struct {
		name     string
		severity Severity
		expected string
	}{
		{"Error", SeverityError, "error"},
		{"Warning", SeverityWarning, "warning"},
		{"Info", SeverityInfo, "info"},
		{"Debug", SeverityDebug, "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.severity) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.severity))
			}
		})
	}
}

func TestProviderInterface(t *testing.T) {
	// Test that NoOpProvider implements Provider interface
	var _ Provider = (*NoOpProvider)(nil)

	// Test that SentryProvider implements Provider interface
	var _ Provider = (*SentryProvider)(nil)
}
