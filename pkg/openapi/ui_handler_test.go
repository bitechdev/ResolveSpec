package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func TestUIHandler_SwaggerUI(t *testing.T) {
	config := UIConfig{
		UIType:  SwaggerUI,
		SpecURL: "/openapi",
		Title:   "Test API Docs",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Check for Swagger UI specific content
	if !strings.Contains(body, "swagger-ui") {
		t.Error("Expected Swagger UI content")
	}
	if !strings.Contains(body, "SwaggerUIBundle") {
		t.Error("Expected SwaggerUIBundle script")
	}
	if !strings.Contains(body, config.Title) {
		t.Errorf("Expected title '%s' in HTML", config.Title)
	}
	if !strings.Contains(body, config.SpecURL) {
		t.Errorf("Expected spec URL '%s' in HTML", config.SpecURL)
	}
	if !strings.Contains(body, "swagger-ui-dist") {
		t.Error("Expected Swagger UI CDN link")
	}
}

func TestUIHandler_RapiDoc(t *testing.T) {
	config := UIConfig{
		UIType:  RapiDoc,
		SpecURL: "/api/spec",
		Title:   "RapiDoc Test",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Check for RapiDoc specific content
	if !strings.Contains(body, "rapi-doc") {
		t.Error("Expected rapi-doc element")
	}
	if !strings.Contains(body, "rapidoc-min.js") {
		t.Error("Expected RapiDoc script")
	}
	if !strings.Contains(body, config.Title) {
		t.Errorf("Expected title '%s' in HTML", config.Title)
	}
	if !strings.Contains(body, config.SpecURL) {
		t.Errorf("Expected spec URL '%s' in HTML", config.SpecURL)
	}
}

func TestUIHandler_Redoc(t *testing.T) {
	config := UIConfig{
		UIType:  Redoc,
		SpecURL: "/spec.json",
		Title:   "Redoc Test",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Check for Redoc specific content
	if !strings.Contains(body, "<redoc") {
		t.Error("Expected redoc element")
	}
	if !strings.Contains(body, "redoc.standalone.js") {
		t.Error("Expected Redoc script")
	}
	if !strings.Contains(body, config.Title) {
		t.Errorf("Expected title '%s' in HTML", config.Title)
	}
	if !strings.Contains(body, config.SpecURL) {
		t.Errorf("Expected spec URL '%s' in HTML", config.SpecURL)
	}
}

func TestUIHandler_Scalar(t *testing.T) {
	config := UIConfig{
		UIType:  Scalar,
		SpecURL: "/openapi.json",
		Title:   "Scalar Test",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Check for Scalar specific content
	if !strings.Contains(body, "api-reference") {
		t.Error("Expected api-reference element")
	}
	if !strings.Contains(body, "@scalar/api-reference") {
		t.Error("Expected Scalar script")
	}
	if !strings.Contains(body, config.Title) {
		t.Errorf("Expected title '%s' in HTML", config.Title)
	}
	if !strings.Contains(body, config.SpecURL) {
		t.Errorf("Expected spec URL '%s' in HTML", config.SpecURL)
	}
}

func TestUIHandler_DefaultValues(t *testing.T) {
	// Test with empty config to check defaults
	config := UIConfig{}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Should default to Swagger UI
	if !strings.Contains(body, "swagger-ui") {
		t.Error("Expected default to Swagger UI")
	}

	// Should default to /openapi spec URL
	if !strings.Contains(body, "/openapi") {
		t.Error("Expected default spec URL '/openapi'")
	}

	// Should default to "API Documentation" title
	if !strings.Contains(body, "API Documentation") {
		t.Error("Expected default title 'API Documentation'")
	}
}

func TestUIHandler_CustomCSS(t *testing.T) {
	customCSS := ".custom-class { color: red; }"
	config := UIConfig{
		UIType:    SwaggerUI,
		CustomCSS: customCSS,
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	body := w.Body.String()

	if !strings.Contains(body, customCSS) {
		t.Errorf("Expected custom CSS to be included. Body:\n%s", body)
	}
}

func TestUIHandler_Favicon(t *testing.T) {
	faviconURL := "https://example.com/favicon.ico"
	config := UIConfig{
		UIType:     SwaggerUI,
		FaviconURL: faviconURL,
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	body := w.Body.String()

	if !strings.Contains(body, faviconURL) {
		t.Error("Expected favicon URL to be included")
	}
}

func TestUIHandler_DarkTheme(t *testing.T) {
	config := UIConfig{
		UIType: SwaggerUI,
		Theme:  "dark",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	body := w.Body.String()

	// SwaggerUI uses monokai theme for dark mode
	if !strings.Contains(body, "monokai") {
		t.Error("Expected dark theme configuration for Swagger UI")
	}
}

func TestUIHandler_InvalidUIType(t *testing.T) {
	config := UIConfig{
		UIType: "invalid-ui-type",
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid UI type, got %d", resp.StatusCode)
	}
}

func TestUIHandler_ContentType(t *testing.T) {
	config := UIConfig{
		UIType: SwaggerUI,
	}

	handler := UIHandler(config)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected Content-Type to contain 'text/html', got '%s'", contentType)
	}
	if !strings.Contains(contentType, "charset=utf-8") {
		t.Errorf("Expected Content-Type to contain 'charset=utf-8', got '%s'", contentType)
	}
}

func TestSetupUIRoute(t *testing.T) {
	router := mux.NewRouter()

	config := UIConfig{
		UIType: SwaggerUI,
	}

	SetupUIRoute(router, "/api-docs", config)

	// Test that the route was added and works
	req := httptest.NewRequest("GET", "/api-docs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify it returns HTML
	body := w.Body.String()
	if !strings.Contains(body, "swagger-ui") {
		t.Error("Expected Swagger UI content")
	}
}
