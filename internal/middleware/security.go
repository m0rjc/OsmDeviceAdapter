package middleware

import (
	"net/http"
	"strings"
)

// SecurityHeadersMiddleware adds security headers to responses.
// It should be applied to admin routes and API endpoints.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking by disallowing embedding in frames
		w.Header().Set("X-Frame-Options", "DENY")

		// Control what information is sent in the Referer header
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy
		// - script-src 'self': Only scripts from our domain
		// - style-src 'self' 'unsafe-inline': Styles from our domain + inline (React may use inline styles)
		// - img-src 'self' data:: Images from our domain + data URIs
		// - connect-src 'self': XHR/fetch only to our domain (API calls)
		// - font-src 'self': Fonts from our domain
		// - object-src 'none': No plugins (Flash, etc.)
		// - base-uri 'self': Base element restricted to our domain
		// - form-action 'self': Form submissions only to our domain
		// - frame-ancestors 'none': No embedding (CSP version of X-Frame-Options)
		// - worker-src 'self': Service workers from our domain
		// - manifest-src 'self': PWA manifest from our domain
		csp := strings.Join([]string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"connect-src 'self'",
			"font-src 'self'",
			"object-src 'none'",
			"base-uri 'self'",
			"form-action 'self'",
			"frame-ancestors 'none'",
			"worker-src 'self'",
			"manifest-src 'self'",
		}, "; ")
		w.Header().Set("Content-Security-Policy", csp)

		// Permissions Policy (formerly Feature-Policy)
		// Restrict access to sensitive browser features
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}
