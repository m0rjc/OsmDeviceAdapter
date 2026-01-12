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

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

var (
	ErrServiceBlocked = fmt.Errorf("OSM service blocked")
)

// fallbackUserBlockTime is the last resort block time to apply if we cannot find a block time from headers.
var fallbackUserBlockTime time.Duration = 10 * time.Minute

// ErrUserBlocked indicates that the user has been blocked until the given time
type ErrUserBlocked struct {
	BlockedUntil time.Time
}

func (e *ErrUserBlocked) Error() string {
	return fmt.Sprintf("OSM user blocked until %v", e.BlockedUntil)
}

// UserRateLimitInfo contains the current rate limit state for a user
type UserRateLimitInfo struct {
	Remaining int // Number of requests remaining in the current window
	Limit     int // Total number of requests allowed per window
	ResetsAt  time.Time
}

type RateLimitStore interface {
	// MarkOsmServiceBlocked marks the OSM service as blocked by the API.
	// This is a hard block requiring human intervention to fix.
	MarkOsmServiceBlocked(ctx context.Context)
	// IsOsmServiceBlocked returns true if the OSM service is blocked by the API.
	// The Request client must return the ErrServiceBlocked sentinel error without calling OSM if this is true.
	IsOsmServiceBlocked(ctx context.Context) bool

	// MarkUserTemporarilyBlocked marks the user as temporarily blocked by the API.
	// The API will have returned a retry duration. See OSM-OAuth-Doc.md for details.
	// User blocking is only relevant if a userID and access token are provided.
	// blockedUntil is the absolute time when the block expires.
	MarkUserTemporarilyBlocked(ctx context.Context, userId int, blockedUntil time.Time)

	// GetUserBlockEndTime retrieves the block end time for a user from Redis.
	// Returns zero time if the user is not blocked.
	GetUserBlockEndTime(ctx context.Context, userId int) time.Time
}

type LatencyRecorder interface {
	// RecordOsmLatency records the latency of an OSM API request.
	RecordOsmLatency(endpoint string, statusCode int, latency time.Duration)

	// RecordRateLimit records a rate limit result
	RecordRateLimit(userId *int, limitRemaining int, limitTotal int, limitResetSeconds int)
}

// requestConfig holds the configuration for a single OSM API request.
type requestConfig struct {
	path            string
	queryParameters map[string]string
	body            io.Reader
	contentType     string
	sensitive       bool
	userId          *int
	userToken       string
}

// RequestOption defines a functional option for configuring an OSM API Request.
type RequestOption func(*requestConfig)

// WithPath sets the URL path for the Request.
// If the path is "/oauth/token", the Request is automatically marked as sensitive.
func WithPath(path string) RequestOption {
	return func(c *requestConfig) {
		c.path = path
		if path == "/oauth/token" {
			c.sensitive = true
		}
	}
}

// WithUser sets the user ID and token for the Request.
// If userID is provided, the Request method will check for user-specific blocks via the MetricsStore.
// The userToken will be used in the Authorization header instead of the client's access token.
func WithUser(user types.User) RequestOption {
	return func(c *requestConfig) {
		c.userId = user.UserID()
		c.userToken = user.AccessToken()
	}
}

// WithSensitive marks the Request as containing sensitive data (like tokens or secrets),
// ensuring the response body is redacted in logs in case of an error.
func WithSensitive() RequestOption {
	return func(c *requestConfig) {
		c.sensitive = true
	}
}

// WithQueryParameters adds or updates query parameters for the Request.
func WithQueryParameters(params map[string]string) RequestOption {
	return func(c *requestConfig) {
		if c.queryParameters == nil {
			c.queryParameters = make(map[string]string)
		}
		for k, v := range params {
			c.queryParameters[k] = v
		}
	}
}

// WithApiAction sets the path to "/api.php" and adds the "action" query parameter.
// This is the standard way to call the OSM API for data requests.
func WithApiAction(action string) RequestOption {
	return func(c *requestConfig) {
		c.path = "/api.php"
		if c.queryParameters == nil {
			c.queryParameters = make(map[string]string)
		}
		c.queryParameters["action"] = action
	}
}

// WithPostBody sets the Request body for POST Requests.
func WithPostBody(body io.Reader) RequestOption {
	return func(c *requestConfig) {
		c.body = body
	}
}

func WithUrlEncodedBody(data *url.Values) RequestOption {
	return func(c *requestConfig) {
		c.contentType = "application/x-www-form-urlencoded"
		c.body = strings.NewReader(data.Encode())
	}
}

// WithContentType sets the Content-Type header for the Request.
func WithContentType(contentType string) RequestOption {
	return func(c *requestConfig) {
		c.contentType = contentType
	}
}

// Response represents a response from the OSM API.
type Response struct {
	httpResponse *http.Response
	StatusCode   int // HTTP status code of the response.
	Limits       UserRateLimitInfo
}

// Request performs an HTTP request to the OSM API.
// It returns a Response and an error if the request failed or the API returned a non-200 status code.
// If the service or user is blocked, it returns ErrServiceBlocked or ErrTemporaryBlocked.
// If the target is non-nil and the response status is 200 OK, the response body is decoded into target.
func (c *Client) Request(ctx context.Context, method string, target any, options ...RequestOption) (*Response, error) {
	config := &requestConfig{
		queryParameters: make(map[string]string),
	}
	for _, option := range options {
		option(config)
	}

	// Check for global service block
	if c.rlStore != nil && c.rlStore.IsOsmServiceBlocked(ctx) {
		slog.Error("osm.api.request_prevented_by_app_block",
			"component", "osm_api",
			"event", "api.request.start",
		)
		return nil, ErrServiceBlocked
	}

	// Check for user-specific block
	if config.userId != nil && c.rlStore != nil {
		blockedUntil := c.rlStore.GetUserBlockEndTime(ctx, *config.userId)
		if blockedUntil.After(time.Now()) {
			slog.Error("osm.api.request_prevented_by_user_block",
				"userId", config.userId,
				"component", "osm_api",
				"event", "api.request.start",
			)
			return nil, &ErrUserBlocked{blockedUntil}
		}
	}

	// endpoint is used for logging and metrics labels to provide more granular visibility.
	// For standard OSM API calls to api.php, we use the 'action' parameter as the endpoint name.
	endpoint := config.path
	if config.path == "/api.php" {
		if action, ok := config.queryParameters["action"]; ok {
			endpoint = action
		}
	}

	slog.Debug("osm.api.request",
		"component", "osm_api",
		"event", "api.request.start",
		"endpoint", endpoint,
		"method", method,
		"path", config.path,
	)

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = config.path

	if len(config.queryParameters) > 0 {
		q := u.Query()
		for k, v := range config.queryParameters {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), config.body)
	if err != nil {
		slog.Error("osm.api.request_creation_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
		)
		return nil, err
	}

	if config.body != nil {
		if v, ok := config.body.(interface{ Len() int }); ok {
			req.ContentLength = int64(v.Len())
		}
	}

	if config.contentType != "" {
		req.Header.Set("Content-Type", config.contentType)
	}

	// Add authorization header
	if config.userToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.userToken))
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		slog.Error("osm.api.request_failed",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		if c.recorder != nil {
			c.recorder.RecordOsmLatency(endpoint, 0, duration)
		}
		return nil, err
	}
	defer resp.Body.Close()

	if c.recorder != nil {
		c.recorder.RecordOsmLatency(endpoint, resp.StatusCode, duration)
	}

	osmResponse := &Response{
		httpResponse: resp,
		StatusCode:   resp.StatusCode,
	}

	// Check for X-Blocked header (complete service block by OSM)
	if blockedHeader := resp.Header.Get("X-Blocked"); blockedHeader != "" {
		slog.Error("osm.service.blocked",
			"component", "osm_api",
			"event", "blocked.detected",
			"blocked_header", blockedHeader,
			"severity", "CRITICAL",
			"action_required", "manual_investigation",
			"impact", "all_osm_api_calls_blocked",
			"endpoint", endpoint,
		)
		if c.rlStore != nil {
			c.rlStore.MarkOsmServiceBlocked(ctx)
		}
		return osmResponse, fmt.Errorf("%w: %s", ErrServiceBlocked, blockedHeader)
	}

	// Parse and store rate limit headers (per-user rate limits)
	remaining, limit, resetSeconds := parseRateLimitHeaders(resp.Header)
	if config.userId != nil {
		c.recorder.RecordRateLimit(config.userId, remaining, limit, resetSeconds)
	}
	osmResponse.Limits = UserRateLimitInfo{
		Remaining: remaining,
		Limit:     limit,
		ResetsAt:  time.Now().Add(time.Duration(resetSeconds) * time.Second),
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterStr := resp.Header.Get("Retry-After")
		blockedUntil := parseRetryAfterHeader(retryAfterStr, resetSeconds)
		if config.userId != nil && c.rlStore != nil {
			// Calculate the absolute time when the block expires
			c.rlStore.MarkUserTemporarilyBlocked(ctx, *config.userId, blockedUntil)
		}
		return osmResponse, &ErrUserBlocked{blockedUntil}
	}

	if resp.StatusCode != http.StatusOK {
		// Only read the body if it's an error and we need to log it.
		// We use a LimitReader to avoid reading excessive amounts of data into memory.
		// SECURITY: Redact response body for sensitive endpoints (e.g. OAuth)
		var logBody string
		if config.sensitive {
			logBody = "[REDACTED]"
		} else {
			const maxErrorBody = 4096
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			logBody = string(bodyBytes)
		}

		slog.Error("osm.api.error_response",
			"component", "osm_api",
			"event", "api.error",
			"endpoint", endpoint,
			"status_code", resp.StatusCode,
			"response_body", logBody,
			"duration_ms", duration.Milliseconds(),
		)
		return osmResponse, fmt.Errorf("OSM API error: %s - %s", resp.Status, logBody)
	}

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			slog.Error("osm.api.decode_error",
				"component", "osm_api",
				"event", "api.error",
				"endpoint", endpoint,
				"error", err,
			)
			return osmResponse, fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return osmResponse, nil
}

func parseRetryAfterHeader(str string, defaultSeconds int) time.Time {
	// OSM specify that they will send a time in seconds.
	// The HTTP standards also allow a HTTP date. Don't support this unless needed.
	retryAfter, err := strconv.Atoi(str)
	if err != nil {
		slog.Error("osm.api.parse_retry_after", "value", str, "error", err)
		retryAfter = defaultSeconds // Try the other limit header
	}
	if retryAfter > 0 {
		return time.Now().Add(time.Duration(retryAfter) * time.Second)
	}
	slog.Warn("osm.api.parse_retry_after_using_default")
	return time.Now().Add(fallbackUserBlockTime)
}

// parseRateLimitHeaders parses rate limit headers from OSM API response
// OSM uses standard rate limit headers as documented in docs/research/OSM-OAuth-Doc.md:
// - X-RateLimit-Limit: Maximum requests per hour (per authenticated user)
// - X-RateLimit-Remaining: Requests remaining before being blocked
// - X-RateLimit-Reset: Seconds until the rate limit resets
// Returns: remaining, limit, resetSeconds
func parseRateLimitHeaders(headers http.Header) (int, int, int) {
	// Parse OSM rate limit headers
	limitStr := headers.Get("X-RateLimit-Limit")
	remainingStr := headers.Get("X-RateLimit-Remaining")
	resetStr := headers.Get("X-RateLimit-Reset")

	if remainingStr == "" {
		// No rate limit headers present
		return 0, 0, 0
	}

	remaining, err := strconv.Atoi(remainingStr)
	if err != nil {
		slog.Warn("osm.api.invalid_rate_limit_header",
			"component", "osm_api",
			"header", "X-RateLimit-Remaining",
			"value", remainingStr,
			"error", err,
		)
		return 0, 0, 0
	}

	limit := 0
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	resetSeconds := 0
	if resetStr != "" {
		resetSeconds, _ = strconv.Atoi(resetStr)
	}

	slog.Info("osm.api.parse_rate_limit_header",
		"component", "osm_api",
		"limit", limit,
		"remaining", remaining,
		"reset", resetSeconds)

	return remaining, limit, resetSeconds
}
