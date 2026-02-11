package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Configuration defaults
const (
	defaultPort              = "8082"
	defaultRateLimit         = 100
	defaultRateLimitWindow   = 3600
	defaultTokenExpiry       = 3600
	defaultClientID          = "mock-client-id"
	defaultClientSecret      = "mock-client-secret"
	authCodeTTL              = 60 * time.Second
	mockUserID               = 12345
	mockUserName             = "Mock User"
	mockUserEmail            = "mock@example.com"
)

// Config holds server configuration from environment variables.
type Config struct {
	Port             string
	RateLimit        int
	RateLimitWindow  int
	ServiceBlocked   bool
	TokenExpiry      int
	ClientID         string
	ClientSecret     string
	AutoApprove      bool
}

func loadConfig() Config {
	cfg := Config{
		Port:            envOrDefault("PORT", defaultPort),
		RateLimit:       envIntOrDefault("MOCK_RATE_LIMIT", defaultRateLimit),
		RateLimitWindow: envIntOrDefault("MOCK_RATE_LIMIT_WINDOW", defaultRateLimitWindow),
		ServiceBlocked:  envBoolOrDefault("MOCK_SERVICE_BLOCKED", false),
		TokenExpiry:     envIntOrDefault("MOCK_TOKEN_EXPIRY", defaultTokenExpiry),
		ClientID:        envOrDefault("MOCK_CLIENT_ID", defaultClientID),
		ClientSecret:    envOrDefault("MOCK_CLIENT_SECRET", defaultClientSecret),
		AutoApprove:     envBoolOrDefault("MOCK_AUTO_APPROVE", false),
	}
	return cfg
}

// AuthCode represents a pending authorization code.
type AuthCode struct {
	Code        string
	RedirectURI string
	Scope       string
	CreatedAt   time.Time
	Used        bool
}

// Token represents an issued access/refresh token pair.
type Token struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Revoked      bool
}

// RateLimitEntry tracks per-user rate limiting.
type RateLimitEntry struct {
	Requests  int
	WindowEnd time.Time
}

// PatrolData matches the OSM API response format in internal/osm/patrol_scores.go.
// Points is a string (not int), matching the real OSM API.
type PatrolData struct {
	PatrolID string        `json:"patrolid"`
	Name     string        `json:"name"`
	Points   string        `json:"points"`
	Members  []interface{} `json:"members"`
}

// SectionData holds mock data for a section.
type SectionData struct {
	SectionID   int
	SectionName string
	GroupName   string
	GroupID     int
	SectionType string
	TermID      int
	TermName    string
	TermStart   string
	TermEnd     string
	Patrols     map[string]PatrolData
}

// State holds all in-memory server state.
type State struct {
	mu         sync.RWMutex
	authCodes  map[string]*AuthCode
	tokens     map[string]*Token // keyed by access token
	refreshMap map[string]*Token // keyed by refresh token
	rateLimits map[string]*RateLimitEntry
	sections   []SectionData
}

var (
	cfg   Config
	state *State
)

func init() {
	cfg = loadConfig()
	state = &State{
		authCodes:  make(map[string]*AuthCode),
		tokens:     make(map[string]*Token),
		refreshMap: make(map[string]*Token),
		rateLimits: make(map[string]*RateLimitEntry),
		sections:   buildMockSections(),
	}
}

func buildMockSections() []SectionData {
	now := time.Now()
	termStart := now.AddDate(0, -3, 0).Format("2006-01-02")
	termEnd := now.AddDate(0, 3, 0).Format("2006-01-02")

	return []SectionData{
		{
			SectionID:   1001,
			SectionName: "1st Anytown Scouts",
			GroupName:   "1st Anytown Group",
			GroupID:     100,
			SectionType: "scouts",
			TermID:      5001,
			TermName:    "Spring 2026",
			TermStart:   termStart,
			TermEnd:     termEnd,
			Patrols: map[string]PatrolData{
				"101": {PatrolID: "101", Name: "Eagles", Points: "42", Members: []interface{}{"member1", "member2", "member3"}},
				"102": {PatrolID: "102", Name: "Hawks", Points: "38", Members: []interface{}{"member4", "member5"}},
				"103": {PatrolID: "103", Name: "Owls", Points: "45", Members: []interface{}{"member6", "member7", "member8"}},
				// Edge cases for adapter filtering logic
				"-1":          {PatrolID: "-1", Name: "Leaders", Points: "0", Members: []interface{}{"leader1"}},
				"-2":          {PatrolID: "-2", Name: "Young Leaders", Points: "0", Members: []interface{}{"yl1"}},
				"unallocated": {PatrolID: "0", Name: "Unallocated", Points: "0", Members: []interface{}{}},
				"199":         {PatrolID: "199", Name: "Empty Patrol", Points: "0", Members: []interface{}{}},
			},
		},
		{
			SectionID:   1002,
			SectionName: "2nd Anytown Scouts",
			GroupName:   "2nd Anytown Group",
			GroupID:     200,
			SectionType: "scouts",
			TermID:      5002,
			TermName:    "Spring 2026",
			TermStart:   termStart,
			TermEnd:     termEnd,
			Patrols: map[string]PatrolData{
				"201": {PatrolID: "201", Name: "Panthers", Points: "51", Members: []interface{}{"member9", "member10"}},
				"202": {PatrolID: "202", Name: "Tigers", Points: "47", Members: []interface{}{"member11", "member12", "member13"}},
				"203": {PatrolID: "203", Name: "Wolves", Points: "49", Members: []interface{}{"member14", "member15"}},
				// Edge cases
				"-3":          {PatrolID: "-3", Name: "Leaders", Points: "0", Members: []interface{}{"leader2"}},
				"unallocated": {PatrolID: "0", Name: "Unallocated", Points: "0", Members: []interface{}{}},
			},
		},
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	// CORS middleware for local development
	cors := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	// OAuth endpoints
	mux.HandleFunc("/oauth/authorize", cors(handleAuthorize))
	mux.HandleFunc("/oauth/token", cors(handleToken))
	mux.HandleFunc("/oauth/resource", cors(rateLimitMiddleware(handleResource)))

	// API endpoints
	mux.HandleFunc("/ext/members/patrols/", cors(rateLimitMiddleware(handlePatrols)))

	// Health check
	mux.HandleFunc("/health", cors(handleHealth))

	addr := ":" + cfg.Port

	slog.Info("mock_osm.starting",
		"component", "mock_osm",
		"event", "startup",
		"port", cfg.Port,
		"client_id", cfg.ClientID,
		"auto_approve", cfg.AutoApprove,
		"rate_limit", cfg.RateLimit,
		"rate_limit_window", cfg.RateLimitWindow,
		"service_blocked", cfg.ServiceBlocked,
		"token_expiry", cfg.TokenExpiry,
	)

	fmt.Printf("\n  Mock OSM Server running on http://localhost:%s\n", cfg.Port)
	fmt.Printf("  Set OSM_DOMAIN=http://localhost:%s when running the adapter\n\n", cfg.Port)
	fmt.Printf("  Endpoints:\n")
	fmt.Printf("    GET/POST /oauth/authorize  - Authorization page\n")
	fmt.Printf("    POST     /oauth/token      - Token exchange & refresh\n")
	fmt.Printf("    GET      /oauth/resource   - User profile (Bearer auth)\n")
	fmt.Printf("    GET      /ext/members/patrols/?action=getPatrolsWithPeople  - Patrol scores\n")
	fmt.Printf("    POST     /ext/members/patrols/?action=updatePatrolPoints    - Update score\n")
	fmt.Printf("    GET      /health           - Health check\n\n")
	fmt.Printf("  Config:\n")
	fmt.Printf("    Client ID:       %s\n", cfg.ClientID)
	fmt.Printf("    Client Secret:   %s\n", cfg.ClientSecret)
	fmt.Printf("    Rate Limit:      %d requests per %d seconds\n", cfg.RateLimit, cfg.RateLimitWindow)
	fmt.Printf("    Token Expiry:    %d seconds\n", cfg.TokenExpiry)
	fmt.Printf("    Auto-Approve:    %v\n", cfg.AutoApprove)
	fmt.Printf("    Service Blocked: %v\n\n", cfg.ServiceBlocked)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// handleAuthorize handles GET (show form) and POST (grant/deny) for OAuth authorization.
func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	responseType := r.URL.Query().Get("response_type")
	stateParam := r.URL.Query().Get("state")
	scope := r.URL.Query().Get("scope")

	if r.Method == http.MethodPost {
		// POST from the authorization form
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		clientID = r.FormValue("client_id")
		redirectURI = r.FormValue("redirect_uri")
		stateParam = r.FormValue("state")
		scope = r.FormValue("scope")
		action := r.FormValue("action")

		if action == "deny" {
			// User denied - redirect with error
			redirectWithError(w, r, redirectURI, stateParam, "access_denied", "The user denied the request")
			return
		}

		// User approved - generate auth code and redirect
		code := generateAuthCode()
		state.mu.Lock()
		state.authCodes[code] = &AuthCode{
			Code:        code,
			RedirectURI: redirectURI,
			Scope:       scope,
			CreatedAt:   time.Now(),
		}
		state.mu.Unlock()

		slog.Info("mock_osm.authorize.code_issued",
			"component", "mock_osm",
			"event", "authorize.code_issued",
			"code_prefix", code[:16],
			"redirect_uri", redirectURI,
			"scope", scope,
		)

		redirectURL := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(stateParam))
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// GET - validate parameters
	if clientID == "" || redirectURI == "" || responseType == "" {
		http.Error(w, "Missing required parameters: client_id, redirect_uri, response_type", http.StatusBadRequest)
		return
	}

	if clientID != cfg.ClientID {
		http.Error(w, "Invalid client_id", http.StatusBadRequest)
		return
	}

	if responseType != "code" {
		http.Error(w, "Unsupported response_type: must be 'code'", http.StatusBadRequest)
		return
	}

	slog.Info("mock_osm.authorize.request",
		"component", "mock_osm",
		"event", "authorize.request",
		"client_id", clientID,
		"redirect_uri", redirectURI,
		"scope", scope,
		"auto_approve", cfg.AutoApprove,
	)

	// Auto-approve mode for automated testing
	if cfg.AutoApprove {
		code := generateAuthCode()
		state.mu.Lock()
		state.authCodes[code] = &AuthCode{
			Code:        code,
			RedirectURI: redirectURI,
			Scope:       scope,
			CreatedAt:   time.Now(),
		}
		state.mu.Unlock()

		slog.Info("mock_osm.authorize.auto_approved",
			"component", "mock_osm",
			"event", "authorize.auto_approved",
			"code_prefix", code[:16],
		)

		redirectURL := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(stateParam))
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	// Render authorization page
	renderAuthorizePage(w, clientID, redirectURI, stateParam, scope)
}

// handleToken handles token exchange (authorization_code) and refresh (refresh_token).
func handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "Invalid form data")
		return
	}

	grantType := r.FormValue("grant_type")

	switch grantType {
	case "authorization_code":
		handleTokenExchange(w, r)
	case "refresh_token":
		handleTokenRefresh(w, r)
	default:
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "Unsupported grant_type: "+grantType)
	}
}

func handleTokenExchange(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	// Validate client credentials
	if clientID != cfg.ClientID || clientSecret != cfg.ClientSecret {
		slog.Warn("mock_osm.token.invalid_client",
			"component", "mock_osm",
			"event", "token.invalid_client",
			"client_id", clientID,
		)
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "Invalid client credentials")
		return
	}

	// Look up and validate auth code
	state.mu.Lock()
	authCode, exists := state.authCodes[code]
	if !exists {
		state.mu.Unlock()
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Invalid authorization code")
		return
	}

	if authCode.Used {
		state.mu.Unlock()
		slog.Warn("mock_osm.token.code_reuse",
			"component", "mock_osm",
			"event", "token.code_reuse",
			"code_prefix", code[:min(16, len(code))],
		)
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Authorization code already used")
		return
	}

	if time.Since(authCode.CreatedAt) > authCodeTTL {
		state.mu.Unlock()
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Authorization code expired")
		return
	}

	if authCode.RedirectURI != redirectURI {
		state.mu.Unlock()
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Redirect URI mismatch")
		return
	}

	// Mark code as used
	authCode.Used = true

	// Generate tokens
	accessToken := generateAccessToken()
	refreshToken := generateRefreshToken()
	now := time.Now()

	token := &Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Scope:        authCode.Scope,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Duration(cfg.TokenExpiry) * time.Second),
	}

	state.tokens[accessToken] = token
	state.refreshMap[refreshToken] = token
	state.mu.Unlock()

	slog.Info("mock_osm.token.issued",
		"component", "mock_osm",
		"event", "token.issued",
		"grant_type", "authorization_code",
		"access_token_prefix", accessToken[:16],
		"scope", authCode.Scope,
		"expires_in", cfg.TokenExpiry,
	)

	writeJSON(w, map[string]interface{}{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    cfg.TokenExpiry,
		"refresh_token": refreshToken,
	})
}

func handleTokenRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	// Validate client credentials
	if clientID != cfg.ClientID || clientSecret != cfg.ClientSecret {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "Invalid client credentials")
		return
	}

	state.mu.Lock()
	oldToken, exists := state.refreshMap[refreshToken]
	if !exists {
		state.mu.Unlock()
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "Invalid refresh token")
		return
	}

	if oldToken.Revoked {
		state.mu.Unlock()
		slog.Warn("mock_osm.token.revoked_refresh",
			"component", "mock_osm",
			"event", "token.revoked_refresh",
		)
		// Return 401 to trigger ErrAccessRevoked in the adapter
		writeTokenError(w, http.StatusUnauthorized, "invalid_grant", "Token has been revoked")
		return
	}

	// Invalidate old tokens
	delete(state.tokens, oldToken.AccessToken)
	delete(state.refreshMap, refreshToken)

	// Generate new tokens
	newAccessToken := generateAccessToken()
	newRefreshToken := generateRefreshToken()
	now := time.Now()

	newToken := &Token{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		Scope:        oldToken.Scope,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Duration(cfg.TokenExpiry) * time.Second),
	}

	state.tokens[newAccessToken] = newToken
	state.refreshMap[newRefreshToken] = newToken
	state.mu.Unlock()

	slog.Info("mock_osm.token.refreshed",
		"component", "mock_osm",
		"event", "token.refreshed",
		"new_access_token_prefix", newAccessToken[:16],
		"expires_in", cfg.TokenExpiry,
	)

	writeJSON(w, map[string]interface{}{
		"access_token":  newAccessToken,
		"token_type":    "Bearer",
		"expires_in":    cfg.TokenExpiry,
		"refresh_token": newRefreshToken,
	})
}

// handleResource returns the user profile with sections and terms.
func handleResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractBearerToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	state.mu.RLock()
	tok, valid := state.tokens[token]
	state.mu.RUnlock()

	if !valid || tok.Revoked || time.Now().After(tok.ExpiresAt) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("mock_osm.resource.fetched",
		"component", "mock_osm",
		"event", "resource.fetched",
		"user_id", mockUserID,
	)

	// Build sections with terms
	state.mu.RLock()
	sections := make([]map[string]interface{}, len(state.sections))
	for i, sec := range state.sections {
		sections[i] = map[string]interface{}{
			"section_name": sec.SectionName,
			"group_name":   sec.GroupName,
			"section_id":   sec.SectionID,
			"group_id":     sec.GroupID,
			"section_type": sec.SectionType,
			"terms": []map[string]interface{}{
				{
					"name":      sec.TermName,
					"startdate": sec.TermStart,
					"enddate":   sec.TermEnd,
					"term_id":   sec.TermID,
				},
			},
		}
	}
	state.mu.RUnlock()

	writeJSON(w, map[string]interface{}{
		"status": true,
		"error":  nil,
		"data": map[string]interface{}{
			"user_id":            mockUserID,
			"full_name":          mockUserName,
			"email":              mockUserEmail,
			"sections":           sections,
			"has_parent_access":  false,
			"has_section_access": true,
		},
	})
}

// handlePatrols routes GET (getPatrolsWithPeople) and POST (updatePatrolPoints).
func handlePatrols(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	state.mu.RLock()
	tok, valid := state.tokens[token]
	state.mu.RUnlock()

	if !valid || tok.Revoked || time.Now().After(tok.ExpiresAt) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	action := r.URL.Query().Get("action")
	sectionIDStr := r.URL.Query().Get("sectionid")

	sectionID, err := strconv.Atoi(sectionIDStr)
	if err != nil {
		http.Error(w, "Invalid sectionid", http.StatusBadRequest)
		return
	}

	switch action {
	case "getPatrolsWithPeople":
		handleGetPatrols(w, r, sectionID)
	case "updatePatrolPoints":
		handleUpdatePatrolPoints(w, r, sectionID)
	default:
		http.Error(w, "Unknown action: "+action, http.StatusBadRequest)
	}
}

func handleGetPatrols(w http.ResponseWriter, _ *http.Request, sectionID int) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	section := findSection(sectionID)
	if section == nil {
		http.Error(w, "Section not found", http.StatusNotFound)
		return
	}

	slog.Info("mock_osm.patrols.fetched",
		"component", "mock_osm",
		"event", "patrols.fetched",
		"section_id", sectionID,
		"patrol_count", len(section.Patrols),
	)

	// Return as map[string]PatrolData matching real OSM API format
	writeJSON(w, section.Patrols)
}

func handleUpdatePatrolPoints(w http.ResponseWriter, r *http.Request, sectionID int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	patrolID := r.FormValue("patrolid")
	pointsStr := r.FormValue("points")

	if patrolID == "" || pointsStr == "" {
		http.Error(w, "Missing patrolid or points", http.StatusBadRequest)
		return
	}

	points, err := strconv.Atoi(pointsStr)
	if err != nil {
		http.Error(w, "Invalid points value", http.StatusBadRequest)
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	section := findSectionUnsafe(sectionID)
	if section == nil {
		http.Error(w, "Section not found", http.StatusNotFound)
		return
	}

	patrol, exists := section.Patrols[patrolID]
	if !exists {
		http.Error(w, "Patrol not found", http.StatusNotFound)
		return
	}

	// Update points (absolute value, matching real OSM behavior)
	patrol.Points = strconv.Itoa(points)
	section.Patrols[patrolID] = patrol

	slog.Info("mock_osm.patrols.updated",
		"component", "mock_osm",
		"event", "patrols.updated",
		"section_id", sectionID,
		"patrol_id", patrolID,
		"patrol_name", patrol.Name,
		"new_score", points,
	)

	// OSM returns an empty array on success
	writeJSON(w, []interface{}{})
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

// rateLimitMiddleware wraps handlers to add OSM-style rate limiting headers.
func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Add service blocked header if configured
		if cfg.ServiceBlocked {
			w.Header().Set("X-Blocked", "Service temporarily blocked")
			slog.Warn("mock_osm.rate_limit.service_blocked",
				"component", "mock_osm",
				"event", "rate_limit.service_blocked",
			)
			http.Error(w, "Service blocked", http.StatusServiceUnavailable)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			// No token - let the handler deal with auth
			next(w, r)
			return
		}

		state.mu.Lock()
		entry, exists := state.rateLimits[token]
		now := time.Now()

		if !exists || now.After(entry.WindowEnd) {
			// New window
			entry = &RateLimitEntry{
				Requests:  0,
				WindowEnd: now.Add(time.Duration(cfg.RateLimitWindow) * time.Second),
			}
			state.rateLimits[token] = entry
		}

		entry.Requests++
		remaining := cfg.RateLimit - entry.Requests
		if remaining < 0 {
			remaining = 0
		}
		resetSeconds := int(time.Until(entry.WindowEnd).Seconds())
		if resetSeconds < 0 {
			resetSeconds = 0
		}
		exceeded := entry.Requests > cfg.RateLimit
		state.mu.Unlock()

		// Always set rate limit headers (matching real OSM behavior)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.RateLimit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.Itoa(resetSeconds))

		if exceeded {
			retryAfter := resetSeconds
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))

			slog.Warn("mock_osm.rate_limit.exceeded",
				"component", "mock_osm",
				"event", "rate_limit.exceeded",
				"token_prefix", token[:min(16, len(token))],
				"requests", entry.Requests,
				"limit", cfg.RateLimit,
				"retry_after", retryAfter,
			)

			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

// renderAuthorizePage renders the HTML authorization form.
func renderAuthorizePage(w http.ResponseWriter, clientID, redirectURI, stateParam, scope string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Mock OSM - Authorize Application</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; max-width: 500px; margin: 50px auto; padding: 20px; background: #f5f5f5; }
        .card { background: white; border-radius: 8px; padding: 30px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; font-size: 1.5em; margin-top: 0; }
        .scope { background: #e8f4fd; border: 1px solid #b3d9f2; border-radius: 4px; padding: 8px 12px; margin: 15px 0; font-family: monospace; }
        .client-info { color: #666; margin: 10px 0; }
        .buttons { display: flex; gap: 10px; margin-top: 25px; }
        button { flex: 1; padding: 12px; border: none; border-radius: 6px; font-size: 1em; cursor: pointer; font-weight: 600; }
        .approve { background: #4CAF50; color: white; }
        .approve:hover { background: #45a049; }
        .deny { background: #f44336; color: white; }
        .deny:hover { background: #da190b; }
        .mock-badge { background: #ff9800; color: white; padding: 4px 8px; border-radius: 4px; font-size: 0.75em; font-weight: bold; display: inline-block; margin-bottom: 15px; }
    </style>
</head>
<body>
    <div class="card">
        <span class="mock-badge">MOCK OSM SERVER</span>
        <h1>Authorize Application</h1>
        <p class="client-info">Application <strong>%s</strong> is requesting access to your account.</p>
        <div class="scope">Scope: %s</div>
        <p>This will allow the application to access your Online Scout Manager data.</p>
        <form method="POST" action="/oauth/authorize">
            <input type="hidden" name="client_id" value="%s">
            <input type="hidden" name="redirect_uri" value="%s">
            <input type="hidden" name="state" value="%s">
            <input type="hidden" name="scope" value="%s">
            <div class="buttons">
                <button type="submit" name="action" value="approve" class="approve">Approve</button>
                <button type="submit" name="action" value="deny" class="deny">Deny</button>
            </div>
        </form>
    </div>
</body>
</html>`,
		escapeHTML(clientID),
		escapeHTML(scope),
		escapeHTML(clientID),
		escapeHTML(redirectURI),
		escapeHTML(stateParam),
		escapeHTML(scope),
	)
}

// Helper functions

func findSection(sectionID int) *SectionData {
	for i := range state.sections {
		if state.sections[i].SectionID == sectionID {
			return &state.sections[i]
		}
	}
	return nil
}

// findSectionUnsafe finds a section without taking a lock (caller must hold lock).
func findSectionUnsafe(sectionID int) *SectionData {
	return findSection(sectionID)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func generateAuthCode() string {
	return "mock_code_" + randomHex(24)
}

func generateAccessToken() string {
	return "mock_at_" + randomHex(32)
}

func generateRefreshToken() string {
	return "mock_rt_" + randomHex(32)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, stateParam, errorCode, errorDesc string) {
	redirectURL := fmt.Sprintf("%s?error=%s&error_description=%s&state=%s",
		redirectURI,
		url.QueryEscape(errorCode),
		url.QueryEscape(errorDesc),
		url.QueryEscape(stateParam),
	)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeTokenError(w http.ResponseWriter, statusCode int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             errorCode,
		"error_description": description,
	})
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

func envBoolOrDefault(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		return strings.EqualFold(v, "true") || v == "1"
	}
	return defaultVal
}
