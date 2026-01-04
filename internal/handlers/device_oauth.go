package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
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

		// Generate device code and user code
		deviceCode, err := generateRandomString(32)
		if err != nil {
			http.Error(w, "Failed to generate device code", http.StatusInternalServerError)
			return
		}

		userCode, err := generateUserCode()
		if err != nil {
			http.Error(w, "Failed to generate user code", http.StatusInternalServerError)
			return
		}

		// Store in database
		expiresAt := time.Now().Add(time.Duration(deps.Config.DeviceCodeExpiry) * time.Second)
		_, err = deps.DB.Exec(`
			INSERT INTO device_codes (device_code, user_code, client_id, expires_at, status)
			VALUES ($1, $2, $3, $4, 'pending')
		`, deviceCode, userCode, req.ClientID, expiresAt)
		if err != nil {
			http.Error(w, "Failed to store device code", http.StatusInternalServerError)
			return
		}

		// Build verification URLs
		verificationURI := fmt.Sprintf("%s/device", deps.Config.ExposedDomain)
		verificationURIComplete := fmt.Sprintf("%s/device?user_code=%s", deps.Config.ExposedDomain, userCode)

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
		var status string
		var osmAccessToken, osmRefreshToken sql.NullString
		var osmTokenExpiry sql.NullTime
		var expiresAt time.Time

		err := deps.DB.QueryRow(`
			SELECT status, osm_access_token, osm_refresh_token, osm_token_expiry, expires_at
			FROM device_codes
			WHERE device_code = $1
		`, req.DeviceCode).Scan(&status, &osmAccessToken, &osmRefreshToken, &osmTokenExpiry, &expiresAt)

		if err == sql.ErrNoRows {
			sendTokenError(w, "invalid_grant", "Invalid device code")
			return
		}
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		// Check if expired
		if time.Now().After(expiresAt) {
			sendTokenError(w, "expired_token", "Device code has expired")
			return
		}

		// Check status
		switch status {
		case "pending":
			sendTokenError(w, "authorization_pending", "User has not yet authorized")
			return
		case "denied":
			sendTokenError(w, "access_denied", "User denied authorization")
			return
		case "authorized":
			// Return the tokens
			if !osmAccessToken.Valid {
				http.Error(w, "Token not available", http.StatusInternalServerError)
				return
			}

			expiresIn := 0
			if osmTokenExpiry.Valid {
				expiresIn = int(time.Until(osmTokenExpiry.Time).Seconds())
			}

			response := DeviceTokenResponse{
				AccessToken:  osmAccessToken.String,
				TokenType:    "Bearer",
				ExpiresIn:    expiresIn,
				RefreshToken: osmRefreshToken.String,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		default:
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
