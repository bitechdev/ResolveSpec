package metrics

import (
	"net/http"
	"strings"
	"sync"
)

const (
	// defaultHTTPMaxPaths is the default cap on distinct values of the "path" label.
	defaultHTTPMaxPaths = 1024

	// overflowPathLabel is used once the cap on distinct path labels is reached.
	overflowPathLabel = "other"
)

// routeLabel returns the low-cardinality path label for a request, preferring
// (in order): the custom normalizer, the matched ServeMux pattern, and finally
// the generic normalization of the raw URL path.
func routeLabel(r *http.Request, custom func(*http.Request) string) string {
	if custom != nil {
		if p := custom(r); p != "" {
			return p
		}
	}
	if r.Pattern != "" {
		return stripPatternMethod(r.Pattern)
	}
	return NormalizePath(r.URL.Path)
}

// stripPatternMethod removes the optional "METHOD " prefix (and host) from a
// Go 1.22+ ServeMux pattern, e.g. "GET /users/{id}" -> "/users/{id}".
func stripPatternMethod(pattern string) string {
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		pattern = strings.TrimLeft(pattern[i+1:], " ")
	}
	if i := strings.IndexByte(pattern, '/'); i > 0 {
		pattern = pattern[i:] // drop host part
	}
	return pattern
}

// NormalizePath replaces dynamic-looking path segments (numeric IDs, UUIDs,
// long hex strings and other long opaque tokens) with ":id" so that
// /users/123 and /users/456 share one label value.
func NormalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.Contains(path, "/") {
		return path
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if isDynamicSegment(s) {
			segs[i] = ":id"
		}
	}
	return strings.Join(segs, "/")
}

func isDynamicSegment(s string) bool {
	if s == "" {
		return false
	}
	if allDigits(s) {
		return true
	}
	if isUUID(s) {
		return true
	}
	// Long hex strings (hashes, object IDs)
	if len(s) >= 16 && allHex(s) {
		return true
	}
	// Long opaque tokens containing digits (base64/ULID-like)
	if len(s) >= 24 && hasDigit(s) && !strings.ContainsAny(s, ".") {
		return true
	}
	return false
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isHexByte(c) {
			return false
		}
	}
	return true
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexByte(c) {
				return false
			}
		}
	}
	return true
}

// pathLimiter bounds the number of distinct path label values. Once the cap is
// reached, unseen paths are reported as "other".
type pathLimiter struct {
	mu   sync.RWMutex
	max  int // <= 0 disables the cap
	seen map[string]struct{}
}

func newPathLimiter(limit int) *pathLimiter {
	return &pathLimiter{max: limit, seen: make(map[string]struct{})}
}

func (l *pathLimiter) label(path string) string {
	if l.max <= 0 {
		return path
	}
	l.mu.RLock()
	_, ok := l.seen[path]
	l.mu.RUnlock()
	if ok {
		return path
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[path]; ok {
		return path
	}
	if len(l.seen) >= l.max {
		return overflowPathLabel
	}
	l.seen[path] = struct{}{}
	return path
}

func (l *pathLimiter) reset() {
	l.mu.Lock()
	l.seen = make(map[string]struct{})
	l.mu.Unlock()
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
