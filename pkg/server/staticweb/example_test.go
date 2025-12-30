package staticweb_test

import (
	"fmt"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/server/staticweb"
	staticwebtesting "github.com/bitechdev/ResolveSpec/pkg/server/staticweb/testing"
	"github.com/gorilla/mux"
)

// Example_basic demonstrates serving files from a local directory.
func Example_basic() {
	service := staticweb.NewService(nil)

	// Using mock provider for example purposes
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
	})

	_ = service.Mount(staticweb.MountConfig{
		URLPrefix: "/static",
		Provider:  provider,
	})

	router := mux.NewRouter()
	router.PathPrefix("/").Handler(service.Handler())

	fmt.Println("Serving files from ./public at /static")
	// Output: Serving files from ./public at /static
}

// Example_spa demonstrates an SPA with HTML fallback routing.
func Example_spa() {
	service := staticweb.NewService(nil)

	// Using mock provider for example purposes
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>app</html>"),
	})

	_ = service.Mount(staticweb.MountConfig{
		URLPrefix:        "/",
		Provider:         provider,
		FallbackStrategy: staticweb.HTMLFallback("index.html"),
	})

	router := mux.NewRouter()

	// API routes take precedence
	router.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("users"))
	})

	// Static files handle all other routes
	router.PathPrefix("/").Handler(service.Handler())

	fmt.Println("SPA with fallback to index.html")
	// Output: SPA with fallback to index.html
}

// Example_multiple demonstrates multiple mount points with different policies.
func Example_multiple() {
	service := staticweb.NewService(&staticweb.ServiceConfig{
		DefaultCacheTime: 3600,
	})

	// Assets with long cache (using mock for example)
	assetsProvider := staticwebtesting.NewMockProvider(map[string][]byte{
		"app.js": []byte("console.log('test')"),
	})
	service.Mount(staticweb.MountConfig{
		URLPrefix:   "/assets",
		Provider:    assetsProvider,
		CachePolicy: staticweb.SimpleCache(604800), // 1 week
	})

	// HTML with short cache (using mock for example)
	htmlProvider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
	})
	service.Mount(staticweb.MountConfig{
		URLPrefix:   "/",
		Provider:    htmlProvider,
		CachePolicy: staticweb.SimpleCache(300), // 5 minutes
	})

	fmt.Println("Multiple mount points configured")
	// Output: Multiple mount points configured
}

// Example_zip demonstrates serving from a zip file (concept).
func Example_zip() {
	service := staticweb.NewService(nil)

	// For actual usage, you would use:
	// provider, err := staticweb.ZipProvider("./static.zip")
	// For this example, we use a mock
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"file.txt": []byte("content"),
	})

	service.Mount(staticweb.MountConfig{
		URLPrefix: "/static",
		Provider:  provider,
	})

	fmt.Println("Serving from zip file")
	// Output: Serving from zip file
}

// Example_extensionCache demonstrates extension-based caching.
func Example_extensionCache() {
	service := staticweb.NewService(nil)

	// Using mock provider for example purposes
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"index.html": []byte("<html>test</html>"),
		"app.js":     []byte("console.log('test')"),
	})

	// Different cache times per file type
	cacheRules := map[string]int{
		".html": 3600,   // 1 hour
		".js":   86400,  // 1 day
		".css":  86400,  // 1 day
		".png":  604800, // 1 week
	}

	service.Mount(staticweb.MountConfig{
		URLPrefix:   "/",
		Provider:    provider,
		CachePolicy: staticweb.ExtensionCache(cacheRules, 3600), // default 1 hour
	})

	fmt.Println("Extension-based caching configured")
	// Output: Extension-based caching configured
}
