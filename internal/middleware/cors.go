package middleware

import "net/http"

// CORS returns a middleware that adds cross-origin headers to every response.
// When origin is empty the middleware is a no-op -- safe to call unconditionally.
//
// Usage: set CORS_ORIGIN="*" (or a specific domain) when the frontend is hosted
// on a different domain than the infera API (e.g. Vercel frontend + Koyeb API).
// Leave it unset when the UI is served by infera itself (same origin, no CORS needed).
func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

				// Preflight request -- respond and stop.
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
