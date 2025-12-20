package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
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
