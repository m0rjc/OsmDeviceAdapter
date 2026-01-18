package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	// Create a simple handler that the middleware will wrap
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with security headers middleware
	handler := SecurityHeadersMiddleware(innerHandler)

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()

	// Call the handler
	handler.ServeHTTP(rec, req)

	// Verify status code
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Test required security headers
	tests := []struct {
		header   string
		expected string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := rec.Header().Get(tt.header)
			if got != tt.expected {
				t.Errorf("header %s: expected %q, got %q", tt.header, tt.expected, got)
			}
		})
	}

	// Test CSP header contains required directives
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header is missing")
	} else {
		requiredDirectives := []string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"connect-src 'self'",
			"object-src 'none'",
			"frame-ancestors 'none'",
			"worker-src 'self'",
		}

		for _, directive := range requiredDirectives {
			if !strings.Contains(csp, directive) {
				t.Errorf("CSP missing directive: %s", directive)
			}
		}
	}

	// Test Permissions-Policy header
	permPolicy := rec.Header().Get("Permissions-Policy")
	if permPolicy == "" {
		t.Error("Permissions-Policy header is missing")
	} else {
		if !strings.Contains(permPolicy, "geolocation=()") {
			t.Error("Permissions-Policy should disable geolocation")
		}
		if !strings.Contains(permPolicy, "camera=()") {
			t.Error("Permissions-Policy should disable camera")
		}
	}
}

func TestSecurityHeadersMiddleware_PassesRequest(t *testing.T) {
	// Verify that the middleware passes the request through to the inner handler
	called := false
	innerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Check that request is intact
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/admin/scores" {
			t.Errorf("expected path /api/admin/scores, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	})

	handler := SecurityHeadersMiddleware(innerHandler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/scores", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler was not called")
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rec.Code)
	}
}
