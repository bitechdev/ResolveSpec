package middleware

import (
	"html"
	"net/http"
	"regexp"
	"strings"
)

// Sanitizer provides input sanitization beyond SQL injection protection
type Sanitizer struct {
	// StripHTML removes HTML tags from input
	StripHTML bool

	// EscapeHTML escapes HTML entities
	EscapeHTML bool

	// RemoveNullBytes removes null bytes from input
	RemoveNullBytes bool

	// RemoveControlChars removes control characters (except newline, carriage return, tab)
	RemoveControlChars bool

	// MaxStringLength limits individual string field length (0 = no limit)
	MaxStringLength int

	// BlockPatterns are regex patterns to block (e.g., script tags, SQL keywords)
	BlockPatterns []*regexp.Regexp

	// Custom sanitization function
	CustomSanitizer func(string) string
}

// DefaultSanitizer returns a sanitizer with secure defaults
func DefaultSanitizer() *Sanitizer {
	return &Sanitizer{
		StripHTML:          false, // Don't strip by default (breaks legitimate HTML content)
		EscapeHTML:         true,  // Escape HTML entities to prevent XSS
		RemoveNullBytes:    true,  // Remove null bytes (security best practice)
		RemoveControlChars: true,  // Remove dangerous control characters
		MaxStringLength:    0,     // No limit by default

		// Block common XSS and injection patterns
		BlockPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`), // Script tags
			regexp.MustCompile(`(?i)javascript:`),               // JavaScript protocol
			regexp.MustCompile(`(?i)on\w+\s*=`),                 // Event handlers (onclick, onerror, etc.)
			regexp.MustCompile(`(?i)<iframe[^>]*>`),             // Iframes
			regexp.MustCompile(`(?i)<object[^>]*>`),             // Objects
			regexp.MustCompile(`(?i)<embed[^>]*>`),              // Embeds
		},
	}
}

// StrictSanitizer returns a sanitizer with very strict rules
func StrictSanitizer() *Sanitizer {
	s := DefaultSanitizer()
	s.StripHTML = true
	s.MaxStringLength = 10000
	return s
}

// Sanitize sanitizes a string value
func (s *Sanitizer) Sanitize(value string) string {
	if value == "" {
		return value
	}

	// Remove null bytes
	if s.RemoveNullBytes {
		value = strings.ReplaceAll(value, "\x00", "")
	}

	// Remove control characters
	if s.RemoveControlChars {
		value = removeControlCharacters(value)
	}

	// Check block patterns
	for _, pattern := range s.BlockPatterns {
		if pattern.MatchString(value) {
			// Replace matched pattern with empty string
			value = pattern.ReplaceAllString(value, "")
		}
	}

	// Strip HTML tags
	if s.StripHTML {
		value = stripHTMLTags(value)
	}

	// Escape HTML entities
	if s.EscapeHTML && !s.StripHTML {
		value = html.EscapeString(value)
	}

	// Apply max length
	if s.MaxStringLength > 0 && len(value) > s.MaxStringLength {
		value = value[:s.MaxStringLength]
	}

	// Apply custom sanitizer
	if s.CustomSanitizer != nil {
		value = s.CustomSanitizer(value)
	}

	return value
}

// SanitizeMap sanitizes all string values in a map
func (s *Sanitizer) SanitizeMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range data {
		result[key] = s.sanitizeValue(value)
	}
	return result
}

// sanitizeValue recursively sanitizes values
func (s *Sanitizer) sanitizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return s.Sanitize(v)
	case map[string]interface{}:
		return s.SanitizeMap(v)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = s.sanitizeValue(item)
		}
		return result
	default:
		return value
	}
}

// Middleware returns an HTTP middleware that sanitizes request headers and query params
// Note: Body sanitization should be done at the application level after parsing
func (s *Sanitizer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanitize query parameters
		if r.URL.RawQuery != "" {
			q := r.URL.Query()
			sanitized := false
			for key, values := range q {
				for i, value := range values {
					sanitizedValue := s.Sanitize(value)
					if sanitizedValue != value {
						values[i] = sanitizedValue
						sanitized = true
					}
				}
				if sanitized {
					q[key] = values
				}
			}
			if sanitized {
				r.URL.RawQuery = q.Encode()
			}
		}

		// Sanitize specific headers (User-Agent, Referer, etc.)
		dangerousHeaders := []string{
			"User-Agent",
			"Referer",
			"X-Forwarded-For",
			"X-Real-IP",
		}

		for _, header := range dangerousHeaders {
			if value := r.Header.Get(header); value != "" {
				sanitized := s.Sanitize(value)
				if sanitized != value {
					r.Header.Set(header, sanitized)
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// Helper functions

// removeControlCharacters removes control characters except \n, \r, \t
func removeControlCharacters(s string) string {
	var result strings.Builder
	for _, r := range s {
		// Keep newline, carriage return, tab, and non-control characters
		if r == '\n' || r == '\r' || r == '\t' || r >= 32 {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// stripHTMLTags removes HTML tags from a string
func stripHTMLTags(s string) string {
	// Simple regex to remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// Common sanitization patterns

// SanitizeFilename sanitizes a filename
func SanitizeFilename(filename string) string {
	// Remove path traversal attempts
	filename = strings.ReplaceAll(filename, "..", "")
	filename = strings.ReplaceAll(filename, "/", "")
	filename = strings.ReplaceAll(filename, "\\", "")

	// Remove null bytes
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Limit length
	if len(filename) > 255 {
		filename = filename[:255]
	}

	return filename
}

// SanitizeEmail performs basic email sanitization
func SanitizeEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))

	// Remove dangerous characters
	email = strings.ReplaceAll(email, "\x00", "")
	email = removeControlCharacters(email)

	return email
}

// SanitizeURL performs basic URL sanitization
func SanitizeURL(url string) string {
	url = strings.TrimSpace(url)

	// Remove null bytes
	url = strings.ReplaceAll(url, "\x00", "")

	// Block javascript: and data: protocols
	if strings.HasPrefix(strings.ToLower(url), "javascript:") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		return ""
	}

	return url
}
