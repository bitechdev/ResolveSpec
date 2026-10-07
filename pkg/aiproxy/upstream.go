package aiproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Kind is the type of an upstream.
type Kind string

const (
	KindOpenAI Kind = "openai"
	KindMCP    Kind = "mcp"
)

const redacted = "[redacted]"

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// RateLimit limits requests per user per upstream (token bucket).
type RateLimit struct {
	PerSecond float64 // sustained requests per second
	Burst     int     // bucket size; defaults to ceil(PerSecond), min 1
}

// Upstream holds the settings shared by every upstream kind.
type Upstream struct {
	// Name is the route segment clients use: {prefix}/{Name}/...
	Name string
	// BaseURL of the upstream. OpenAI: include the version path (https://api.openai.com/v1).
	// MCP: the full streamable HTTP endpoint (http://localhost:3000/mcp).
	BaseURL string
	// APIKey is sent to the upstream. Never exposed to clients or logs.
	APIKey string
	// AuthHeader carries the key. Default "Authorization". Applies to OpenAI and MCP upstreams.
	AuthHeader string
	// AuthFormat renders the header value; %s is the key (e.g. "Bearer %s", "Token %s", "%s").
	// Default: "Bearer %s" for the Authorization header, "%s" (raw key) for any other header.
	AuthFormat string
	// Headers are extra static headers sent to the upstream (applied before the key header).
	// Per-request values can be set in a BeforeProxy hook.
	Headers map[string]string
	// AllowedRoles: caller needs at least one. Empty means any authenticated user.
	AllowedRoles []string
	// RateLimit per user for this upstream. Nil means unlimited.
	RateLimit *RateLimit
	// Timeout waiting for the upstream response headers. Default 120s. Streams are not cut off.
	Timeout time.Duration
}

// OpenAIUpstream is an OpenAI-compatible API (OpenAI, Azure, Ollama, vLLM, ...).
type OpenAIUpstream struct {
	Upstream
	// AllowedModels restricts the "model" a request may use. Empty means any.
	// When set, write requests must be JSON bodies carrying an allowed model.
	AllowedModels []string
}

// MCPUpstream is an MCP server speaking streamable HTTP.
type MCPUpstream struct {
	Upstream
	// AllowedTools restricts tools/call to these tool names. Empty means any.
	AllowedTools []string
}

// String redacts the key.
func (u Upstream) String() string {
	return fmt.Sprintf("Upstream{Name:%s BaseURL:%s APIKey:%s}", u.Name, u.BaseURL, keyMask(u.APIKey))
}

// GoString redacts the key (%#v).
func (u Upstream) GoString() string { return u.String() }

// MarshalJSON redacts the key.
func (u Upstream) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name         string   `json:"name"`
		BaseURL      string   `json:"base_url"`
		APIKey       string   `json:"api_key,omitempty"`
		AllowedRoles []string `json:"allowed_roles,omitempty"`
	}{u.Name, u.BaseURL, keyMask(u.APIKey), u.AllowedRoles})
}

func keyMask(k string) string {
	if k == "" {
		return ""
	}
	return redacted
}

// validate checks the shared fields and returns the parsed base URL.
func (u Upstream) validate() (*url.URL, error) {
	if !nameRe.MatchString(u.Name) {
		return nil, fmt.Errorf("aiproxy: invalid upstream name %q", u.Name)
	}
	base, err := url.Parse(strings.TrimRight(u.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("aiproxy: upstream %q has an invalid base URL", u.Name)
	}
	if u.AuthFormat != "" && (strings.Count(u.AuthFormat, "%s") != 1 || strings.Count(u.AuthFormat, "%") != 1) {
		return nil, fmt.Errorf("aiproxy: upstream %q auth format must contain exactly one %%s", u.Name)
	}
	if u.RateLimit != nil && u.RateLimit.PerSecond <= 0 {
		return nil, fmt.Errorf("aiproxy: upstream %q rate limit must be > 0", u.Name)
	}
	return base, nil
}

// authValue returns the header name and value carrying the key, or "" when no key is set.
func (u Upstream) authValue() (name, value string) {
	if u.APIKey == "" {
		return "", ""
	}
	name = u.AuthHeader
	if name == "" {
		name = "Authorization"
	}
	format := u.AuthFormat
	if format == "" {
		if http.CanonicalHeaderKey(name) == "Authorization" {
			format = "Bearer %s"
		} else {
			format = "%s"
		}
	}
	return name, fmt.Sprintf(format, u.APIKey)
}

func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(items))
	for _, i := range items {
		m[i] = struct{}{}
	}
	return m
}
