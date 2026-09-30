package tracing

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

const (
	// DefaultSampleRate is used when Config.SampleRate is zero.
	DefaultSampleRate = 0.1
	// DefaultAttributeValueLimit caps the length of any span attribute value.
	DefaultAttributeValueLimit = 1024
	// DefaultInitTimeout bounds exporter and resource creation.
	DefaultInitTimeout = 10 * time.Second

	unmatchedRoute = "<unmatched>"
)

var (
	tracer      atomic.Pointer[trace.Tracer]
	initMu      sync.Mutex
	initialized bool
)

// Config holds tracing configuration
type Config struct {
	ServiceName    string
	ServiceVersion string
	Endpoint       string // OTLP endpoint (e.g., "localhost:4317")
	Enabled        bool

	// Insecure exports traces over plaintext gRPC. TLS is used unless this is set.
	Insecure bool
	// TLSConfig customises TLS for the exporter. Nil uses the system roots. Ignored when Insecure.
	TLSConfig *tls.Config
	// Headers are sent with every export request (e.g. an authorization token).
	Headers map[string]string

	// SampleRate is the fraction of new root traces sampled (0 < rate <= 1).
	// Zero selects DefaultSampleRate. Upstream sampling decisions are respected.
	SampleRate float64
	// AttributeValueLimit caps span attribute value length. Zero selects DefaultAttributeValueLimit.
	AttributeValueLimit int
	// InitTimeout bounds exporter/resource creation. Zero selects DefaultInitTimeout.
	InitTimeout time.Duration
}

// InitTracer initializes the OpenTelemetry tracer using a background context.
func InitTracer(config Config) (func(context.Context) error, error) {
	return InitTracerContext(context.Background(), config)
}

// InitTracerContext initializes the OpenTelemetry tracer. ctx bounds startup work.
// Calling it again while a tracer is active returns an error; call the returned
// shutdown function first.
func InitTracerContext(ctx context.Context, config Config) (func(context.Context) error, error) {
	if !config.Enabled {
		// Return no-op shutdown function
		return func(context.Context) error { return nil }, nil
	}

	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return nil, errors.New("tracing already initialized; shut down the previous tracer first")
	}

	if config.SampleRate < 0 || config.SampleRate > 1 {
		return nil, fmt.Errorf("invalid tracing sample rate %v: must be within [0, 1]", config.SampleRate)
	}
	rate := config.SampleRate
	if rate == 0 {
		rate = DefaultSampleRate
	}
	valueLimit := config.AttributeValueLimit
	if valueLimit <= 0 {
		valueLimit = DefaultAttributeValueLimit
	}
	timeout := config.InitTimeout
	if timeout <= 0 {
		timeout = DefaultInitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create OTLP exporter
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(config.Endpoint)}
	if config.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		tlsCfg := config.TLSConfig
		if tlsCfg == nil {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)))
	}
	if len(config.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
	}

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.ServiceVersion),
		),
	)
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	limits := sdktrace.NewSpanLimits()
	limits.AttributeValueLengthLimit = valueLimit
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))),
		sdktrace.WithRawSpanLimits(limits),
	)

	// Set global trace provider
	otel.SetTracerProvider(tp)

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Get tracer
	t := tp.Tracer(config.ServiceName)
	tracer.Store(&t)
	initialized = true

	// Return shutdown function
	return func(ctx context.Context) error {
		initMu.Lock()
		tracer.Store(nil)
		initialized = false
		initMu.Unlock()
		return tp.Shutdown(ctx)
	}, nil
}

func currentTracer() trace.Tracer {
	if p := tracer.Load(); p != nil {
		return *p
	}
	return nil
}

// statusRecorder captures the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush preserves streaming support.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware returns an HTTP middleware that creates spans for requests. The span
// name uses the matched route pattern (http.ServeMux's Request.Pattern); use
// MiddlewareWithRoute for other routers. It should be installed inside the panic
// recovery middleware: panics are recorded on the span and re-raised.
func Middleware(next http.Handler) http.Handler {
	return MiddlewareWithRoute(nil)(next)
}

// MiddlewareWithRoute is like Middleware but resolves the low-cardinality route
// template with fn, called after the handler ran (so the router has matched).
// A nil fn falls back to Request.Pattern.
func MiddlewareWithRoute(fn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := currentTracer()
			if t == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Extract context from request headers
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span; the query string is deliberately not exported and the
			// name is refined to the route template once routing has happened.
			ctx, span := t.Start(ctx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPath(r.URL.Path),
					semconv.URLScheme(requestScheme(r)),
				),
			)
			rec := &statusRecorder{ResponseWriter: w}
			// Create new request with updated context
			r = r.WithContext(ctx)

			defer func() {
				route := unmatchedRoute
				if fn != nil {
					if v := fn(r); v != "" {
						route = v
					}
				} else if r.Pattern != "" {
					route = r.Pattern
				}
				if p := recover(); p != nil {
					span.RecordError(fmt.Errorf("panic: %v", p))
					span.SetStatus(codes.Error, "panic")
					span.SetAttributes(semconv.HTTPResponseStatusCode(http.StatusInternalServerError))
					span.SetName(r.Method + " " + route)
					span.End()
					panic(p)
				}
				status := rec.status
				if status == 0 {
					status = http.StatusOK
				}
				span.SetName(r.Method + " " + route)
				span.SetAttributes(semconv.HTTPRoute(route), semconv.HTTPResponseStatusCode(status))
				if status >= 500 {
					span.SetStatus(codes.Error, http.StatusText(status))
				}
				span.End()
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

func requestScheme(r *http.Request) string {
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// StartSpan starts a new span with the given name
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	t := currentTracer()
	if t == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.Start(ctx, name, trace.WithAttributes(attrs...))
}

// SpanFromContext returns the current span from the context
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddEvent adds an event to the current span
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets attributes on the current span
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordError records an error on the current span
func RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
}
