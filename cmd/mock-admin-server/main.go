package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/handlers"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

const (
	mockUserID               = 12345
	mockUserName             = "Test User"
	mockCSRFToken            = "mock-csrf-token"
	defaultPort              = "8081"
	selectedSection          = 1001 // Default selected section
	defaultRateLimitInterval = 60   // Default: 1 update per 60 seconds
)

// MockAdhocPatrol represents an ad-hoc patrol in mock state
type MockAdhocPatrol struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Score    int    `json:"score"`
	Position int    `json:"position"`
}

// MockScoreboard represents a device scoreboard in mock state
type MockScoreboard struct {
	DeviceCodePrefix string `json:"deviceCodePrefix"`
	SectionID        *int   `json:"sectionId"`
	SectionName      string `json:"sectionName"`
	ClientID         string `json:"clientId"`
	LastUsedAt       string `json:"lastUsedAt,omitempty"`
}

// MockState holds the in-memory state for the mock server
type MockState struct {
	mu              sync.RWMutex
	sections        []AdminSection
	scores          map[int][]types.PatrolScore   // section ID -> patrol scores
	settings        map[int]map[string]string     // section ID -> patrol ID -> color
	lastUpdateTimes map[string]time.Time          // patrol ID -> last successful update time
	rateLimitSec    int                           // Rate limit interval in seconds
	adhocPatrols    []MockAdhocPatrol             // Ad-hoc patrols for the mock user
	adhocNextID     int64                         // Next ID for ad-hoc patrols
	scoreboards     []MockScoreboard              // Mock scoreboards
}

// AdminSection represents a section (copied from handlers package to avoid import cycles)
type AdminSection struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"groupName"`
}

var state *MockState

func init() {
	// Read rate limit from environment (default: 60 seconds = 1 update per minute)
	rateLimitSec := defaultRateLimitInterval
	if rateLimitEnv := os.Getenv("MOCK_RATE_LIMIT_SECONDS"); rateLimitEnv != "" {
		if parsed, err := strconv.Atoi(rateLimitEnv); err == nil && parsed >= 0 {
			rateLimitSec = parsed
		}
	}

	// Initialize mock data
	state = &MockState{
		sections: []AdminSection{
			{ID: 0, Name: "Ad-hoc Teams", GroupName: "Local"},
			{ID: 1001, Name: "1st Anytown Scouts", GroupName: "1st Anytown Group"},
			{ID: 1002, Name: "2nd Anytown Scouts", GroupName: "2nd Anytown Group"},
		},
		scores: map[int][]types.PatrolScore{
			1001: {
				{ID: "patrol_1", Name: "Eagles", Score: 42},
				{ID: "patrol_2", Name: "Hawks", Score: 38},
				{ID: "patrol_3", Name: "Owls", Score: 45},
			},
			1002: {
				{ID: "patrol_4", Name: "Panthers", Score: 51},
				{ID: "patrol_5", Name: "Tigers", Score: 47},
				{ID: "patrol_6", Name: "Wolves", Score: 49},
			},
		},
		settings: map[int]map[string]string{
			1001: {
				"patrol_1": "red",   // Eagles
				"patrol_2": "green", // Hawks
				"patrol_3": "blue",  // Owls
			},
			1002: {}, // No colors set for section 1002
		},
		lastUpdateTimes: make(map[string]time.Time),
		rateLimitSec:    rateLimitSec,
		adhocPatrols: []MockAdhocPatrol{
			{ID: 1, Name: "Red Team", Color: "red", Score: 15, Position: 0},
			{ID: 2, Name: "Blue Team", Color: "blue", Score: 22, Position: 1},
			{ID: 3, Name: "Green Team", Color: "green", Score: 18, Position: 2},
		},
		adhocNextID: 4,
		scoreboards: []MockScoreboard{
			{DeviceCodePrefix: "abc12345", SectionID: intPtr(1001), SectionName: "1st Anytown Scouts", ClientID: "mock-client", LastUsedAt: time.Now().Add(-1 * time.Hour).Format(time.RFC3339)},
			{DeviceCodePrefix: "def67890", SectionID: intPtr(0), SectionName: "Ad-hoc Teams", ClientID: "mock-client", LastUsedAt: time.Now().Add(-24 * time.Hour).Format(time.RFC3339)},
		},
	}
}

func main() {
	// Setup JSON logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()

	// CORS middleware for local development
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// Register endpoints
	mux.HandleFunc("/api/admin/session", corsMiddleware(handleSession))
	mux.HandleFunc("/api/admin/sections", corsMiddleware(handleSections))
	mux.HandleFunc("/api/admin/sections/", corsMiddleware(handleSectionRoutes)) // Trailing slash for path matching
	mux.HandleFunc("/api/admin/adhoc/patrols", corsMiddleware(handleAdhocPatrols))
	mux.HandleFunc("/api/admin/adhoc/patrols/", corsMiddleware(handleAdhocPatrolByID))
	mux.HandleFunc("/api/admin/scoreboards", corsMiddleware(handleScoreboards))
	mux.HandleFunc("/api/admin/scoreboards/", corsMiddleware(handleScoreboardSection))
	mux.HandleFunc("/health", corsMiddleware(handleHealth))

	addr := ":" + port
	slog.Info("mock_server.starting",
		"component", "mock_server",
		"event", "startup",
		"port", port,
		"sections", len(state.sections),
		"rate_limit_seconds", state.rateLimitSec,
	)

	fmt.Printf("\n🚀 Mock Admin Server running on http://localhost:%s\n", port)
	fmt.Printf("   Session:     GET  http://localhost:%s/api/admin/session\n", port)
	fmt.Printf("   Sections:    GET  http://localhost:%s/api/admin/sections\n", port)
	fmt.Printf("   Scores:      GET  http://localhost:%s/api/admin/sections/{id}/scores\n", port)
	fmt.Printf("   Update:      POST http://localhost:%s/api/admin/sections/{id}/scores\n", port)
	fmt.Printf("   Ad-hoc:      CRUD http://localhost:%s/api/admin/adhoc/patrols\n", port)
	fmt.Printf("   Scoreboards: GET  http://localhost:%s/api/admin/scoreboards\n", port)
	if state.rateLimitSec > 0 {
		fmt.Printf("\n⏱️  Rate Limit: 1 update per %d seconds per patrol\n", state.rateLimitSec)
		fmt.Printf("   Set MOCK_RATE_LIMIT_SECONDS=0 to disable\n")
	} else {
		fmt.Printf("\n⚡ Rate Limit: Disabled\n")
	}
	fmt.Println()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// handleSession always returns an authenticated session
func handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	slog.Info("mock_server.session",
		"component", "mock_server",
		"event", "session.fetched",
	)

	selectedSectionID := selectedSection
	response := handlers.AdminSessionResponse{
		Authenticated: true,
		User: &handlers.AdminUserInfo{
			OSMUserID: mockUserID,
			Name:      mockUserName,
		},
		SelectedSectionID: &selectedSectionID,
		CSRFToken:         mockCSRFToken,
	}

	writeJSON(w, response)
}

// handleSections returns the mock sections
func handleSections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	slog.Info("mock_server.sections",
		"component", "mock_server",
		"event", "sections.fetched",
		"section_count", len(state.sections),
	)

	// Convert to handlers.AdminSection type
	sections := make([]handlers.AdminSection, len(state.sections))
	for i, s := range state.sections {
		sections[i] = handlers.AdminSection{
			ID:        s.ID,
			Name:      s.Name,
			GroupName: s.GroupName,
		}
	}

	writeJSON(w, handlers.AdminSectionsResponse{Sections: sections})
}

// handleSectionRoutes routes to scores or settings based on URL suffix
func handleSectionRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/settings") {
		handleSettings(w, r)
	} else {
		handleScores(w, r)
	}
}

// handleScores handles both GET and POST for /api/admin/sections/{id}/scores
func handleScores(w http.ResponseWriter, r *http.Request) {
	// Parse section ID from URL
	path := r.URL.Path
	prefix := "/api/admin/sections/"
	suffix := "/scores"

	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
		return
	}

	sectionStr := path[len(prefix) : len(path)-len(suffix)]
	sectionID, err := strconv.Atoi(sectionStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
		return
	}

	// Handle ad-hoc section (section 0)
	if sectionID == 0 {
		switch r.Method {
		case http.MethodGet:
			handleGetAdhocScores(w, r)
		case http.MethodPost:
			handleUpdateAdhocScores(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Validate section exists
	state.mu.RLock()
	scores, exists := state.scores[sectionID]
	if !exists {
		state.mu.RUnlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "Section not found")
		return
	}
	state.mu.RUnlock()

	switch r.Method {
	case http.MethodGet:
		handleGetScores(w, r, sectionID, scores)
	case http.MethodPost:
		handleUpdateScores(w, r, sectionID)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleGetScores returns patrol scores for a section
func handleGetScores(w http.ResponseWriter, _ *http.Request, sectionID int, scores []types.PatrolScore) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Find section name
	var sectionName string
	for _, s := range state.sections {
		if s.ID == sectionID {
			sectionName = s.Name
			break
		}
	}

	slog.Info("mock_server.scores.fetched",
		"component", "mock_server",
		"event", "scores.fetched",
		"section_id", sectionID,
		"patrol_count", len(scores),
	)

	response := handlers.AdminScoresResponse{
		Section: handlers.AdminSectionInfo{
			ID:   sectionID,
			Name: sectionName,
		},
		TermID:    1, // Mock term ID
		Patrols:   scores,
		FetchedAt: time.Now().UTC(),
	}

	writeJSON(w, response)
}

// handleUpdateScores handles score updates
func handleUpdateScores(w http.ResponseWriter, r *http.Request, sectionID int) {
	// Validate CSRF token (mock - accept our mock token)
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	// Parse request
	var req handlers.AdminUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	if len(req.Updates) == 0 {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "No updates provided")
		return
	}

	// Validate points range
	for _, update := range req.Updates {
		if update.Points < -1000 || update.Points > 1000 {
			writeJSONError(w, http.StatusBadRequest, "validation_error", "Points must be between -1000 and 1000")
			return
		}
	}

	// Apply updates
	state.mu.Lock()
	defer state.mu.Unlock()

	scores := state.scores[sectionID]
	results := make([]handlers.AdminPatrolResult, 0, len(req.Updates))
	now := time.Now()

	for _, update := range req.Updates {
		// Find patrol
		var patrol *types.PatrolScore
		for i := range scores {
			if scores[i].ID == update.PatrolID {
				patrol = &scores[i]
				break
			}
		}

		if patrol == nil {
			// Patrol not found - return permanent error result
			errMsg := fmt.Sprintf("Patrol %s not found", update.PatrolID)
			results = append(results, handlers.AdminPatrolResult{
				ID:           update.PatrolID,
				Success:      false,
				ErrorMessage: &errMsg,
			})
			continue
		}

		// Check rate limit (if enabled)
		if state.rateLimitSec > 0 {
			if lastUpdate, exists := state.lastUpdateTimes[patrol.ID]; exists {
				timeSinceLastUpdate := now.Sub(lastUpdate)
				rateLimitDuration := time.Duration(state.rateLimitSec) * time.Second

				if timeSinceLastUpdate < rateLimitDuration {
					// Rate limited - return temporary error
					retryAfter := lastUpdate.Add(rateLimitDuration)
					errMsg := fmt.Sprintf("Rate limit exceeded. Please wait %d seconds between updates for this patrol.",
						state.rateLimitSec)
					isTemporary := true

					slog.Info("mock_server.scores.rate_limited",
						"component", "mock_server",
						"event", "scores.rate_limited",
						"section_id", sectionID,
						"patrol_id", patrol.ID,
						"patrol_name", patrol.Name,
						"retry_after", retryAfter,
					)

					results = append(results, handlers.AdminPatrolResult{
						ID:               patrol.ID,
						Name:             patrol.Name,
						Success:          false,
						IsTemporaryError: &isTemporary,
						RetryAfter:       &retryAfter,
						ErrorMessage:     &errMsg,
					})
					continue
				}
			}
		}

		// Update score
		previousScore := patrol.Score
		patrol.Score += update.Points

		// Track update time for rate limiting
		state.lastUpdateTimes[patrol.ID] = now

		slog.Info("mock_server.scores.updated",
			"component", "mock_server",
			"event", "scores.updated",
			"section_id", sectionID,
			"patrol_id", patrol.ID,
			"patrol_name", patrol.Name,
			"previous_score", previousScore,
			"new_score", patrol.Score,
			"points_added", update.Points,
		)

		results = append(results, handlers.AdminPatrolResult{
			ID:            patrol.ID,
			Name:          patrol.Name,
			Success:       true,
			PreviousScore: previousScore,
			NewScore:      patrol.Score,
		})
	}

	writeJSON(w, handlers.AdminUpdateResponse{
		Success: true,
		Patrols: results,
	})
}

// intPtr returns a pointer to an int value
func intPtr(v int) *int {
	return &v
}

// handleHealth returns a simple health check
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, statusCode int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(handlers.AdminErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// handleSettings handles GET and PUT for /api/admin/sections/{id}/settings
func handleSettings(w http.ResponseWriter, r *http.Request) {
	// Parse section ID from URL
	path := r.URL.Path
	prefix := "/api/admin/sections/"
	suffix := "/settings"

	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
		return
	}

	sectionStr := path[len(prefix) : len(path)-len(suffix)]
	sectionID, err := strconv.Atoi(sectionStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
		return
	}

	// Handle ad-hoc section (section 0)
	if sectionID == 0 {
		switch r.Method {
		case http.MethodGet:
			handleGetAdhocSettings(w, r)
		case http.MethodPut:
			handleUpdateAdhocSettings(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}

	// Validate section exists
	state.mu.RLock()
	_, exists := state.scores[sectionID]
	if !exists {
		state.mu.RUnlock()
		writeJSONError(w, http.StatusNotFound, "not_found", "Section not found")
		return
	}
	state.mu.RUnlock()

	switch r.Method {
	case http.MethodGet:
		handleGetSettings(w, r, sectionID)
	case http.MethodPut:
		handleUpdateSettings(w, r, sectionID)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleGetSettings returns settings for a section
func handleGetSettings(w http.ResponseWriter, _ *http.Request, sectionID int) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Get patrol list for this section
	scores := state.scores[sectionID]
	patrols := make([]types.PatrolInfo, len(scores))
	for i, s := range scores {
		patrols[i] = types.PatrolInfo{
			ID:   s.ID,
			Name: s.Name,
		}
	}

	// Get colors
	colors := state.settings[sectionID]
	if colors == nil {
		colors = make(map[string]string)
	}

	slog.Info("mock_server.settings.fetched",
		"component", "mock_server",
		"event", "settings.fetched",
		"section_id", sectionID,
		"patrol_count", len(patrols),
		"color_count", len(colors),
	)

	writeJSON(w, handlers.AdminSettingsResponse{
		SectionID:    sectionID,
		PatrolColors: colors,
		Patrols:      patrols,
	})
}

// handleUpdateSettings updates settings for a section
func handleUpdateSettings(w http.ResponseWriter, r *http.Request, sectionID int) {
	// Validate CSRF token
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	// Parse request
	var req handlers.AdminSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	// Update settings
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.settings[sectionID] == nil {
		state.settings[sectionID] = make(map[string]string)
	}

	// Replace all colors
	state.settings[sectionID] = req.PatrolColors

	slog.Info("mock_server.settings.updated",
		"component", "mock_server",
		"event", "settings.updated",
		"section_id", sectionID,
		"color_count", len(req.PatrolColors),
	)

	writeJSON(w, handlers.AdminSettingsResponse{
		SectionID:    sectionID,
		PatrolColors: req.PatrolColors,
		Patrols:      nil, // Don't need to return patrols for PUT
	})
}

// --- Ad-hoc scores handlers ---

// handleGetAdhocScores returns ad-hoc patrol scores as section 0
func handleGetAdhocScores(w http.ResponseWriter, _ *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	patrols := make([]types.PatrolScore, len(state.adhocPatrols))
	for i, p := range state.adhocPatrols {
		patrols[i] = types.PatrolScore{
			ID:    strconv.FormatInt(p.ID, 10),
			Name:  p.Name,
			Score: p.Score,
		}
	}

	writeJSON(w, handlers.AdminScoresResponse{
		Section: handlers.AdminSectionInfo{
			ID:   0,
			Name: "Ad-hoc Teams",
		},
		TermID:    0,
		Patrols:   patrols,
		FetchedAt: time.Now().UTC(),
	})
}

// handleUpdateAdhocScores applies score deltas to ad-hoc patrols
func handleUpdateAdhocScores(w http.ResponseWriter, r *http.Request) {
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	var req handlers.AdminUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	results := make([]handlers.AdminPatrolResult, 0, len(req.Updates))
	for _, update := range req.Updates {
		var found *MockAdhocPatrol
		for i := range state.adhocPatrols {
			if strconv.FormatInt(state.adhocPatrols[i].ID, 10) == update.PatrolID {
				found = &state.adhocPatrols[i]
				break
			}
		}
		if found == nil {
			errMsg := fmt.Sprintf("Patrol %s not found", update.PatrolID)
			results = append(results, handlers.AdminPatrolResult{
				ID:           update.PatrolID,
				Success:      false,
				ErrorMessage: &errMsg,
			})
			continue
		}

		previousScore := found.Score
		found.Score += update.Points
		results = append(results, handlers.AdminPatrolResult{
			ID:            strconv.FormatInt(found.ID, 10),
			Name:          found.Name,
			Success:       true,
			PreviousScore: previousScore,
			NewScore:      found.Score,
		})
	}

	writeJSON(w, handlers.AdminUpdateResponse{
		Success: true,
		Patrols: results,
	})
}

// --- Ad-hoc settings handlers ---

// handleGetAdhocSettings returns patrol list and colors for ad-hoc section
func handleGetAdhocSettings(w http.ResponseWriter, _ *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	patrols := make([]types.PatrolInfo, len(state.adhocPatrols))
	colors := make(map[string]string)
	for i, p := range state.adhocPatrols {
		id := strconv.FormatInt(p.ID, 10)
		patrols[i] = types.PatrolInfo{
			ID:   id,
			Name: p.Name,
		}
		if p.Color != "" {
			colors[id] = p.Color
		}
	}

	writeJSON(w, handlers.AdminSettingsResponse{
		SectionID:    0,
		PatrolColors: colors,
		Patrols:      patrols,
	})
}

// handleUpdateAdhocSettings updates patrol colors for ad-hoc section
func handleUpdateAdhocSettings(w http.ResponseWriter, r *http.Request) {
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	var req handlers.AdminSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for i := range state.adhocPatrols {
		id := strconv.FormatInt(state.adhocPatrols[i].ID, 10)
		if color, ok := req.PatrolColors[id]; ok {
			state.adhocPatrols[i].Color = color
		}
	}

	writeJSON(w, handlers.AdminSettingsResponse{
		SectionID:    0,
		PatrolColors: req.PatrolColors,
	})
}

// --- Ad-hoc patrol CRUD handlers ---

// AdhocPatrolJSON is the mock API response format for ad-hoc patrols
type AdhocPatrolJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Score    int    `json:"score"`
	Position int    `json:"position"`
}

func toAdhocPatrolJSON(p MockAdhocPatrol) AdhocPatrolJSON {
	return AdhocPatrolJSON{
		ID:       strconv.FormatInt(p.ID, 10),
		Name:     p.Name,
		Color:    p.Color,
		Score:    p.Score,
		Position: p.Position,
	}
}

// handleAdhocPatrols handles GET /api/admin/adhoc/patrols and POST /api/admin/adhoc/patrols
func handleAdhocPatrols(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state.mu.RLock()
		defer state.mu.RUnlock()

		resp := make([]AdhocPatrolJSON, len(state.adhocPatrols))
		for i, p := range state.adhocPatrols {
			resp[i] = toAdhocPatrolJSON(p)
		}
		writeJSON(w, resp)

	case http.MethodPost:
		csrfToken := r.Header.Get("X-CSRF-Token")
		if csrfToken != mockCSRFToken {
			writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
			return
		}

		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > 50 {
			writeJSONError(w, http.StatusBadRequest, "validation_error", "Name must be 1-50 characters")
			return
		}

		state.mu.Lock()
		if len(state.adhocPatrols) >= 20 {
			state.mu.Unlock()
			writeJSONError(w, http.StatusConflict, "max_patrols_reached", "Maximum of 20 ad-hoc patrols reached")
			return
		}

		nextPosition := 0
		for _, p := range state.adhocPatrols {
			if p.Position >= nextPosition {
				nextPosition = p.Position + 1
			}
		}

		patrol := MockAdhocPatrol{
			ID:       state.adhocNextID,
			Name:     req.Name,
			Color:    req.Color,
			Score:    0,
			Position: nextPosition,
		}
		state.adhocNextID++
		state.adhocPatrols = append(state.adhocPatrols, patrol)
		state.mu.Unlock()

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, toAdhocPatrolJSON(patrol))

	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleAdhocPatrolByID handles PUT/DELETE /api/admin/adhoc/patrols/{id}
func handleAdhocPatrolByID(w http.ResponseWriter, r *http.Request) {
	// Parse patrol ID from path: /api/admin/adhoc/patrols/{id}
	path := r.URL.Path
	prefix := "/api/admin/adhoc/patrols/"

	// Check for reset endpoint
	if strings.HasSuffix(path, "/reset") {
		handleAdhocReset(w, r)
		return
	}

	idStr := path[len(prefix):]
	if idStr == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Missing patrol ID")
		return
	}

	patrolID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid patrol ID")
		return
	}

	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > 50 {
			writeJSONError(w, http.StatusBadRequest, "validation_error", "Name must be 1-50 characters")
			return
		}

		state.mu.Lock()
		defer state.mu.Unlock()

		for i := range state.adhocPatrols {
			if state.adhocPatrols[i].ID == patrolID {
				state.adhocPatrols[i].Name = req.Name
				state.adhocPatrols[i].Color = req.Color
				writeJSON(w, toAdhocPatrolJSON(state.adhocPatrols[i]))
				return
			}
		}
		writeJSONError(w, http.StatusNotFound, "not_found", "Patrol not found")

	case http.MethodDelete:
		state.mu.Lock()
		defer state.mu.Unlock()

		for i := range state.adhocPatrols {
			if state.adhocPatrols[i].ID == patrolID {
				state.adhocPatrols = append(state.adhocPatrols[:i], state.adhocPatrols[i+1:]...)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		writeJSONError(w, http.StatusNotFound, "not_found", "Patrol not found")

	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleAdhocReset resets all ad-hoc patrol scores to 0
func handleAdhocReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for i := range state.adhocPatrols {
		state.adhocPatrols[i].Score = 0
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Scoreboard handlers ---

// handleScoreboards handles GET /api/admin/scoreboards
func handleScoreboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	writeJSON(w, state.scoreboards)
}

// handleScoreboardSection handles PUT /api/admin/scoreboards/{deviceCode}/section
func handleScoreboardSection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken != mockCSRFToken {
		writeJSONError(w, http.StatusForbidden, "csrf_invalid", "Invalid CSRF token")
		return
	}

	// Parse device code from path: /api/admin/scoreboards/{deviceCode}/section
	path := r.URL.Path
	prefix := "/api/admin/scoreboards/"
	suffix := "/section"
	if !strings.HasSuffix(path, suffix) {
		writeJSONError(w, http.StatusNotFound, "not_found", "Invalid path")
		return
	}

	deviceCode := path[len(prefix) : len(path)-len(suffix)]
	if deviceCode == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Missing device code")
		return
	}

	var req struct {
		SectionID int `json:"sectionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid request body")
		return
	}

	// Validate section exists
	sectionFound := false
	var sectionName string
	state.mu.RLock()
	for _, s := range state.sections {
		if s.ID == req.SectionID {
			sectionFound = true
			sectionName = s.Name
			break
		}
	}
	state.mu.RUnlock()

	if !sectionFound {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "Invalid section ID")
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	for i := range state.scoreboards {
		if state.scoreboards[i].DeviceCodePrefix == deviceCode {
			state.scoreboards[i].SectionID = intPtr(req.SectionID)
			state.scoreboards[i].SectionName = sectionName
			writeJSON(w, state.scoreboards[i])
			return
		}
	}

	writeJSONError(w, http.StatusNotFound, "not_found", "Device not found")
}
