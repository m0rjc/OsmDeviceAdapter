package osm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

type Client struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

type Patrol struct {
	PatrolID   string `json:"patrol_id"`
	PatrolName string `json:"patrol_name"`
	Points     int    `json:"points"`
}

func NewClient(baseURL, accessToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) GetPatrolScores(ctx context.Context) ([]types.PatrolScore, error) {
	// Note: This is a placeholder implementation
	// You'll need to adjust this based on OSM's actual API endpoints
	// OSM API documentation: https://www.onlinescoutmanager.co.uk/api/

	endpoint := "/api.php?action=getPatrolScores"
	start := time.Now()

	slog.Info("osm.api.request",
		"component", "osm_api",
		"event", "api.request.start",
		"endpoint", endpoint,
		"method", "GET",
	)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/api.php", c.baseURL),
		nil,
	)
	if err != nil {
		slog.Error("osm.api.request_creation_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	// Add authorization header
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	// Add OSM-specific parameters
	q := req.URL.Query()
	q.Set("action", "getPatrolScores") // Adjust based on actual OSM API
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("osm.api.request_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		metrics.OSMAPILatency.WithLabelValues(endpoint, "error").Observe(time.Since(start).Seconds())
		return nil, err
	}
	defer resp.Body.Close()

	// Record latency
	duration := time.Since(start)
	metrics.OSMAPILatency.WithLabelValues(endpoint, strconv.Itoa(resp.StatusCode)).Observe(duration.Seconds())

	// Check for X-Blocked header (complete service block by OSM)
	if blockedHeader := resp.Header.Get("X-Blocked"); blockedHeader != "" {
		metrics.OSMServiceBlocked.Set(1)
		metrics.OSMBlockCount.Inc()
		slog.Error("osm.service.blocked",
			"component", "osm_api",
			"event", "blocked.detected",
			"blocked_header", blockedHeader,
			"severity", "CRITICAL",
			"action_required", "manual_investigation",
			"impact", "all_osm_api_calls_blocked",
			"endpoint", endpoint,
		)
		return nil, fmt.Errorf("OSM service blocked: %s", blockedHeader)
	} else {
		// Clear the blocked flag if we get a successful response
		metrics.OSMServiceBlocked.Set(0)
	}

	// Parse rate limit headers (per-user rate limits)
	c.parseRateLimitHeaders(resp.Header, endpoint)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("osm.api.error_response",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"status_code", resp.StatusCode,
			"response_body", string(body),
			"duration_ms", duration.Milliseconds(),
		)
		return nil, fmt.Errorf("OSM API error: %s - %s", resp.Status, string(body))
	}

	var patrols []Patrol
	if err := json.NewDecoder(resp.Body).Decode(&patrols); err != nil {
		slog.Error("osm.api.decode_error",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	// Convert to our response format
	result := make([]types.PatrolScore, len(patrols))
	for i, p := range patrols {
		result[i] = types.PatrolScore{
			ID:    p.PatrolID,
			Name:  p.PatrolName,
			Score: p.Points,
		}
	}

	slog.Info("osm.api.success",
		"component", "osm_api",
		"event", "api.request.success",
		"endpoint", endpoint,
		"status_code", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
		"patrol_count", len(result),
	)

	return result, nil
}

func (c *Client) RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*types.OSMTokenResponse, error) {
	endpoint := "/oauth/token"
	start := time.Now()

	slog.Info("osm.oauth.refresh",
		"component", "osm_api",
		"event", "token.refresh.start",
		"endpoint", endpoint,
	)

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/oauth/token", c.baseURL),
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		slog.Error("osm.oauth.request_creation_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("osm.oauth.request_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		metrics.OSMAPILatency.WithLabelValues(endpoint, "error").Observe(time.Since(start).Seconds())
		return nil, err
	}
	defer resp.Body.Close()

	// Record latency
	duration := time.Since(start)
	metrics.OSMAPILatency.WithLabelValues(endpoint, strconv.Itoa(resp.StatusCode)).Observe(duration.Seconds())

	// Check for X-Blocked header
	if blockedHeader := resp.Header.Get("X-Blocked"); blockedHeader != "" {
		metrics.OSMServiceBlocked.Set(1)
		metrics.OSMBlockCount.Inc()
		slog.Error("osm.service.blocked",
			"component", "osm_api",
			"event", "blocked.detected",
			"blocked_header", blockedHeader,
			"severity", "CRITICAL",
			"action_required", "manual_investigation",
			"impact", "all_osm_api_calls_blocked",
			"endpoint", endpoint,
		)
		return nil, fmt.Errorf("OSM service blocked: %s", blockedHeader)
	} else {
		metrics.OSMServiceBlocked.Set(0)
	}

	// Parse rate limit headers
	c.parseRateLimitHeaders(resp.Header, endpoint)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("osm.oauth.refresh_failed",
			"component", "osm_api",
			"event", "token.refresh.error",
			"endpoint", endpoint,
			"status_code", resp.StatusCode,
			"response_body", string(body),
			"duration_ms", duration.Milliseconds(),
		)
		return nil, fmt.Errorf("token refresh failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		slog.Error("osm.oauth.decode_error",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	slog.Info("osm.oauth.refresh_success",
		"component", "osm_api",
		"event", "token.refresh.success",
		"endpoint", endpoint,
		"status_code", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
	)

	return &tokenResp, nil
}

// parseRateLimitHeaders parses rate limit headers from OSM API response
// OSM uses standard rate limit headers as documented in docs/research/OSM-OAuth-Doc.md:
// - X-RateLimit-Limit: Maximum requests per hour (per authenticated user)
// - X-RateLimit-Remaining: Requests remaining before being blocked
// - X-RateLimit-Reset: Seconds until the rate limit resets
func (c *Client) parseRateLimitHeaders(headers http.Header, endpoint string) {
	// Parse OSM rate limit headers
	limitStr := headers.Get("X-RateLimit-Limit")
	remainingStr := headers.Get("X-RateLimit-Remaining")
	resetStr := headers.Get("X-RateLimit-Reset")

	if remainingStr == "" {
		// No rate limit headers present
		return
	}

	remaining, err := strconv.Atoi(remainingStr)
	if err != nil {
		slog.Warn("osm.api.invalid_rate_limit_header",
			"component", "osm_api",
			"header", "X-RateLimit-Remaining",
			"value", remainingStr,
			"error", err,
		)
		return
	}

	// Use a placeholder user_id since OSM rate limits are typically per user
	// In a real implementation, you'd extract the user_id from the access token or context
	userID := "current_user" // TODO: Extract from access token or context

	// Update metrics
	metrics.OSMRateLimitRemaining.WithLabelValues(userID).Set(float64(remaining))

	if limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			metrics.OSMRateLimitTotal.WithLabelValues(userID).Set(float64(limit))
		}
	}

	var resetSeconds int
	if resetStr != "" {
		if reset, err := strconv.Atoi(resetStr); err == nil {
			resetSeconds = reset
			metrics.OSMRateLimitResetSeconds.WithLabelValues(userID).Set(float64(reset))
		}
	}

	// Log rate limit status
	logLevel := slog.LevelInfo
	event := "rate_limit.info"
	severity := "INFO"

	if remaining < 20 {
		logLevel = slog.LevelError
		event = "rate_limit.critical"
		severity = "CRITICAL"
	} else if remaining < 100 {
		logLevel = slog.LevelWarn
		event = "rate_limit.warning"
		severity = "WARN"
	}

	logAttrs := []any{
		"component", "osm_api",
		"event", event,
		"user_id", userID,
		"endpoint", endpoint,
		"rate_limit_remaining", remaining,
		"rate_limit_limit", limitStr,
		"severity", severity,
	}

	if resetSeconds > 0 {
		logAttrs = append(logAttrs, "rate_limit_reset_seconds", resetSeconds)
	}

	slog.Log(context.Background(), logLevel, "osm.api.rate_limit", logAttrs...)
}
