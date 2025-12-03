package common

import (
	"fmt"
	"strings"
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
	return CORSConfig{
		AllowedOrigins: []string{"*"},
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

// SetCORSHeaders sets CORS headers on a response writer
func SetCORSHeaders(w ResponseWriter, config CORSConfig) {
	// Set allowed origins
	if len(config.AllowedOrigins) > 0 {
		w.SetHeader("Access-Control-Allow-Origin", strings.Join(config.AllowedOrigins, ", "))
	}

	// Set allowed methods
	if len(config.AllowedMethods) > 0 {
		w.SetHeader("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
	}

	// Set allowed headers
	if len(config.AllowedHeaders) > 0 {
		w.SetHeader("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
	}

	// Set max age
	if config.MaxAge > 0 {
		w.SetHeader("Access-Control-Max-Age", fmt.Sprintf("%d", config.MaxAge))
	}

	// Allow credentials
	w.SetHeader("Access-Control-Allow-Credentials", "true")

	// Expose headers that clients can read
	w.SetHeader("Access-Control-Expose-Headers", "Content-Range, X-Api-Range-Total, X-Api-Range-Size")
}
