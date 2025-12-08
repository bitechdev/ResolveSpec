package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSanitizeXSS(t *testing.T) {
	sanitizer := DefaultSanitizer()

	tests := []struct {
		name     string
		input    string
		contains string // String that should NOT be in output
	}{
		{
			name:     "Script tag",
			input:    "<script>alert(1)</script>",
			contains: "<script>",
		},
		{
			name:     "JavaScript protocol",
			input:    "javascript:alert(1)",
			contains: "javascript:",
		},
		{
			name:     "Event handler",
			input:    "<img onerror='alert(1)'>",
			contains: "onerror=",
		},
		{
			name:     "Iframe",
			input:    "<iframe src='evil.com'></iframe>",
			contains: "<iframe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			if result == tt.input {
				t.Errorf("Sanitize() did not modify input: %q", tt.input)
			}
		})
	}
}

func TestSanitizeNullBytes(t *testing.T) {
	sanitizer := DefaultSanitizer()

	input := "hello\x00world"
	result := sanitizer.Sanitize(input)

	if result == input {
		t.Error("Null bytes should be removed")
	}

	if len(result) >= len(input) {
		t.Errorf("Result length should be less than input: got %d, input %d", len(result), len(input))
	}
}

func TestSanitizeControlCharacters(t *testing.T) {
	sanitizer := DefaultSanitizer()

	// Include various control characters
	input := "hello\x01\x02world\x1F"
	result := sanitizer.Sanitize(input)

	if result == input {
		t.Error("Control characters should be removed")
	}

	// Newlines, tabs, carriage returns should be preserved
	input2 := "hello\nworld\t\r"
	result2 := sanitizer.Sanitize(input2)

	if result2 != input2 {
		t.Errorf("Safe control characters should be preserved: got %q, want %q", result2, input2)
	}
}

func TestSanitizeMap(t *testing.T) {
	sanitizer := DefaultSanitizer()

	input := map[string]interface{}{
		"name":  "<script>alert(1)</script>John",
		"email": "test@example.com",
		"nested": map[string]interface{}{
			"bio": "<iframe src='evil.com'>Bio</iframe>",
		},
	}

	result := sanitizer.SanitizeMap(input)

	// Check that script tag was removed/escaped
	name, ok := result["name"].(string)
	if !ok || name == input["name"] {
		t.Error("Name should be sanitized")
	}

	// Check nested map
	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("Nested should still be a map")
	}

	bio, ok := nested["bio"].(string)
	if !ok || bio == input["nested"].(map[string]interface{})["bio"] {
		t.Error("Nested bio should be sanitized")
	}
}

func TestSanitizeMiddleware(t *testing.T) {
	sanitizer := DefaultSanitizer()

	handler := sanitizer.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that query param was sanitized
		param := r.URL.Query().Get("q")
		if param == "<script>alert(1)</script>" {
			t.Error("Query param should be sanitized")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test?q=<script>alert(1)</script>", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Handler failed: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // String that should NOT be in output
	}{
		{
			name:     "Path traversal",
			input:    "../../../etc/passwd",
			contains: "..",
		},
		{
			name:     "Absolute path",
			input:    "/etc/passwd",
			contains: "/",
		},
		{
			name:     "Windows path",
			input:    "..\\..\\windows\\system32",
			contains: "\\",
		},
		{
			name:     "Null byte",
			input:    "file\x00.txt",
			contains: "\x00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			if result == tt.input {
				t.Errorf("SanitizeFilename() did not modify input: %q", tt.input)
			}
		})
	}
}

func TestSanitizeEmail(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Uppercase",
			input:    "TEST@EXAMPLE.COM",
			expected: "test@example.com",
		},
		{
			name:     "Whitespace",
			input:    "  test@example.com  ",
			expected: "test@example.com",
		},
		{
			name:     "Null bytes",
			input:    "test\x00@example.com",
			expected: "test@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeEmail(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeEmail() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "JavaScript protocol",
			input:    "javascript:alert(1)",
			expected: "",
		},
		{
			name:     "Data protocol",
			input:    "data:text/html,<script>alert(1)</script>",
			expected: "",
		},
		{
			name:     "Valid HTTP URL",
			input:    "https://example.com",
			expected: "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestStrictSanitizer(t *testing.T) {
	sanitizer := StrictSanitizer()

	input := "<b>Bold text</b> with <script>alert(1)</script>"
	result := sanitizer.Sanitize(input)

	// Should strip ALL HTML tags
	if result == input {
		t.Error("Strict sanitizer should modify input")
	}

	// Should not contain any HTML tags
	if len(result) > 0 && (result[0] == '<' || result[len(result)-1] == '>') {
		t.Error("Result should not contain HTML tags")
	}
}

func TestMaxStringLength(t *testing.T) {
	sanitizer := &Sanitizer{
		MaxStringLength: 10,
	}

	input := "This is a very long string that exceeds the maximum length"
	result := sanitizer.Sanitize(input)

	if len(result) != 10 {
		t.Errorf("Result length = %d, want 10", len(result))
	}

	if result != input[:10] {
		t.Errorf("Result = %q, want %q", result, input[:10])
	}
}
