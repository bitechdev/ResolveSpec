package router

import (
	"net/http"

	"github.com/uptrace/bunrouter"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// BunRouterAdapter adapts uptrace/bunrouter to work with our Router interface
type BunRouterAdapter struct {
	router *bunrouter.Router
}

// NewBunRouterAdapter creates a new bunrouter adapter
func NewBunRouterAdapter(router *bunrouter.Router) *BunRouterAdapter {
	return &BunRouterAdapter{router: router}
}

// NewBunRouterAdapterDefault creates a new bunrouter adapter with default router
func NewBunRouterAdapterDefault() *BunRouterAdapter {
	return &BunRouterAdapter{router: bunrouter.New()}
}

func (b *BunRouterAdapter) HandleFunc(pattern string, handler common.HTTPHandlerFunc) common.RouteRegistration {
	route := &BunRouterRegistration{
		router:  b.router,
		pattern: pattern,
		handler: handler,
	}
	return route
}

func (b *BunRouterAdapter) ServeHTTP(w common.ResponseWriter, r common.Request) {
	// This method would be used when we need to serve through our interface
	// For now, we'll work directly with the underlying router
	panic("ServeHTTP not implemented - use GetBunRouter() for direct access")
}

// GetBunRouter returns the underlying bunrouter for direct access
func (b *BunRouterAdapter) GetBunRouter() *bunrouter.Router {
	return b.router
}

// BunRouterRegistration implements RouteRegistration for bunrouter
type BunRouterRegistration struct {
	router  *bunrouter.Router
	pattern string
	handler common.HTTPHandlerFunc
}

func (b *BunRouterRegistration) Methods(methods ...string) common.RouteRegistration {
	// bunrouter handles methods differently - we'll register for each method
	for _, method := range methods {
		b.router.Handle(method, b.pattern, func(w http.ResponseWriter, req bunrouter.Request) error {
			// Convert bunrouter.Request to our BunRouterRequest
			reqAdapter := &BunRouterRequest{req: req}
			respAdapter := &HTTPResponseWriter{resp: w}
			b.handler(respAdapter, reqAdapter)
			return nil
		})
	}
	return b
}

func (b *BunRouterRegistration) PathPrefix(prefix string) common.RouteRegistration {
	// bunrouter doesn't have PathPrefix like mux, but we can modify the pattern
	newPattern := prefix + b.pattern
	b.pattern = newPattern
	return b
}

// BunRouterRequest adapts bunrouter.Request to our Request interface
type BunRouterRequest struct {
	req  bunrouter.Request
	body []byte
}

// NewBunRouterRequest creates a new BunRouterRequest adapter
func NewBunRouterRequest(req bunrouter.Request) *BunRouterRequest {
	return &BunRouterRequest{req: req}
}

func (b *BunRouterRequest) Method() string {
	return b.req.Method
}

func (b *BunRouterRequest) URL() string {
	return b.req.URL.String()
}

func (b *BunRouterRequest) Header(key string) string {
	return b.req.Header.Get(key)
}

func (b *BunRouterRequest) Body() ([]byte, error) {
	if b.body != nil {
		return b.body, nil
	}

	if b.req.Body == nil {
		return nil, nil
	}

	// Create HTTPRequest adapter and use its Body() method
	httpAdapter := NewHTTPRequest(b.req.Request)
	body, err := httpAdapter.Body()
	if err != nil {
		return nil, err
	}
	b.body = body
	return body, nil
}

func (b *BunRouterRequest) PathParam(key string) string {
	return b.req.Param(key)
}

func (b *BunRouterRequest) QueryParam(key string) string {
	return b.req.URL.Query().Get(key)
}

func (b *BunRouterRequest) AllQueryParams() map[string]string {
	params := make(map[string]string)
	for key, values := range b.req.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}

func (b *BunRouterRequest) AllHeaders() map[string]string {
	headers := make(map[string]string)
	for key, values := range b.req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

// UnderlyingRequest returns the underlying *http.Request
// This is useful when you need to pass the request to other handlers
func (b *BunRouterRequest) UnderlyingRequest() *http.Request {
	return b.req.Request
}

// StandardBunRouterAdapter creates routes compatible with standard bunrouter handlers
type StandardBunRouterAdapter struct {
	*BunRouterAdapter
}

func NewStandardBunRouterAdapter() *StandardBunRouterAdapter {
	return &StandardBunRouterAdapter{
		BunRouterAdapter: NewBunRouterAdapterDefault(),
	}
}

// RegisterRoute registers a route that works with the existing Handler
func (s *StandardBunRouterAdapter) RegisterRoute(method, pattern string, handler func(http.ResponseWriter, *http.Request, map[string]string)) {
	s.router.Handle(method, pattern, func(w http.ResponseWriter, req bunrouter.Request) error {
		// Extract path parameters
		params := make(map[string]string)

		// bunrouter doesn't provide a direct way to get all params
		// You would typically access them individually with req.Param("name")
		// For this example, we'll create the map based on the request context

		handler(w, req.Request, params)
		return nil
	})
}

// RegisterRouteWithParams registers a route with explicit parameter extraction
func (s *StandardBunRouterAdapter) RegisterRouteWithParams(method, pattern string, paramNames []string, handler func(http.ResponseWriter, *http.Request, map[string]string)) {
	s.router.Handle(method, pattern, func(w http.ResponseWriter, req bunrouter.Request) error {
		// Extract specified path parameters
		params := make(map[string]string)
		for _, paramName := range paramNames {
			params[paramName] = req.Param(paramName)
		}

		handler(w, req.Request, params)
		return nil
	})
}

// BunRouterConfig holds bunrouter-specific configuration
type BunRouterConfig struct {
	UseStrictSlash         bool
	RedirectTrailingSlash  bool
	HandleMethodNotAllowed bool
	HandleOPTIONS          bool
	GlobalOPTIONS          http.Handler
	GlobalMethodNotAllowed http.Handler
	PanicHandler           func(http.ResponseWriter, *http.Request, interface{})
}

// DefaultBunRouterConfig returns default bunrouter configuration
func DefaultBunRouterConfig() *BunRouterConfig {
	return &BunRouterConfig{
		UseStrictSlash:         false,
		RedirectTrailingSlash:  true,
		HandleMethodNotAllowed: true,
		HandleOPTIONS:          true,
	}
}
