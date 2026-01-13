package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
)

// cfVisitor represents the JSON structure in CF-Visitor header
type cfVisitor struct {
	Scheme string `json:"scheme"`
}

// RemoteMetadataMiddleware captures reverse proxy headers (Cloudflare Tunnel)
// and adds them to the request context. Works for all routes.
// It also enforces HTTPS by redirecting HTTP requests to the canonical HTTPS URL
// and sets HSTS headers on HTTPS responses.
func RemoteMetadataMiddleware(exposedDomain string) func(http.Handler) http.Handler {
	// Parse the exposed domain once at initialization for safety and efficiency
	exposedURL, err := url.Parse(exposedDomain)
	if err != nil {
		slog.Error("middleware.remote.invalid_domain",
			"component", "middleware",
			"error", err,
			"domain", exposedDomain,
		)
		// If we can't parse the domain, we can't safely redirect
		// Fall back to a middleware that only adds metadata
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metadata := RemoteMetadata{
					IP:       extractRemoteIP(r),
					Protocol: extractProtocol(r),
					Country:  r.Header.Get("CF-IPCountry"),
				}
				ctx := ContextWithRemote(r.Context(), metadata)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
	}

	canonicalHost := exposedURL.Host

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metadata := RemoteMetadata{
				IP:       extractRemoteIP(r),
				Protocol: extractProtocol(r),
				Country:  r.Header.Get("CF-IPCountry"),
			}

			// Add metadata to context
			ctx := ContextWithRemote(r.Context(), metadata)

			// Only perform protocol-based actions if we can reliably determine the protocol
			// This prevents redirect loops when running without reverse proxy headers
			if metadata.Protocol == "" {
				slog.Warn("middleware.remote.protocol_unknown",
					"component", "middleware",
					"event", "protocol.unknown",
					"message", "Cannot determine protocol - skipping HTTPS redirect and HSTS header",
					"ip", metadata.IP,
					"path", r.URL.Path,
				)
			} else if metadata.Protocol == "http" {
				// Enforce HTTPS by redirecting HTTP requests to canonical domain
				// Build safe redirect URL using url.URL struct
				redirectURL := &url.URL{
					Scheme:   "https",
					Host:     canonicalHost,
					Path:     r.URL.Path,
					RawQuery: r.URL.RawQuery,
					Fragment: r.URL.Fragment,
				}

				slog.Info("middleware.remote.https_redirect",
					"component", "middleware",
					"event", "https.redirect",
					"from_path", r.URL.Path,
					"to", redirectURL.String(),
					"ip", metadata.IP,
				)

				http.Redirect(w, r, redirectURL.String(), http.StatusMovedPermanently)
				return
			} else if metadata.Protocol == "https" {
				// Set HSTS header for HTTPS requests
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
			}

			// Log the remote metadata for debugging
			slog.Debug("middleware.remote.metadata",
				"component", "middleware",
				"event", "remote.captured",
				"ip", metadata.IP,
				"protocol", metadata.Protocol,
				"country", metadata.Country,
				"path", r.URL.Path,
			)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractRemoteIP extracts the client IP from Cloudflare headers or falls back to RemoteAddr
func extractRemoteIP(r *http.Request) string {
	// Try Cloudflare Connecting IP header first
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}

	// Fallback to X-Forwarded-For (standard reverse proxy header)
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}

	// Last resort: use RemoteAddr
	return r.RemoteAddr
}

// extractProtocol extracts the protocol from Cloudflare CF-Visitor header or falls back to X-Forwarded-Proto
// Returns empty string if protocol cannot be determined reliably
func extractProtocol(r *http.Request) string {
	// Try Cloudflare CF-Visitor header first (JSON format)
	if visitorHeader := r.Header.Get("CF-Visitor"); visitorHeader != "" {
		var visitor cfVisitor
		if err := json.Unmarshal([]byte(visitorHeader), &visitor); err == nil {
			return visitor.Scheme
		}
	}

	// Fallback to X-Forwarded-Proto (standard reverse proxy header)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}

	// Determine from TLS state
	if r.TLS != nil {
		return "https"
	}

	// Return empty string if we can't determine the protocol
	// This prevents redirect loops when running without a reverse proxy
	return ""
}
