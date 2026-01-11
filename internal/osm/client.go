package osm

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	rlStore    RateLimitStore
	recorder   LatencyRecorder
}

func NewClient(baseURL string, rlStore RateLimitStore, recorder LatencyRecorder) *Client {
	return &Client{
		baseURL:  baseURL,
		rlStore:  rlStore,
		recorder: recorder,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// OSMDomain returns the OSM domain
func (c *Client) OSMDomain() string {
	return c.baseURL
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

	return remaining, limit, resetSeconds
}
