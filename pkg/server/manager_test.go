package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort(t *testing.T) int {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.NoError(t, err)

	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestServerManagerLifecycle(t *testing.T) {
	// Initialize logger for test output
	logger.Init(true)

	// Create a new server manager
	sm := NewManager()

	// Define a simple test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	// Get a free port for the server to listen on to avoid conflicts
	testPort := getFreePort(t)

	// Add a new server configuration
	serverConfig := Config{
		Name:    "TestServer",
		Host:    "localhost",
		Port:    testPort,
		Handler: testHandler,
	}
	instance, err := sm.Add(serverConfig)
	require.NoError(t, err, "should be able to add a new server")
	require.NotNil(t, instance, "added instance should not be nil")

	// --- Test StartAll ---
	err = sm.StartAll()
	require.NoError(t, err, "StartAll should not return an error")

	// Give the server a moment to start up
	time.Sleep(100 * time.Millisecond)

	// --- Verify Server is Running ---
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://localhost:%d", testPort)
	resp, err := client.Get(url)
	require.NoError(t, err, "should be able to make a request to the running server")

	assert.Equal(t, http.StatusOK, resp.StatusCode, "expected status OK from the test server")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, "Hello, World!", string(body), "response body should match expected value")

	// --- Test Get ---
	retrievedInstance, err := sm.Get("TestServer")
	require.NoError(t, err, "should be able to get server by name")
	assert.Equal(t, instance.Addr(), retrievedInstance.Addr(), "retrieved instance should be the same")

	// --- Test List ---
	instanceList := sm.List()
	require.Len(t, instanceList, 1, "list should contain one instance")
	assert.Equal(t, instance.Addr(), instanceList[0].Addr(), "listed instance should be the same")

	// --- Test StopAll ---
	err = sm.StopAll()
	require.NoError(t, err, "StopAll should not return an error")

	// Give the server a moment to shut down
	time.Sleep(100 * time.Millisecond)

	// --- Verify Server is Stopped ---
	_, err = client.Get(url)
	require.Error(t, err, "should not be able to make a request to a stopped server")

	// --- Test Remove ---
	err = sm.Remove("TestServer")
	require.NoError(t, err, "should be able to remove a server")

	_, err = sm.Get("TestServer")
	require.Error(t, err, "should not be able to get a removed server")
}

func TestManagerErrorCases(t *testing.T) {
	logger.Init(true)
	sm := NewManager()
	testPort := getFreePort(t)

	// --- Test Add Duplicate Name ---
	config1 := Config{Name: "Duplicate", Host: "localhost", Port: testPort, Handler: http.NewServeMux()}
	_, err := sm.Add(config1)
	require.NoError(t, err)

	config2 := Config{Name: "Duplicate", Host: "localhost", Port: getFreePort(t), Handler: http.NewServeMux()}
	_, err = sm.Add(config2)
	require.Error(t, err, "should not be able to add a server with a duplicate name")

	// --- Test Get Non-existent ---
	_, err = sm.Get("NonExistent")
	require.Error(t, err, "should get an error for a non-existent server")

	// --- Test Add with Nil Handler ---
	config3 := Config{Name: "NilHandler", Host: "localhost", Port: getFreePort(t), Handler: nil}
	_, err = sm.Add(config3)
	require.Error(t, err, "should not be able to add a server with a nil handler")
}

func TestGracefulShutdown(t *testing.T) {
	logger.Init(true)
	sm := NewManager()

	requestsHandled := 0
	var requestsMu sync.Mutex

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requestsHandled++
		requestsMu.Unlock()
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	testPort := getFreePort(t)
	instance, err := sm.Add(Config{
		Name:         "TestServer",
		Host:         "localhost",
		Port:         testPort,
		Handler:      handler,
		DrainTimeout: 2 * time.Second,
	})
	require.NoError(t, err)

	err = sm.StartAll()
	require.NoError(t, err)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Send some concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			url := fmt.Sprintf("http://localhost:%d", testPort)
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
			}
		}()
	}

	// Wait a bit for requests to start
	time.Sleep(50 * time.Millisecond)

	// Check in-flight requests
	inFlight := instance.InFlightRequests()
	assert.Greater(t, inFlight, int64(0), "Should have in-flight requests")

	// Stop the server
	err = sm.StopAll()
	require.NoError(t, err)

	// Wait for all requests to complete
	wg.Wait()

	// Verify all requests were handled
	requestsMu.Lock()
	handled := requestsHandled
	requestsMu.Unlock()
	assert.GreaterOrEqual(t, handled, 1, "At least some requests should have been handled")

	// Verify no in-flight requests
	assert.Equal(t, int64(0), instance.InFlightRequests(), "Should have no in-flight requests after shutdown")
}

func TestHealthAndReadinessEndpoints(t *testing.T) {
	logger.Init(true)
	sm := NewManager()

	mux := http.NewServeMux()
	testPort := getFreePort(t)

	instance, err := sm.Add(Config{
		Name:    "TestServer",
		Host:    "localhost",
		Port:    testPort,
		Handler: mux,
	})
	require.NoError(t, err)

	// Add health and readiness endpoints
	mux.HandleFunc("/health", instance.HealthCheckHandler())
	mux.HandleFunc("/ready", instance.ReadinessHandler())

	err = sm.StartAll()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Timeout: 2 * time.Second}
	baseURL := fmt.Sprintf("http://localhost:%d", testPort)

	// Test health endpoint
	resp, err := client.Get(baseURL + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Contains(t, string(body), "healthy")

	// Test readiness endpoint
	resp, err = client.Get(baseURL + "/ready")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Contains(t, string(body), "ready")
	assert.Contains(t, string(body), "in_flight_requests")

	// Stop the server
	sm.StopAll()
}

func TestRequestRejectionDuringShutdown(t *testing.T) {
	logger.Init(true)
	sm := NewManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	testPort := getFreePort(t)
	_, err := sm.Add(Config{
		Name:         "TestServer",
		Host:         "localhost",
		Port:         testPort,
		Handler:      handler,
		DrainTimeout: 1 * time.Second,
	})
	require.NoError(t, err)

	err = sm.StartAll()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Start shutdown in background
	go func() {
		time.Sleep(50 * time.Millisecond)
		sm.StopAll()
	}()

	// Give shutdown time to start
	time.Sleep(100 * time.Millisecond)

	// Try to make a request after shutdown started
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://localhost:%d", testPort)
	resp, err := client.Get(url)

	// The request should either fail (connection refused) or get 503
	if err == nil {
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "Should get 503 during shutdown")
		resp.Body.Close()
	}
}

func TestShutdownCallbacks(t *testing.T) {
	logger.Init(true)
	sm := NewManager()

	callbackExecuted := false
	var callbackMu sync.Mutex

	sm.RegisterShutdownCallback(func(ctx context.Context) error {
		callbackMu.Lock()
		callbackExecuted = true
		callbackMu.Unlock()
		return nil
	})

	testPort := getFreePort(t)
	_, err := sm.Add(Config{
		Name:    "TestServer",
		Host:    "localhost",
		Port:    testPort,
		Handler: http.NewServeMux(),
	})
	require.NoError(t, err)

	err = sm.StartAll()
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = sm.StopAll()
	require.NoError(t, err)

	callbackMu.Lock()
	executed := callbackExecuted
	callbackMu.Unlock()

	assert.True(t, executed, "Shutdown callback should have been executed")
}

func TestSelfSignedSSLCertificateReuse(t *testing.T) {
	logger.Init(true)
	
	// Get expected cert directory location
	cacheDir, err := os.UserCacheDir()
	require.NoError(t, err)
	certDir := filepath.Join(cacheDir, "resolvespec", "certs")
	
	host := "localhost"
	certFile := filepath.Join(certDir, fmt.Sprintf("%s-cert.pem", host))
	keyFile := filepath.Join(certDir, fmt.Sprintf("%s-key.pem", host))
	
	// Clean up any existing cert files from previous tests
	os.Remove(certFile)
	os.Remove(keyFile)
	
	// First server creation - should generate new certificates
	sm1 := NewManager()
	testPort1 := getFreePort(t)
	_, err = sm1.Add(Config{
		Name:           "SSLTestServer1",
		Host:           host,
		Port:           testPort1,
		Handler:        http.NewServeMux(),
		SelfSignedSSL:  true,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	
	// Verify certificates were created
	_, err = os.Stat(certFile)
	require.NoError(t, err, "certificate file should exist after first creation")
	_, err = os.Stat(keyFile)
	require.NoError(t, err, "key file should exist after first creation")
	
	// Get modification time of cert file
	info1, err := os.Stat(certFile)
	require.NoError(t, err)
	modTime1 := info1.ModTime()
	
	// Wait a bit to ensure different modification times
	time.Sleep(100 * time.Millisecond)
	
	// Second server creation - should reuse existing certificates
	sm2 := NewManager()
	testPort2 := getFreePort(t)
	_, err = sm2.Add(Config{
		Name:           "SSLTestServer2",
		Host:           host,
		Port:           testPort2,
		Handler:        http.NewServeMux(),
		SelfSignedSSL:  true,
		ShutdownTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	
	// Get modification time of cert file after second creation
	info2, err := os.Stat(certFile)
	require.NoError(t, err)
	modTime2 := info2.ModTime()
	
	// Verify the certificate was reused (same modification time)
	assert.Equal(t, modTime1, modTime2, "certificate should be reused, not regenerated")
	
	// Clean up
	sm1.StopAll()
	sm2.StopAll()
}
