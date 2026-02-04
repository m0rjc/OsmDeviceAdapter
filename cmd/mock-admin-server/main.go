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

// MockState holds the in-memory state for the mock server
type MockState struct {
	mu              sync.RWMutex
	sections        []AdminSection
	scores          map[int][]types.PatrolScore // section ID -> patrol scores
	lastUpdateTimes map[string]time.Time        // patrol ID -> last successful update time
	rateLimitSec    int                         // Rate limit interval in seconds
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
		lastUpdateTimes: make(map[string]time.Time),
		rateLimitSec:    rateLimitSec,
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
	mux.HandleFunc("/api/admin/sections/", corsMiddleware(handleScores)) // Trailing slash for path matching
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
	fmt.Printf("   Session:  GET  http://localhost:%s/api/admin/session\n", port)
	fmt.Printf("   Sections: GET  http://localhost:%s/api/admin/sections\n", port)
	fmt.Printf("   Scores:   GET  http://localhost:%s/api/admin/sections/{id}/scores\n", port)
	fmt.Printf("   Update:   POST http://localhost:%s/api/admin/sections/{id}/scores\n", port)
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
