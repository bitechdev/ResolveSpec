package resolvemcp

import (
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// Guard returns middleware that requires an authenticated caller on every request.
//
// The security list's provider decides which credentials are accepted: build it from a
// security.ChainAuthenticator over an OAuth bearer token, a session token (header or cookie)
// and an API key authenticator. The authenticated security.UserContext is placed in the request
// context, which the MCP transports pass on to every tool call, so rules, row security and
// OnTxBegin apply to that caller.
//
// Unlike security.NewAuthMiddleware this guard has no guest or optional mode: it ignores
// security.SkipAuth / security.OptionalAuth markers on the request context, and fails closed
// (500) when no provider is configured.
func Guard(securityList *security.SecurityList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		authed := security.NewAuthHandler(securityList, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uc, ok := security.GetUserContext(r.Context()); !ok || uc == nil {
				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if securityList == nil {
				http.Error(w, "Security provider not configured", http.StatusInternalServerError)
				return
			}
			authed.ServeHTTP(w, r)
		})
	}
}

// requireGuard reports whether securityList can guard a route. Setup helpers use it to refuse
// to mount an endpoint rather than serve it unauthenticated by mistake.
func requireGuard(fn string, securityList *security.SecurityList) bool {
	if securityList == nil || securityList.Provider() == nil {
		logger.Error("resolvemcp.%s: no security provider configured; MCP endpoint NOT mounted", fn)
		return false
	}
	return true
}

func warnUnauthenticated(fn string) {
	logger.Warn("resolvemcp.%s: serving the MCP endpoint WITHOUT authentication; every caller can read and write all registered models", fn)
}
