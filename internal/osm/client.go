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
func parseRateLimitHeaders(headers http.Header) RateLimitInfo {
	info := RateLimitInfo{}

	// Parse OSM rate limit headers
	limitStr := headers.Get("X-RateLimit-Limit")
	remainingStr := headers.Get("X-RateLimit-Remaining")
	resetStr := headers.Get("X-RateLimit-Reset")

	if remainingStr == "" {
		// No rate limit headers present
		return info
	}

	remaining, err := strconv.Atoi(remainingStr)
	if err != nil {
		slog.Warn("osm.api.invalid_rate_limit_header",
			"component", "osm_api",
			"header", "X-RateLimit-Remaining",
			"value", remainingStr,
			"error", err,
		)
		return info
	}
	info.Remaining = remaining

	if limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			info.Limit = limit
		}
	}

	if resetStr != "" {
		if reset, err := strconv.Atoi(resetStr); err == nil {
			info.ResetSeconds = reset
		}
	}

	return info
}
