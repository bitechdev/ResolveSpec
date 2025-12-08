package router

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// MuxAdapter adapts Gorilla Mux to work with our Router interface
type MuxAdapter struct {
	router *mux.Router
}

// NewMuxAdapter creates a new Mux adapter
func NewMuxAdapter(router *mux.Router) *MuxAdapter {
	return &MuxAdapter{router: router}
}

func (m *MuxAdapter) HandleFunc(pattern string, handler common.HTTPHandlerFunc) common.RouteRegistration {
	route := &MuxRouteRegistration{
		router:  m.router,
		pattern: pattern,
		handler: handler,
	}
	return route
}

func (m *MuxAdapter) ServeHTTP(w common.ResponseWriter, r common.Request) {
	// This method would be used when we need to serve through our interface
	// For now, we'll work directly with the underlying router
	w.WriteHeader(http.StatusNotImplemented)
	_, err := w.Write([]byte(`{"error":"ServeHTTP not implemented - use GetMuxRouter() for direct access"}`))
	if err != nil {
		logger.Warn("Failed to write. %v", err)
	}
}

// MuxRouteRegistration implements RouteRegistration for Mux
type MuxRouteRegistration struct {
	router  *mux.Router
	pattern string
	handler common.HTTPHandlerFunc
	route   *mux.Route
}

func (m *MuxRouteRegistration) Methods(methods ...string) common.RouteRegistration {
	if m.route == nil {
		m.route = m.router.HandleFunc(m.pattern, func(w http.ResponseWriter, r *http.Request) {
			reqAdapter := &HTTPRequest{req: r, vars: mux.Vars(r)}
			respAdapter := &HTTPResponseWriter{resp: w}
			m.handler(respAdapter, reqAdapter)
		})
	}
	m.route.Methods(methods...)
	return m
}

func (m *MuxRouteRegistration) PathPrefix(prefix string) common.RouteRegistration {
	if m.route == nil {
		m.route = m.router.HandleFunc(m.pattern, func(w http.ResponseWriter, r *http.Request) {
			reqAdapter := &HTTPRequest{req: r, vars: mux.Vars(r)}
			respAdapter := &HTTPResponseWriter{resp: w}
			m.handler(respAdapter, reqAdapter)
		})
	}
	m.route.PathPrefix(prefix)
	return m
}

// HTTPRequest adapts standard http.Request to our Request interface
type HTTPRequest struct {
	req  *http.Request
	vars map[string]string
	body []byte
}

func NewHTTPRequest(r *http.Request) *HTTPRequest {
	return &HTTPRequest{
		req:  r,
		vars: make(map[string]string),
	}
}

func (h *HTTPRequest) Method() string {
	return h.req.Method
}

func (h *HTTPRequest) URL() string {
	return h.req.URL.String()
}

func (h *HTTPRequest) Header(key string) string {
	return h.req.Header.Get(key)
}

func (h *HTTPRequest) Body() ([]byte, error) {
	if h.body != nil {
		return h.body, nil
	}
	if h.req.Body == nil {
		return nil, nil
	}
	defer h.req.Body.Close()
	body, err := io.ReadAll(h.req.Body)
	if err != nil {
		return nil, err
	}
	h.body = body
	return body, nil
}

func (h *HTTPRequest) PathParam(key string) string {
	return h.vars[key]
}

func (h *HTTPRequest) QueryParam(key string) string {
	return h.req.URL.Query().Get(key)
}

func (h *HTTPRequest) AllQueryParams() map[string]string {
	params := make(map[string]string)
	for key, values := range h.req.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}

func (h *HTTPRequest) AllHeaders() map[string]string {
	headers := make(map[string]string)
	for key, values := range h.req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

// UnderlyingRequest returns the underlying *http.Request
// This is useful when you need to pass the request to other handlers
func (h *HTTPRequest) UnderlyingRequest() *http.Request {
	return h.req
}

// HTTPResponseWriter adapts our ResponseWriter interface to standard http.ResponseWriter
type HTTPResponseWriter struct {
	resp   http.ResponseWriter
	w      common.ResponseWriter //nolint:unused
	status int
}

func NewHTTPResponseWriter(w http.ResponseWriter) *HTTPResponseWriter {
	return &HTTPResponseWriter{resp: w}
}

func (h *HTTPResponseWriter) SetHeader(key, value string) {
	h.resp.Header().Set(key, value)
}

func (h *HTTPResponseWriter) WriteHeader(statusCode int) {
	h.status = statusCode
	h.resp.WriteHeader(statusCode)
}

func (h *HTTPResponseWriter) Write(data []byte) (int, error) {
	return h.resp.Write(data)
}

func (h *HTTPResponseWriter) WriteJSON(data interface{}) error {
	h.SetHeader("Content-Type", "application/json")
	return json.NewEncoder(h.resp).Encode(data)
}

// UnderlyingResponseWriter returns the underlying http.ResponseWriter
// This is useful when you need to pass the response writer to other handlers
func (h *HTTPResponseWriter) UnderlyingResponseWriter() http.ResponseWriter {
	return h.resp
}

// StandardMuxAdapter creates routes compatible with standard http.HandlerFunc
type StandardMuxAdapter struct {
	*MuxAdapter
}

func NewStandardMuxAdapter() *StandardMuxAdapter {
	return &StandardMuxAdapter{
		MuxAdapter: NewMuxAdapter(mux.NewRouter()),
	}
}

// RegisterRoute registers a route that works with the existing Handler
func (s *StandardMuxAdapter) RegisterRoute(pattern string, handler func(http.ResponseWriter, *http.Request, map[string]string)) *mux.Route {
	return s.router.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		handler(w, r, vars)
	})
}

// GetMuxRouter returns the underlying mux router for direct access
func (s *StandardMuxAdapter) GetMuxRouter() *mux.Router {
	return s.router
}

// PathParamExtractor extracts path parameters from different router types
type PathParamExtractor interface {
	ExtractParams(*http.Request) map[string]string
}

// MuxParamExtractor extracts parameters from Gorilla Mux
type MuxParamExtractor struct{}

func (m MuxParamExtractor) ExtractParams(r *http.Request) map[string]string {
	return mux.Vars(r)
}

// RouterConfig holds router configuration
type RouterConfig struct {
	PathPrefix     string
	Middleware     []func(http.Handler) http.Handler
	ParamExtractor PathParamExtractor
}

// DefaultRouterConfig returns default router configuration
func DefaultRouterConfig() *RouterConfig {
	return &RouterConfig{
		PathPrefix:     "",
		Middleware:     make([]func(http.Handler) http.Handler, 0),
		ParamExtractor: MuxParamExtractor{},
	}
}
