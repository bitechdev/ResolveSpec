package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestGracefulServerTrackRequests(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}),
	})

	handler := srv.TrackRequestsMiddleware(srv.server.Handler)

	// Start some requests
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()
	}

	// Wait a bit for requests to start
	time.Sleep(10 * time.Millisecond)

	// Check in-flight count
	inFlight := srv.InFlightRequests()
	if inFlight == 0 {
		t.Error("Should have in-flight requests")
	}

	// Wait for all requests to complete
	wg.Wait()

	// Check that counter is back to zero
	inFlight = srv.InFlightRequests()
	if inFlight != 0 {
		t.Errorf("In-flight requests should be 0, got %d", inFlight)
	}
}

func TestGracefulServerRejectsRequestsDuringShutdown(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})

	handler := srv.TrackRequestsMiddleware(srv.server.Handler)

	// Mark as shutting down
	srv.isShuttingDown.Store(true)

	// Try to make a request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should get 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestHealthCheckHandler(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr:    ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})

	handler := srv.HealthCheckHandler()

	// Healthy
	t.Run("Healthy", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}

		if w.Body.String() != `{"status":"healthy"}` {
			t.Errorf("Unexpected body: %s", w.Body.String())
		}
	})

	// Shutting down
	t.Run("ShuttingDown", func(t *testing.T) {
		srv.isShuttingDown.Store(true)

		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected 503, got %d", w.Code)
		}
	})
}

func TestReadinessHandler(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr:    ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})

	handler := srv.ReadinessHandler()

	// Ready with no in-flight requests
	t.Run("Ready", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}

		body := w.Body.String()
		if body != `{"ready":true,"in_flight_requests":0}` {
			t.Errorf("Unexpected body: %s", body)
		}
	})

	// Not ready during shutdown
	t.Run("NotReady", func(t *testing.T) {
		srv.isShuttingDown.Store(true)

		req := httptest.NewRequest("GET", "/ready", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected 503, got %d", w.Code)
		}
	})
}

func TestShutdownCallbacks(t *testing.T) {
	callbackExecuted := false

	RegisterShutdownCallback(func(ctx context.Context) error {
		callbackExecuted = true
		return nil
	})

	ctx := context.Background()
	err := executeShutdownCallbacks(ctx)

	if err != nil {
		t.Errorf("executeShutdownCallbacks() error = %v", err)
	}

	if !callbackExecuted {
		t.Error("Shutdown callback was not executed")
	}

	// Reset for other tests
	shutdownCallbacks = nil
}

func TestDrainRequests(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr:         ":0",
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		DrainTimeout: 1 * time.Second,
	})

	// Simulate in-flight requests
	srv.inFlightRequests.Add(3)

	// Start draining in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		// Simulate requests completing
		srv.inFlightRequests.Add(-3)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.drainRequests(ctx)
	if err != nil {
		t.Errorf("drainRequests() error = %v", err)
	}

	if srv.InFlightRequests() != 0 {
		t.Errorf("In-flight requests should be 0, got %d", srv.InFlightRequests())
	}
}

func TestDrainRequestsTimeout(t *testing.T) {
	srv := NewGracefulServer(Config{
		Addr:         ":0",
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		DrainTimeout: 100 * time.Millisecond,
	})

	// Simulate in-flight requests that don't complete
	srv.inFlightRequests.Add(5)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := srv.drainRequests(ctx)
	if err == nil {
		t.Error("drainRequests() should timeout with error")
	}

	// Cleanup
	srv.inFlightRequests.Add(-5)
}

func TestGetClientIP(t *testing.T) {
	// This test is in ratelimit_test.go since getClientIP is used by rate limiter
	// Including here for completeness of server tests
}
