package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
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

		var req DeviceAuthorizationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.ClientID == "" {
			http.Error(w, "client_id is required", http.StatusBadRequest)
			return
		}

		// Validate client ID
		if !isClientIDAllowed(req.ClientID, deps.Config.AllowedClientIDs) {
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
		expiresAt := time.Now().Add(time.Duration(deps.Config.DeviceCodeExpiry) * time.Second)
		deviceCodeRecord := &db.DeviceCode{
			DeviceCode: deviceCode,
			UserCode:   userCode,
			ClientID:   req.ClientID,
			ExpiresAt:  expiresAt,
			Status:     "pending",
			CreatedAt:  time.Now(),
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

		// Build verification URLs
		verificationURI := fmt.Sprintf("%s/device", deps.Config.ExposedDomain)
		verificationURIComplete := fmt.Sprintf("%s/device?user_code=%s", deps.Config.ExposedDomain, userCode)

		slog.Info("device.authorize.success",
			"component", "device_oauth",
			"event", "authorize.start",
			"client_id", req.ClientID,
			"user_code", userCode,
			"device_code_hash", fmt.Sprintf("%s...", deviceCode[:8]), // Log truncated for security
			"expires_in", deps.Config.DeviceCodeExpiry,
		)
		metrics.DeviceAuthRequests.WithLabelValues(req.ClientID, "success").Inc()

		response := DeviceAuthorizationResponse{
			DeviceCode:              deviceCode,
			UserCode:                userCode,
			VerificationURI:         verificationURI,
			VerificationURIComplete: verificationURIComplete,
			ExpiresIn:               deps.Config.DeviceCodeExpiry,
			Interval:                deps.Config.DevicePollInterval,
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
			// Return the tokens
			if deviceCodeRecord.OSMAccessToken == nil {
				slog.Error("device.token.missing_token",
					"component", "device_oauth",
					"event", "token.error",
					"client_id", deviceCodeRecord.ClientID,
					"user_code", deviceCodeRecord.UserCode,
					"error", "osm_access_token_missing",
				)
				http.Error(w, "Token not available", http.StatusInternalServerError)
				return
			}

			expiresIn := 0
			if deviceCodeRecord.OSMTokenExpiry != nil {
				expiresIn = int(time.Until(*deviceCodeRecord.OSMTokenExpiry).Seconds())
			}

			refreshToken := ""
			if deviceCodeRecord.OSMRefreshToken != nil {
				refreshToken = *deviceCodeRecord.OSMRefreshToken
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
				AccessToken:  *deviceCodeRecord.OSMAccessToken,
				TokenType:    "Bearer",
				ExpiresIn:    expiresIn,
				RefreshToken: refreshToken,
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

func generateUserCode() (string, error) {
	// Generate a user-friendly code (e.g., ABCD-EFGH format)
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Avoid ambiguous characters
	const codeLength = 8

	bytes := make([]byte, codeLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	code := make([]byte, codeLength)
	for i, b := range bytes {
		code[i] = charset[int(b)%len(charset)]
	}

	// Format as XXXX-XXXX
	return fmt.Sprintf("%s-%s", string(code[:4]), string(code[4:])), nil
}

func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}
	return ""
}

func isClientIDAllowed(clientID string, allowedClientIDs []string) bool {
	for _, allowed := range allowedClientIDs {
		if clientID == allowed {
			return true
		}
	}
	return false
}
