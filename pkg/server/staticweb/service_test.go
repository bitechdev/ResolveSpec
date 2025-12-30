package staticweb

import (
	"net/http"
	"net/http/httptest"
	"testing"

	staticwebtesting "github.com/bitechdev/ResolveSpec/pkg/server/staticweb/testing"
)

func TestServiceMount(t *testing.T) {
	service := NewService(nil)

	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
	})

	err := service.Mount(MountConfig{
		URLPrefix: "/test",
		Provider:  provider,
	})

	if err != nil {
		t.Fatalf("Failed to mount: %v", err)
	}

	mounts := service.ListMounts()
	if len(mounts) != 1 || mounts[0] != "/test" {
		t.Errorf("Expected [/test], got %v", mounts)
	}
}

func TestServiceUnmount(t *testing.T) {
	service := NewService(nil)

	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
	})

	service.Mount(MountConfig{
		URLPrefix: "/test",
		Provider:  provider,
	})

	err := service.Unmount("/test")
	if err != nil {
		t.Fatalf("Failed to unmount: %v", err)
	}

	mounts := service.ListMounts()
	if len(mounts) != 0 {
		t.Errorf("Expected empty list, got %v", mounts)
	}
}

func TestServiceHandler(t *testing.T) {
	service := NewService(nil)

	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
		"app.js":     []byte("console.log('test')"),
	})

	err := service.Mount(MountConfig{
		URLPrefix: "/static",
		Provider:  provider,
	})

	if err != nil {
		t.Fatalf("Failed to mount: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "serve index.html",
			path:           "/static/index.html",
			expectedStatus: http.StatusOK,
			expectedBody:   "<html>test</html>",
		},
		{
			name:           "serve app.js",
			path:           "/static/app.js",
			expectedStatus: http.StatusOK,
			expectedBody:   "console.log('test')",
		},
		{
			name:           "non-existent file",
			path:           "/static/nonexistent.html",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "",
		},
		{
			name:           "non-matching prefix returns nothing",
			path:           "/api/test",
			expectedStatus: http.StatusOK, // Handler returns without writing
			expectedBody:   "",
		},
	}

	handler := service.Handler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			// For non-matching prefix, handler doesn't write anything
			if tt.path == "/api/test" {
				if rec.Code != 200 || rec.Body.Len() != 0 {
					t.Errorf("Expected no response, got status %d with body length %d", rec.Code, rec.Body.Len())
				}
				return
			}

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.expectedBody != "" && rec.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func TestServiceLongestPrefixMatching(t *testing.T) {
	service := NewService(nil)

	// Mount at /
	provider1 := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("root"),
	})

	// Mount at /static
	provider2 := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("static"),
	})

	service.Mount(MountConfig{
		URLPrefix: "/",
		Provider:  provider1,
	})

	service.Mount(MountConfig{
		URLPrefix: "/static",
		Provider:  provider2,
	})

	handler := service.Handler()

	tests := []struct {
		path         string
		expectedBody string
	}{
		{"/index.html", "root"},
		{"/static/index.html", "static"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", rec.Code)
			}

			if rec.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func TestServiceClose(t *testing.T) {
	service := NewService(nil)

	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
	})

	service.Mount(MountConfig{
		URLPrefix: "/test",
		Provider:  provider,
	})

	err := service.Close()
	if err != nil {
		t.Fatalf("Failed to close service: %v", err)
	}

	mounts := service.ListMounts()
	if len(mounts) != 0 {
		t.Errorf("Expected empty list after close, got %v", mounts)
	}
}

func TestServiceReload(t *testing.T) {
	service := NewService(nil)

	// Create a mock provider that supports reload
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("original"),
	})

	service.Mount(MountConfig{
		URLPrefix: "/test",
		Provider:  provider,
	})

	handler := service.Handler()

	// Test initial content
	req := httptest.NewRequest("GET", "/test/index.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "original" {
		t.Errorf("Expected body 'original', got %q", rec.Body.String())
	}

	// Update the provider's content
	provider.AddFile("index.html", []byte("updated"))

	// The content is already updated since we're using a mock
	// In a real scenario with zip files, you'd call Reload() here
	err := service.Reload()
	if err != nil {
		t.Fatalf("Failed to reload service: %v", err)
	}

	// Test updated content
	req = httptest.NewRequest("GET", "/test/index.html", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "updated" {
		t.Errorf("Expected body 'updated', got %q", rec.Body.String())
	}
}
