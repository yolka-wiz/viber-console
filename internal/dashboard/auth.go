package dashboard

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireAPIToken protects every API route except the liveness endpoint.
// An empty token is safe only because startup validation limits that mode to a
// loopback listener.
func RequireAPIToken(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" && !validAPIToken(r, token) {
			w.Header().Set("WWW-Authenticate", `Basic realm="viber-console", charset="UTF-8"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validAPIToken(r *http.Request, token string) bool {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return tokenEqual(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")), token)
	}
	if provided := strings.TrimSpace(r.Header.Get("X-Console-Token")); provided != "" {
		return tokenEqual(provided, token)
	}
	if _, password, ok := r.BasicAuth(); ok {
		return tokenEqual(password, token)
	}
	return false
}

func tokenEqual(provided, expected string) bool {
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
