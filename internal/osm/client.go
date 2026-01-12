package osm

import (
	"net/http"
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
