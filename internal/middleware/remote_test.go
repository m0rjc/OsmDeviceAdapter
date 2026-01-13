package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteMetadataMiddleware_CloudflareHeaders(t *testing.T) {
	// Create a test handler that checks the context
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	// Create request with Cloudflare headers
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("CF-Connecting-IP", "203.0.113.1")
	req.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	req.Header.Set("CF-IPCountry", "GB")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Verify metadata was captured correctly
	if capturedMetadata.IP != "203.0.113.1" {
		t.Errorf("Expected IP '203.0.113.1', got '%s'", capturedMetadata.IP)
	}

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected protocol 'https', got '%s'", capturedMetadata.Protocol)
	}

	if capturedMetadata.Country != "GB" {
		t.Errorf("Expected country 'GB', got '%s'", capturedMetadata.Country)
	}
}

func TestRemoteMetadataMiddleware_XForwardedHeaders(t *testing.T) {
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	// Create request with X-Forwarded headers (no Cloudflare headers)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedMetadata.IP != "198.51.100.1" {
		t.Errorf("Expected IP '198.51.100.1', got '%s'", capturedMetadata.IP)
	}

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected protocol 'https', got '%s'", capturedMetadata.Protocol)
	}
}

func TestRemoteMetadataMiddleware_CloudflarePriority(t *testing.T) {
	// Cloudflare headers should take priority over X-Forwarded headers
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("CF-Connecting-IP", "203.0.113.5")
	req.Header.Set("X-Forwarded-For", "198.51.100.5") // Should be ignored
	req.Header.Set("CF-Visitor", `{"scheme":"https"}`)
	req.Header.Set("X-Forwarded-Proto", "http") // Should be ignored

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedMetadata.IP != "203.0.113.5" {
		t.Errorf("Expected Cloudflare IP to take priority, got '%s'", capturedMetadata.IP)
	}

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected Cloudflare protocol to take priority, got '%s'", capturedMetadata.Protocol)
	}
}

func TestRemoteMetadataMiddleware_InvalidCFVisitor(t *testing.T) {
	// Should fall back to X-Forwarded-Proto if CF-Visitor is invalid JSON
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("CF-Visitor", `invalid json`)
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected fallback to X-Forwarded-Proto, got '%s'", capturedMetadata.Protocol)
	}
}

func TestRemoteMetadataMiddleware_NoHeaders(t *testing.T) {
	// Should use RemoteAddr as fallback for IP when no proxy headers present
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.0.2.1:54321"
	// Set HTTPS protocol to avoid redirect and allow handler to run
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedMetadata.IP != "192.0.2.1:54321" {
		t.Errorf("Expected RemoteAddr as fallback, got '%s'", capturedMetadata.IP)
	}

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected https protocol from header, got '%s'", capturedMetadata.Protocol)
	}
}

func TestRemoteMetadataMiddleware_TLS(t *testing.T) {
	// Should detect HTTPS from TLS state when no headers present
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	req := httptest.NewRequest("GET", "https://example.com/test", nil)
	// httptest.NewRequest with https:// scheme automatically sets TLS

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedMetadata.Protocol != "https" {
		t.Errorf("Expected https protocol from TLS state, got '%s'", capturedMetadata.Protocol)
	}
}

func TestRemoteMetadataMiddleware_HTTPSRedirect(t *testing.T) {
	// Should redirect HTTP requests to HTTPS with canonical domain
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for HTTP redirect")
	})

	handler := RemoteMetadataMiddleware("https://canonical.example.com")(testHandler)

	req := httptest.NewRequest("GET", "/api/v1/patrols?section=123", nil)
	req.Header.Set("X-Forwarded-Proto", "http")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check redirect status code
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status %d, got %d", http.StatusMovedPermanently, w.Code)
	}

	// Check redirect location
	expectedLocation := "https://canonical.example.com/api/v1/patrols?section=123"
	location := w.Header().Get("Location")
	if location != expectedLocation {
		t.Errorf("Expected Location '%s', got '%s'", expectedLocation, location)
	}
}

func TestRemoteMetadataMiddleware_HSTSHeader(t *testing.T) {
	// Should set HSTS header for HTTPS requests
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check HSTS header is set
	hsts := w.Header().Get("Strict-Transport-Security")
	expectedHSTS := "max-age=31536000; includeSubDomains; preload"
	if hsts != expectedHSTS {
		t.Errorf("Expected HSTS header '%s', got '%s'", expectedHSTS, hsts)
	}
}

func TestRemoteMetadataMiddleware_NoHSTSOnHTTP(t *testing.T) {
	// Should NOT set HSTS header for HTTP requests (after redirect)
	// This test uses a fallback middleware that doesn't redirect (invalid domain)
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Use invalid domain to get fallback middleware without redirect
	handler := RemoteMetadataMiddleware("://invalid")(testHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-Proto", "http")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check HSTS header is NOT set
	hsts := w.Header().Get("Strict-Transport-Security")
	if hsts != "" {
		t.Errorf("Expected no HSTS header for HTTP, got '%s'", hsts)
	}
}

func TestRemoteMetadataMiddleware_RedirectPreservesQueryAndFragment(t *testing.T) {
	// Should preserve query parameters and fragment in redirect
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for HTTP redirect")
	})

	handler := RemoteMetadataMiddleware("https://example.com:8443")(testHandler)

	req := httptest.NewRequest("GET", "/path/to/resource?foo=bar&baz=qux", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	req.URL.Fragment = "section"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check redirect preserves everything
	expectedLocation := "https://example.com:8443/path/to/resource?foo=bar&baz=qux#section"
	location := w.Header().Get("Location")
	if location != expectedLocation {
		t.Errorf("Expected Location '%s', got '%s'", expectedLocation, location)
	}
}

func TestRemoteMetadataMiddleware_UnknownProtocol(t *testing.T) {
	// Should NOT redirect or set HSTS when protocol cannot be determined
	// This prevents redirect loops
	var capturedMetadata RemoteMetadata
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMetadata = RemoteFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RemoteMetadataMiddleware("https://example.com")(testHandler)

	// Request with no protocol headers and no TLS
	req := httptest.NewRequest("GET", "/test", nil)
	// Don't set any protocol headers - protocol will be empty string

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should NOT redirect
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d (no redirect), got %d", http.StatusOK, w.Code)
	}

	// Should NOT set HSTS header
	hsts := w.Header().Get("Strict-Transport-Security")
	if hsts != "" {
		t.Errorf("Expected no HSTS header when protocol unknown, got '%s'", hsts)
	}

	// Protocol should be empty
	if capturedMetadata.Protocol != "" {
		t.Errorf("Expected empty protocol, got '%s'", capturedMetadata.Protocol)
	}
}
