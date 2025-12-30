package staticweb_test

import (
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/server/staticweb"
	staticwebtesting "github.com/bitechdev/ResolveSpec/pkg/server/staticweb/testing"
)

// Example_reload demonstrates reloading content when files change.
func Example_reload() {
	service := staticweb.NewService(nil)

	// Create a provider
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"version.txt": []byte("v1.0.0"),
	})

	service.Mount(staticweb.MountConfig{
		URLPrefix: "/static",
		Provider:  provider,
	})

	// Simulate updating the file
	provider.AddFile("version.txt", []byte("v2.0.0"))

	// Reload to pick up changes (in real usage with zip files)
	err := service.Reload()
	if err != nil {
		fmt.Printf("Failed to reload: %v\n", err)
	} else {
		fmt.Println("Successfully reloaded static files")
	}

	// Output: Successfully reloaded static files
}

// Example_reloadZip demonstrates reloading a zip file provider.
func Example_reloadZip() {
	service := staticweb.NewService(nil)

	// In production, you would use:
	// provider, _ := staticweb.ZipProvider("./dist.zip")
	// For this example, we use a mock
	provider := staticwebtesting.NewMockProvider(map[string][]byte{
		"app.js": []byte("console.log('v1')"),
	})

	service.Mount(staticweb.MountConfig{
		URLPrefix: "/app",
		Provider:  provider,
	})

	fmt.Println("Serving from zip file")

	// When the zip file is updated, call Reload()
	// service.Reload()

	// Output: Serving from zip file
}
