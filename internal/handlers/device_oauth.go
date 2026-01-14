package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/m0rjc/OsmDeviceAdapter/internal/middleware"
)

type DeviceAuthorizationRequest struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
}

type DeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	VerificationURIShort    string `json:"verification_uri_short"` // Short URL for QR codes
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type DeviceTokenRequest struct {
	GrantType  string `json:"grant_type"`
	DeviceCode string `json:"device_code"`
	ClientID   string `json:"client_id"`
}

type DeviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type DeviceTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func DeviceAuthorizeHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get client IP from context
		remoteMetadata := middleware.RemoteFromContext(r.Context())
		clientIP := remoteMetadata.IP

		// Check rate limit (6 requests per minute per IP)
		rateLimitKey := fmt.Sprintf("%s:/device/authorize", clientIP)
		rateLimitResult, err := deps.Conns.GetRateLimiter().CheckRateLimit(
			r.Context(),
			"device_authorize",
			rateLimitKey,
			int64(deps.Config.RateLimit.DeviceAuthorizeRateLimit),
			time.Minute,
		)
		if err != nil {
			slog.Error("device.authorize.rate_limit_error",
				"component", "device_oauth",
				"event", "authorize.rate_limit_error",
				"client_ip", clientIP,
				"error", err,
			)
			// Continue on rate limit check error - don't block legitimate requests
		} else if !rateLimitResult.Allowed {
			slog.Warn("device.authorize.rate_limited",
				"component", "device_oauth",
				"event", "authorize.rate_limited",
				"client_ip", clientIP,
				"remaining", rateLimitResult.Remaining,
				"retry_after", rateLimitResult.RetryAfter.Seconds(),
			)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(rateLimitResult.RetryAfter.Seconds())))
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", deps.Config.RateLimit.DeviceAuthorizeRateLimit))
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		var req DeviceAuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.ClientID == "" {
			http.Error(w, "client_id is required", http.StatusBadRequest)
			return
		}

		// Validate client ID against database
		allowed, allowedClientID, err := db.IsClientIDAllowed(deps.Conns, req.ClientID)
		if err != nil {
			slog.Error("device.authorize.db_error",
				"component", "device_oauth",
				"event", "authorize.error",
				"client_id", req.ClientID,
				"error", err,
			)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			slog.Warn("device.authorize.denied",
				"component", "device_oauth",
				"event", "authorize.denied",
				"client_id", req.ClientID,
				"reason", "invalid_client_id",
				"remote_addr", r.RemoteAddr,
			)
			metrics.DeviceAuthRequests.WithLabelValues(req.ClientID, "denied").Inc()
			http.Error(w, "invalid client_id", http.StatusUnauthorized)
			return
		}

		// Generate device code and user code
		deviceCode, err := generateRandomString(32)
		if err != nil {
			slog.Error("device.authorize.code_generation_failed",
				"component", "device_oauth",
				"event", "authorize.error",
				"client_id", req.ClientID,
				"error", err,
			)
			http.Error(w, "Failed to generate device code", http.StatusInternalServerError)
			return
		}

		userCode, err := generateUserCode()
		if err != nil {
			slog.Error("device.authorize.user_code_generation_failed",
				"component", "device_oauth",
				"event", "authorize.error",
				"client_id", req.ClientID,
				"error", err,
			)
			http.Error(w, "Failed to generate user code", http.StatusInternalServerError)
			return
		}

		// Store in database
		expiresAt := time.Now().Add(time.Duration(deps.Config.DeviceOAuth.DeviceCodeExpiry) * time.Second)
		now := time.Now()
		deviceCodeRecord := &db.DeviceCode{
			DeviceCode:           deviceCode,
			UserCode:             userCode,
			ClientID:             req.ClientID,
			CreatedByID:          &allowedClientID,
			ExpiresAt:            expiresAt,
			Status:               "pending",
			CreatedAt:            now,
			DeviceRequestIP:      &remoteMetadata.IP,
			DeviceRequestCountry: &remoteMetadata.Country,
			DeviceRequestTime:    &now,
		}
		if err := db.CreateDeviceCode(deps.Conns, deviceCodeRecord); err != nil {
			slog.Error("device.authorize.db_store_failed",
				"component", "device_oauth",
				"event", "authorize.error",
				"client_id", req.ClientID,
				"user_code", userCode,
				"error", err,
			)
			http.Error(w, "Failed to store device code", http.StatusInternalServerError)
			return
		}

		// Build verification URLs using configurable path prefix
		verificationURI := fmt.Sprintf("%s%s", deps.Config.ExternalDomains.ExposedDomain, deps.Config.Paths.DevicePrefix)
		verificationURIComplete := fmt.Sprintf("%s%s?user_code=%s", deps.Config.ExternalDomains.ExposedDomain, deps.Config.Paths.DevicePrefix, userCode)
		// Short URL for QR codes (no hyphen in user code)
		userCodeNoHyphen := strings.ReplaceAll(userCode, "-", "")
		verificationURIShort := fmt.Sprintf("%s/d/%s", deps.Config.ExternalDomains.ExposedDomain, userCodeNoHyphen)

		slog.Info("device.authorize.success",
			"component", "device_oauth",
			"event", "authorize.start",
			"client_id", req.ClientID,
			"user_code", userCode,
			"device_code_hash", fmt.Sprintf("%s...", deviceCode[:8]), // Log truncated for security
			"expires_in", deps.Config.DeviceOAuth.DeviceCodeExpiry,
		)
		metrics.DeviceAuthRequests.WithLabelValues(req.ClientID, "success").Inc()

		response := DeviceAuthorizationResponse{
			DeviceCode:              deviceCode,
			UserCode:                userCode,
			VerificationURI:         verificationURI,
			VerificationURIComplete: verificationURIComplete,
			VerificationURIShort:    verificationURIShort,
			ExpiresIn:               deps.Config.DeviceOAuth.DeviceCodeExpiry,
			Interval:                deps.Config.DeviceOAuth.DevicePollInterval,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func DeviceTokenHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req DeviceTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendTokenError(w, "invalid_request", "Invalid request body")
			return
		}

		if req.GrantType != "urn:ietf:params:oauth:grant-type:device_code" {
			sendTokenError(w, "unsupported_grant_type", "")
			return
		}

		if req.DeviceCode == "" {
			sendTokenError(w, "invalid_request", "device_code is required")
			return
		}

		// Enforce slow_down - client must not poll faster than the configured interval
		pollInterval := time.Duration(deps.Config.DeviceOAuth.DevicePollInterval) * time.Second
		pollKey := fmt.Sprintf("device_token_poll:%s", req.DeviceCode)

		// Check if polling too fast using rate limiter (1 request per poll interval)
		pollResult, err := deps.Conns.GetRateLimiter().CheckRateLimit(
			r.Context(),
			"device_token_poll",
			pollKey,
			1, // Only 1 request allowed
			pollInterval,
		)

		if err != nil {
			slog.Error("device.token.poll_check_error",
				"component", "device_oauth",
				"event", "token.poll_check_error",
				"client_id", req.ClientID,
				"error", err,
			)
			// Continue on error - don't block legitimate requests
		} else if !pollResult.Allowed {
			slog.Debug("device.token.slow_down",
				"component", "device_oauth",
				"event", "token.slow_down",
				"client_id", req.ClientID,
				"device_code_hash", fmt.Sprintf("%s...", req.DeviceCode[:8]),
				"retry_after", pollResult.RetryAfter.Seconds(),
			)
			sendTokenError(w, "slow_down", fmt.Sprintf("Polling too fast. Please wait at least %d seconds between requests.", deps.Config.DeviceOAuth.DevicePollInterval))
			return
		}

		// Look up device code
		deviceCodeRecord, err := db.FindDeviceCodeByCode(deps.Conns, req.DeviceCode)
		if err != nil {
			slog.Error("device.token.db_error",
				"component", "device_oauth",
				"event", "token.error",
				"client_id", req.ClientID,
				"error", err,
			)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if deviceCodeRecord == nil {
			slog.Warn("device.token.invalid_code",
				"component", "device_oauth",
				"event", "token.error",
				"client_id", req.ClientID,
				"error", "invalid_device_code",
			)
			sendTokenError(w, "invalid_grant", "Invalid device code")
			return
		}

		// Check if expired
		if time.Now().After(deviceCodeRecord.ExpiresAt) {
			slog.Info("device.token.expired",
				"component", "device_oauth",
				"event", "token.expired",
				"client_id", deviceCodeRecord.ClientID,
				"user_code", deviceCodeRecord.UserCode,
			)
			sendTokenError(w, "expired_token", "Device code has expired")
			return
		}

		// Check status
		switch deviceCodeRecord.Status {
		case "pending", "awaiting_section":
			slog.Debug("device.token.pending",
				"component", "device_oauth",
				"event", "token.pending",
				"client_id", deviceCodeRecord.ClientID,
				"user_code", deviceCodeRecord.UserCode,
				"status", deviceCodeRecord.Status,
			)
			sendTokenError(w, "authorization_pending", "User has not yet authorized")
			return
		case "denied":
			slog.Info("device.token.denied",
				"component", "device_oauth",
				"event", "token.denied",
				"client_id", deviceCodeRecord.ClientID,
				"user_code", deviceCodeRecord.UserCode,
			)
			metrics.DeviceAuthRequests.WithLabelValues(deviceCodeRecord.ClientID, "user_denied").Inc()
			sendTokenError(w, "access_denied", "User denied authorization")
			return
		case "authorized":
			// Return the device access token (not the OSM token)
			if deviceCodeRecord.DeviceAccessToken == nil {
				slog.Error("device.token.missing_token",
					"component", "device_oauth",
					"event", "token.error",
					"client_id", deviceCodeRecord.ClientID,
					"user_code", deviceCodeRecord.UserCode,
					"error", "device_access_token_missing",
				)
				http.Error(w, "Token not available", http.StatusInternalServerError)
				return
			}

			// Device tokens don't expire, but we still track OSM token expiry server-side
			// For client compatibility, we can return a long expiration time
			expiresIn := 0
			if deviceCodeRecord.OSMTokenExpiry != nil {
				expiresIn = int(time.Until(*deviceCodeRecord.OSMTokenExpiry).Seconds())
			}

			slog.Info("device.token.issued",
				"component", "device_oauth",
				"event", "token.issued",
				"client_id", deviceCodeRecord.ClientID,
				"user_code", deviceCodeRecord.UserCode,
				"expires_in", expiresIn,
			)
			metrics.DeviceAuthRequests.WithLabelValues(deviceCodeRecord.ClientID, "authorized").Inc()

			response := DeviceTokenResponse{
				AccessToken: *deviceCodeRecord.DeviceAccessToken,
				TokenType:   "Bearer",
				ExpiresIn:   expiresIn,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		default:
			slog.Error("device.token.unknown_status",
				"component", "device_oauth",
				"event", "token.error",
				"client_id", deviceCodeRecord.ClientID,
				"user_code", deviceCodeRecord.UserCode,
				"status", deviceCodeRecord.Status,
			)
			http.Error(w, "Unknown status", http.StatusInternalServerError)
			return
		}
	}
}

func sendTokenError(w http.ResponseWriter, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(DeviceTokenErrorResponse{
		Error:            errorCode,
		ErrorDescription: description,
	})
}

func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// generateDeviceAccessToken generates a cryptographically secure random token
// for device authentication. This token is returned to the device and used
// instead of the OSM access token.
func generateDeviceAccessToken() (string, error) {
	// 64 bytes of randomness = 512 bits, more than sufficient for security
	bytes := make([]byte, 64)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Use base64 URL encoding (no padding) for a clean token
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateUserCode() (string, error) {
	// Base20: No vowels (prevents accidental words), no ambiguous chars. RFC-8628
	const charset = "BCDFGHJKLMNPQRSTVWXZ"
	const codeLength = 8

	var code strings.Builder
	max := big.NewInt(int64(len(charset)))

	for i := 0; i < codeLength; i++ {
		// Use crypto/rand.Int to avoid modulo bias
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		code.WriteByte(charset[idx.Int64()])
	}

	raw := code.String()
	// Returns format: XXXX-XXXX
	return fmt.Sprintf("%s-%s", raw[:4], raw[4:]), nil
}

// ShortCodeRedirectHandler handles short URL redirects from /d/{code} to /device?user_code={code}
// This provides shorter URLs suitable for QR codes on small displays
func ShortCodeRedirectHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract code from path
		code := strings.TrimPrefix(r.URL.Path, "/d/")
		if code == "" || code == r.URL.Path {
			http.Error(w, "Invalid short code", http.StatusBadRequest)
			return
		}

		// Remove any remaining slashes or invalid characters
		code = strings.Trim(code, "/")
		if len(code) != 8 { // Expecting 8 characters without hyphen
			http.Error(w, "Invalid short code format", http.StatusBadRequest)
			return
		}

		// Reformat code with hyphen: MRHQTDY4 -> MRHQ-TDY4
		formattedCode := fmt.Sprintf("%s-%s", code[:4], code[4:])

		// Build redirect URL using configurable path prefix
		redirectURL := fmt.Sprintf("%s?user_code=%s", deps.Config.Paths.DevicePrefix, formattedCode)

		slog.Info("device.short_redirect",
			"component", "device_oauth",
			"event", "short_redirect",
			"short_code", code,
			"formatted_code", formattedCode,
		)

		// Redirect to full URL
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}
	return ""
}
