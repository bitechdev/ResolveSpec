package middleware

import (
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/metrics"
)

const panicMiddlewareMethodName = "PanicMiddleware"

// PanicRecovery is a middleware that recovers from panics, logs the error,
// sends it to an error tracker, records a metric, and returns a 500 Internal Server Error.
func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rcv := recover(); rcv != nil {
				// Record the panic metric
				metrics.GetProvider().RecordPanic(panicMiddlewareMethodName)

				// Log the panic and send to error tracker
				// We pass the request context so the error tracker can potentially
				// link the panic to the request trace.
				ctx := r.Context()
				err := logger.HandlePanic(panicMiddlewareMethodName, rcv, ctx)

				// Respond with a 500 error
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
