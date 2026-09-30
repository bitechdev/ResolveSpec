package resolvespec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIError is the server error object.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	Detail  string `json:"detail,omitempty"` // server-side reason (funcspec / restheadspec)
	SQL     string `json:"sql,omitempty"`
}

// Error is returned on a non-2xx response or an unsuccessful result.
type Error struct {
	StatusCode int
	APIError
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("http %d", e.StatusCode)
}

type config struct {
	baseURL string
	token   string
	headers http.Header
	http    *http.Client
}

// Option configures a client.
type Option func(*config)

func WithToken(token string) Option        { return func(c *config) { c.token = token } }
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.http = h } }
func WithHeader(name, value string) Option {
	return func(c *config) { c.headers.Set(name, value) }
}

func newConfig(baseURL string, opts []Option) config {
	c := config{baseURL: strings.TrimRight(baseURL, "/"), headers: http.Header{}, http: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// headers: Content-Type < custom headers < bearer token.
func (c *config) newRequest(ctx context.Context, method, u string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range c.headers {
		req.Header[k] = append([]string(nil), vs...)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *config) do(req *http.Request) (*http.Response, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp, b, err
}

func errorFrom(status int, body []byte) *Error {
	e := &Error{StatusCode: status}
	var env struct {
		Error *APIError `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error != nil {
		e.APIError = *env.Error
	}
	if e.Message == "" {
		text := ""
		if !json.Valid(body) {
			text = strings.TrimSpace(string(body))
			if len(text) > 200 {
				text = text[:200]
			}
		}
		if text == "" {
			text = fmt.Sprintf("%s (%d)", http.StatusText(status), status)
		}
		e.Message = text
	}
	return e
}

func buildURL(base, schema, entity string, id string) string {
	u := base + "/" + url.PathEscape(schema) + "/" + url.PathEscape(entity)
	if id != "" {
		u += "/" + url.PathEscape(id)
	}
	return u
}
