package openapi

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// UIType represents the type of OpenAPI UI to serve
type UIType string

const (
	// SwaggerUI is the most popular OpenAPI UI
	SwaggerUI UIType = "swagger-ui"
	// RapiDoc is a modern, customizable OpenAPI UI
	RapiDoc UIType = "rapidoc"
	// Redoc is a clean, responsive OpenAPI UI
	Redoc UIType = "redoc"
	// Scalar is a modern and sleek OpenAPI UI
	Scalar UIType = "scalar"
)

// UIConfig holds configuration for the OpenAPI UI handler
type UIConfig struct {
	// UIType specifies which UI framework to use (default: SwaggerUI)
	UIType UIType
	// SpecURL is the URL to the OpenAPI spec JSON (default: "/openapi")
	SpecURL string
	// Title is the page title (default: "API Documentation")
	Title string
	// FaviconURL is the URL to the favicon (optional)
	FaviconURL string
	// CustomCSS allows injecting custom CSS (optional)
	CustomCSS string
	// Theme for the UI (light/dark, depends on UI type)
	Theme string
}

// UIHandler creates an HTTP handler that serves an OpenAPI UI
func UIHandler(config UIConfig) http.HandlerFunc {
	// Set defaults
	if config.UIType == "" {
		config.UIType = SwaggerUI
	}
	if config.SpecURL == "" {
		config.SpecURL = "/openapi"
	}
	if config.Title == "" {
		config.Title = "API Documentation"
	}
	if config.Theme == "" {
		config.Theme = "light"
	}

	return func(w http.ResponseWriter, r *http.Request) {
		var htmlContent string
		var err error

		switch config.UIType {
		case SwaggerUI:
			htmlContent, err = generateSwaggerUI(config)
		case RapiDoc:
			htmlContent, err = generateRapiDoc(config)
		case Redoc:
			htmlContent, err = generateRedoc(config)
		case Scalar:
			htmlContent, err = generateScalar(config)
		default:
			http.Error(w, "Unsupported UI type", http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to generate UI: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(htmlContent))
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to write response: %v", err), http.StatusInternalServerError)
			return
		}
	}
}

// templateData wraps UIConfig to properly handle CSS in templates
type templateData struct {
	UIConfig
	SafeCustomCSS template.CSS
}

// generateSwaggerUI generates the HTML for Swagger UI
func generateSwaggerUI(config UIConfig) (string, error) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    {{if .FaviconURL}}<link rel="icon" type="image/png" href="{{.FaviconURL}}">{{end}}
    <link rel="stylesheet" type="text/css" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
    {{if .SafeCustomCSS}}<style>{{.SafeCustomCSS}}</style>{{end}}
    <style>
        html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
        *, *:before, *:after { box-sizing: inherit; }
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const ui = SwaggerUIBundle({
                url: "{{.SpecURL}}",
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                {{if eq .Theme "dark"}}
                syntaxHighlight: {
                    activate: true,
                    theme: "monokai"
                }
                {{end}}
            });
            window.ui = ui;
        };
    </script>
</body>
</html>`

	t, err := template.New("swagger").Parse(tmpl)
	if err != nil {
		return "", err
	}

	data := templateData{
		UIConfig:      config,
		SafeCustomCSS: template.CSS(config.CustomCSS),
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateRapiDoc generates the HTML for RapiDoc
func generateRapiDoc(config UIConfig) (string, error) {
	theme := "light"
	if config.Theme == "dark" {
		theme = "dark"
	}

	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    {{if .FaviconURL}}<link rel="icon" type="image/png" href="{{.FaviconURL}}">{{end}}
    <script type="module" src="https://unpkg.com/rapidoc/dist/rapidoc-min.js"></script>
    {{if .SafeCustomCSS}}<style>{{.SafeCustomCSS}}</style>{{end}}
</head>
<body>
    <rapi-doc
        spec-url="{{.SpecURL}}"
        theme="` + theme + `"
        render-style="read"
        show-header="true"
        show-info="true"
        allow-try="true"
        allow-server-selection="true"
        allow-authentication="true"
        api-key-name="Authorization"
        api-key-location="header"
    ></rapi-doc>
</body>
</html>`

	t, err := template.New("rapidoc").Parse(tmpl)
	if err != nil {
		return "", err
	}

	data := templateData{
		UIConfig:      config,
		SafeCustomCSS: template.CSS(config.CustomCSS),
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateRedoc generates the HTML for Redoc
func generateRedoc(config UIConfig) (string, error) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    {{if .FaviconURL}}<link rel="icon" type="image/png" href="{{.FaviconURL}}">{{end}}
    {{if .SafeCustomCSS}}<style>{{.SafeCustomCSS}}</style>{{end}}
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <redoc spec-url="{{.SpecURL}}" {{if eq .Theme "dark"}}theme='{"colors": {"primary": {"main": "#dd5522"}}}'{{end}}></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`

	t, err := template.New("redoc").Parse(tmpl)
	if err != nil {
		return "", err
	}

	data := templateData{
		UIConfig:      config,
		SafeCustomCSS: template.CSS(config.CustomCSS),
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateScalar generates the HTML for Scalar
func generateScalar(config UIConfig) (string, error) {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    {{if .FaviconURL}}<link rel="icon" type="image/png" href="{{.FaviconURL}}">{{end}}
    {{if .SafeCustomCSS}}<style>{{.SafeCustomCSS}}</style>{{end}}
    <style>
        body { margin: 0; padding: 0; }
    </style>
</head>
<body>
    <script id="api-reference" data-url="{{.SpecURL}}" {{if eq .Theme "dark"}}data-theme="dark"{{end}}></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body>
</html>`

	t, err := template.New("scalar").Parse(tmpl)
	if err != nil {
		return "", err
	}

	data := templateData{
		UIConfig:      config,
		SafeCustomCSS: template.CSS(config.CustomCSS),
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// SetupUIRoute adds the OpenAPI UI route to a mux router
// This is a convenience function for the most common use case
func SetupUIRoute(router *mux.Router, path string, config UIConfig) {
	router.Handle(path, UIHandler(config))
}
