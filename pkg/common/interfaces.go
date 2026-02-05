package common

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Database interface designed to work with both GORM and Bun
type Database interface {
	// Core query operations
	NewSelect() SelectQuery
	NewInsert() InsertQuery
	NewUpdate() UpdateQuery
	NewDelete() DeleteQuery

	// Raw SQL execution
	Exec(ctx context.Context, query string, args ...interface{}) (Result, error)
	Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error

	// Transaction support
	BeginTx(ctx context.Context) (Database, error)
	CommitTx(ctx context.Context) error
	RollbackTx(ctx context.Context) error
	RunInTransaction(ctx context.Context, fn func(Database) error) error

	// GetUnderlyingDB returns the underlying database connection
	// For GORM, this returns *gorm.DB
	// For Bun, this returns *bun.DB
	// This is useful for provider-specific features like PostgreSQL NOTIFY/LISTEN
	GetUnderlyingDB() interface{}

	// DriverName returns the canonical name of the underlying database driver.
	// Possible values: "postgres", "sqlite", "mssql", "mysql".
	// All adapters normalise vendor-specific strings (e.g. Bun's "pg", GORM's
	// "sqlserver") to the values above before returning.
	DriverName() string
}

// SelectQuery interface for building SELECT queries (compatible with both GORM and Bun)
type SelectQuery interface {
	Model(model interface{}) SelectQuery
	Table(table string) SelectQuery
	Column(columns ...string) SelectQuery
	ColumnExpr(query string, args ...interface{}) SelectQuery
	Where(query string, args ...interface{}) SelectQuery
	WhereOr(query string, args ...interface{}) SelectQuery
	Join(query string, args ...interface{}) SelectQuery
	LeftJoin(query string, args ...interface{}) SelectQuery
	Preload(relation string, conditions ...interface{}) SelectQuery
	PreloadRelation(relation string, apply ...func(SelectQuery) SelectQuery) SelectQuery
	JoinRelation(relation string, apply ...func(SelectQuery) SelectQuery) SelectQuery
	Order(order string) SelectQuery
	OrderExpr(order string, args ...interface{}) SelectQuery
	Limit(n int) SelectQuery
	Offset(n int) SelectQuery
	Group(group string) SelectQuery
	Having(having string, args ...interface{}) SelectQuery

	// Execution methods
	Scan(ctx context.Context, dest interface{}) error
	ScanModel(ctx context.Context) error
	Count(ctx context.Context) (int, error)
	Exists(ctx context.Context) (bool, error)
}

// InsertQuery interface for building INSERT queries
type InsertQuery interface {
	Model(model interface{}) InsertQuery
	Table(table string) InsertQuery
	Value(column string, value interface{}) InsertQuery
	OnConflict(action string) InsertQuery
	Returning(columns ...string) InsertQuery

	// Execution
	Exec(ctx context.Context) (Result, error)
}

// UpdateQuery interface for building UPDATE queries
type UpdateQuery interface {
	Model(model interface{}) UpdateQuery
	Table(table string) UpdateQuery
	Set(column string, value interface{}) UpdateQuery
	SetMap(values map[string]interface{}) UpdateQuery
	Where(query string, args ...interface{}) UpdateQuery
	Returning(columns ...string) UpdateQuery

	// Execution
	Exec(ctx context.Context) (Result, error)
}

// DeleteQuery interface for building DELETE queries
type DeleteQuery interface {
	Model(model interface{}) DeleteQuery
	Table(table string) DeleteQuery
	Where(query string, args ...interface{}) DeleteQuery

	// Execution
	Exec(ctx context.Context) (Result, error)
}

// Result interface for query execution results
type Result interface {
	RowsAffected() int64
	LastInsertId() (int64, error)
}

// ModelRegistry manages model registration and retrieval
type ModelRegistry interface {
	RegisterModel(name string, model interface{}) error
	GetModel(name string) (interface{}, error)
	GetAllModels() map[string]interface{}
	GetModelByEntity(schema, entity string) (interface{}, error)
}

// Router interface for HTTP router abstraction
type Router interface {
	HandleFunc(pattern string, handler HTTPHandlerFunc) RouteRegistration
	ServeHTTP(w ResponseWriter, r Request)
}

// RouteRegistration allows method chaining for route configuration
type RouteRegistration interface {
	Methods(methods ...string) RouteRegistration
	PathPrefix(prefix string) RouteRegistration
}

// Request interface abstracts HTTP request
type Request interface {
	Method() string
	URL() string
	Header(key string) string
	AllHeaders() map[string]string // Get all headers as a map
	Body() ([]byte, error)
	PathParam(key string) string
	QueryParam(key string) string
	AllQueryParams() map[string]string // Get all query parameters as a map
	UnderlyingRequest() *http.Request  // Get the underlying *http.Request for forwarding to other handlers
}

// ResponseWriter interface abstracts HTTP response
type ResponseWriter interface {
	SetHeader(key, value string)
	WriteHeader(statusCode int)
	Write(data []byte) (int, error)
	WriteJSON(data interface{}) error
	UnderlyingResponseWriter() http.ResponseWriter // Get the underlying http.ResponseWriter for forwarding to other handlers
}

// HTTPHandlerFunc type for HTTP handlers
type HTTPHandlerFunc func(ResponseWriter, Request)

// WrapHTTPRequest wraps standard http.ResponseWriter and *http.Request into common interfaces
func WrapHTTPRequest(w http.ResponseWriter, r *http.Request) (ResponseWriter, Request) {
	return &StandardResponseWriter{w: w}, &StandardRequest{r: r}
}

// StandardResponseWriter adapts http.ResponseWriter to ResponseWriter interface
type StandardResponseWriter struct {
	w      http.ResponseWriter
	status int
}

func (s *StandardResponseWriter) SetHeader(key, value string) {
	s.w.Header().Set(key, value)
}

func (s *StandardResponseWriter) WriteHeader(statusCode int) {
	s.status = statusCode
	s.w.WriteHeader(statusCode)
}

func (s *StandardResponseWriter) Write(data []byte) (int, error) {
	return s.w.Write(data)
}

func (s *StandardResponseWriter) WriteJSON(data interface{}) error {
	s.SetHeader("Content-Type", "application/json")
	return json.NewEncoder(s.w).Encode(data)
}

func (s *StandardResponseWriter) UnderlyingResponseWriter() http.ResponseWriter {
	return s.w
}

// StandardRequest adapts *http.Request to Request interface
type StandardRequest struct {
	r    *http.Request
	body []byte
}

func (s *StandardRequest) Method() string {
	return s.r.Method
}

func (s *StandardRequest) URL() string {
	return s.r.URL.String()
}

func (s *StandardRequest) Header(key string) string {
	return s.r.Header.Get(key)
}

func (s *StandardRequest) AllHeaders() map[string]string {
	headers := make(map[string]string)
	for key, values := range s.r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

func (s *StandardRequest) Body() ([]byte, error) {
	if s.body != nil {
		return s.body, nil
	}
	if s.r.Body == nil {
		return nil, nil
	}
	defer s.r.Body.Close()
	body, err := io.ReadAll(s.r.Body)
	if err != nil {
		return nil, err
	}
	s.body = body
	return body, nil
}

func (s *StandardRequest) PathParam(key string) string {
	// Standard http.Request doesn't have path params
	// This should be set by the router
	return ""
}

func (s *StandardRequest) QueryParam(key string) string {
	return s.r.URL.Query().Get(key)
}

func (s *StandardRequest) AllQueryParams() map[string]string {
	params := make(map[string]string)
	for key, values := range s.r.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}

func (s *StandardRequest) UnderlyingRequest() *http.Request {
	return s.r
}

// TableNameProvider interface for models that provide table names
type TableNameProvider interface {
	TableName() string
}

type TableAliasProvider interface {
	TableAlias() string
}

// PrimaryKeyNameProvider interface for models that provide primary key column names
type PrimaryKeyNameProvider interface {
	GetIDName() string
}

// SchemaProvider interface for models that provide schema names
type SchemaProvider interface {
	SchemaName() string
}

// SpecHandler interface represents common functionality across all spec handlers
// This is the base interface implemented by:
//   - resolvespec.Handler: Handles CRUD operations via request body with explicit operation field
//   - restheadspec.Handler: Handles CRUD operations via HTTP methods (GET/POST/PUT/DELETE)
//   - funcspec.Handler: Handles custom SQL query execution with dynamic parameters
//
// The interface hierarchy is:
//
//	SpecHandler (base)
//	├── CRUDHandler (resolvespec, restheadspec)
//	└── QueryHandler (funcspec)
type SpecHandler interface {
	// GetDatabase returns the underlying database connection
	GetDatabase() Database
}

// CRUDHandler interface for handlers that support CRUD operations
// This is implemented by resolvespec.Handler and restheadspec.Handler
type CRUDHandler interface {
	SpecHandler

	// Handle processes API requests through router-agnostic interface
	Handle(w ResponseWriter, r Request, params map[string]string)

	// HandleGet processes GET requests for metadata
	HandleGet(w ResponseWriter, r Request, params map[string]string)
}

// QueryHandler interface for handlers that execute SQL queries
// This is implemented by funcspec.Handler
// Note: funcspec uses standard http.ResponseWriter and *http.Request instead of common interfaces
type QueryHandler interface {
	SpecHandler
	// Methods are defined in funcspec package due to different function signature requirements
}
