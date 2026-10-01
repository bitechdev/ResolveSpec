package common

import (
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int
}

// DefaultCORSConfig returns a default CORS configuration suitable for HeadSpec
func DefaultCORSConfig() CORSConfig {
	configManager := config.GetConfigManager()
	cfg, _ := configManager.GetConfig()
	hosts := make([]string, 0)
	if cfg == nil {
		return CORSConfig{
			AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: GetHeadSpecHeaders(),
			MaxAge:         86400,
		}
	}
	// Explicitly configured origins (cors.allowed_origins); "*" allows any origin without credentials
	hosts = append(hosts, cfg.CORS.AllowedOrigins...)

	_, _, ipsList := config.GetIPs()

	for i := range cfg.Servers.Instances {
		server := cfg.Servers.Instances[i]
		if server.Port == 0 {
			continue
		}
		hosts = append(hosts, server.ExternalURLs...)
		hosts = append(hosts, fmt.Sprintf("http://%s:%d", server.Host, server.Port))
		hosts = append(hosts, fmt.Sprintf("https://%s:%d", server.Host, server.Port))
		hosts = append(hosts, fmt.Sprintf("http://%s:%d", "localhost", server.Port))
		for _, ip := range ipsList {
			hosts = append(hosts, fmt.Sprintf("http://%s:%d", ip.String(), server.Port))
			hosts = append(hosts, fmt.Sprintf("https://%s:%d", ip.String(), server.Port))
		}
	}

	return CORSConfig{
		AllowedOrigins: hosts,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: GetHeadSpecHeaders(),
		MaxAge:         86400, // 24 hours
	}
}

// GetHeadSpecHeaders returns all headers used by HeadSpec
func GetHeadSpecHeaders() []string {
	return []string{
		// Standard headers
		"Content-Type",
		"Authorization",
		"Accept",
		"Accept-Language",
		"Content-Language",

		// Field Selection
		"X-Select-Fields",
		"X-Not-Select-Fields",
		"X-Clean-JSON",

		// Filtering & Search
		"X-FieldFilter-*",
		"X-SearchFilter-*",
		"X-SearchOp-*",
		"X-SearchOr-*",
		"X-SearchAnd-*",
		"X-SearchCols",
		"X-Custom-SQL-W",
		"X-Custom-SQL-W-*",
		"X-Custom-SQL-Or",
		"X-Custom-SQL-Or-*",

		// Joins & Relations
		"X-Preload",
		"X-Preload-*",
		"X-Expand",
		"X-Expand-*",
		"X-Custom-SQL-Join",
		"X-Custom-SQL-Join-*",

		// Sorting & Pagination
		"X-Sort",
		"X-Sort-*",
		"X-Limit",
		"X-Offset",
		"X-Cursor-Forward",
		"X-Cursor-Backward",

		// Advanced Features
		"X-AdvSQL-*",
		"X-CQL-Sel-*",
		"X-Distinct",
		"X-SkipCount",
		"X-SkipCache",
		"X-Fetch-RowNumber",
		"X-PKRow",

		// Response Format
		"X-SimpleAPI",
		"X-DetailAPI",
		"X-Syncfusion",
		"X-Single-Record-As-Object",

		// Transaction Control
		"X-Transaction-Atomic",

		// X-Files - comprehensive JSON configuration
		"X-Files",
	}
}

// originAllowed reports whether origin matches config.AllowedOrigins exactly
// (case-insensitive, trailing slash ignored). wildcard is true when the list
// contains "*".
func originAllowed(origin string, allowed []string) (ok bool, wildcard bool) {
	norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimRight(strings.TrimSpace(a), "/"))
		if a == "*" {
			wildcard = true
			continue
		}
		if a != "" && a == norm {
			return true, wildcard
		}
	}
	return false, wildcard
}

// SetCORSHeaders sets CORS headers on a response writer.
//
// The request Origin is only reflected (and credentials only allowed) when it
// is listed in config.AllowedOrigins. A "*" entry allows any origin but never
// with credentials. Unlisted origins get no CORS headers, so browsers block
// the cross-origin read.
func SetCORSHeaders(w ResponseWriter, r Request, config CORSConfig) {
	if !Hardening().CORSStrictOrigins {
		setCORSHeadersLegacy(w, r, config)
		return
	}
	origin := r.Header("Origin")
	if origin == "" {
		// Not a cross-origin browser request; nothing to protect.
		w.SetHeader("Access-Control-Allow-Origin", "*")
	} else {
		// Vary must be set so caches don't serve one origin's response to another
		w.UnderlyingResponseWriter().Header().Set("Vary", "Origin")

		ok, wildcard := originAllowed(origin, config.AllowedOrigins)
		switch {
		case ok:
			w.SetHeader("Access-Control-Allow-Origin", origin)
			w.SetHeader("Access-Control-Allow-Credentials", "true")
		case wildcard:
			w.SetHeader("Access-Control-Allow-Origin", "*")
		default:
			return
		}
	}

	// Set allowed methods
	if len(config.AllowedMethods) > 0 {
		w.SetHeader("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	}

	// The origin is trusted at this point, so reflecting the preflight request
	// headers is safe (the config list contains "X-Foo-*" patterns that browsers
	// cannot match literally).
	requestedHeaders := r.Header("Access-Control-Request-Headers")
	if requestedHeaders != "" {
		w.SetHeader("Access-Control-Allow-Headers", requestedHeaders)
	} else if len(config.AllowedHeaders) > 0 {
		w.SetHeader("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	}

	// Set max age
	if config.MaxAge > 0 {
		w.SetHeader("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))
	}

	// Expose headers that clients can read (fresh slice: avoid appending into
	// config.AllowedHeaders' backing array)
	exposeHeaders := make([]string, 0, len(config.AllowedHeaders)+3)
	exposeHeaders = append(exposeHeaders, config.AllowedHeaders...)
	exposeHeaders = append(exposeHeaders, "Content-Range", "X-Api-Range-Total", "X-Api-Range-Size")
	w.SetHeader("Access-Control-Expose-Headers", strings.Join(exposeHeaders, ", "))
}

// setCORSHeadersLegacy is the pre-hardening behaviour (reflect any origin with
// credentials). Used only when hardening.cors_strict_origins is false.
func setCORSHeadersLegacy(w ResponseWriter, r Request, config CORSConfig) {
	origin := r.Header("Origin")
	if origin == "" {
		origin = "*"
	} else {
		w.UnderlyingResponseWriter().Header().Set("Vary", "Origin")
	}
	w.SetHeader("Access-Control-Allow-Origin", origin)
	if len(config.AllowedMethods) > 0 {
		w.SetHeader("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	}
	if requested := r.Header("Access-Control-Request-Headers"); requested != "" {
		w.SetHeader("Access-Control-Allow-Headers", requested)
	} else if len(config.AllowedHeaders) > 0 {
		w.SetHeader("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	}
	if config.MaxAge > 0 {
		w.SetHeader("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))
	}
	if origin != "*" {
		w.SetHeader("Access-Control-Allow-Credentials", "true")
	}
	exposeHeaders := make([]string, 0, len(config.AllowedHeaders)+3)
	exposeHeaders = append(exposeHeaders, config.AllowedHeaders...)
	exposeHeaders = append(exposeHeaders, "Content-Range", "X-Api-Range-Total", "X-Api-Range-Size")
	w.SetHeader("Access-Control-Expose-Headers", strings.Join(exposeHeaders, ", "))
}
