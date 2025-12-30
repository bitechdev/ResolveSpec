package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

// mockMetricsProvider is a mock for the metrics provider to check if methods are called.
type mockMetricsProvider struct {
	metrics.NoOpProvider // Embed NoOpProvider to avoid implementing all methods
	panicRecorded bool
	methodName    string
}

func (m *mockMetricsProvider) RecordPanic(methodName string) {
	m.panicRecorded = true
	m.methodName = methodName
}

func TestPanicRecovery(t *testing.T) {
	// Initialize a mock logger to avoid actual logging output during tests
	logger.Init(true)

	// Setup mock metrics provider
	mockProvider := &mockMetricsProvider{}
	originalProvider := metrics.GetProvider()
	metrics.SetProvider(mockProvider)
	defer metrics.SetProvider(originalProvider) // Restore original provider after test

	// 1. Test case: A handler that panics
	t.Run("recovers from panic and returns 500", func(t *testing.T) {
		// Reset mock state for this sub-test
		mockProvider.panicRecorded = false
		mockProvider.methodName = ""

		panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("something went terribly wrong")
		})

		// Create the middleware wrapping the panicking handler
		testHandler := PanicRecovery(panicHandler)

		// Create a test request and response recorder
		req := httptest.NewRequest("GET", "http://example.com/foo", nil)
		rr := httptest.NewRecorder()

		// Serve the request
		testHandler.ServeHTTP(rr, req)

		// Assertions
		assert.Equal(t, http.StatusInternalServerError, rr.Code, "expected status code to be 500")
		assert.Contains(t, rr.Body.String(), "panic in PanicMiddleware: something went terribly wrong", "expected error message in response body")

		// Assert that the metric was recorded
		assert.True(t, mockProvider.panicRecorded, "expected RecordPanic to be called on metrics provider")
		assert.Equal(t, panicMiddlewareMethodName, mockProvider.methodName, "expected panic to be recorded with the correct method name")
	})

	// 2. Test case: A handler that does NOT panic
	t.Run("does not interfere with a non-panicking handler", func(t *testing.T) {
		// Reset mock state for this sub-test
		mockProvider.panicRecorded = false

		successHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})

		testHandler := PanicRecovery(successHandler)

		req := httptest.NewRequest("GET", "http://example.com/foo", nil)
		rr := httptest.NewRecorder()

		testHandler.ServeHTTP(rr, req)

		// Assertions
		assert.Equal(t, http.StatusOK, rr.Code, "expected status code to be 200")
		assert.Equal(t, "OK", rr.Body.String(), "expected 'OK' response body")
		assert.False(t, mockProvider.panicRecorded, "expected RecordPanic to not be called when there is no panic")
	})
}
