package osm

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache defines the interface for Redis operations needed by the OSM client
type RedisCache interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
}

type Client struct {
	baseURL     string
	httpClient  *http.Client
	rlStore     RateLimitStore
	recorder    LatencyRecorder
	redisCache  RedisCache
}

func NewClient(baseURL string, rlStore RateLimitStore, recorder LatencyRecorder, redisCache RedisCache) *Client {
	return &Client{
		baseURL:     baseURL,
		rlStore:     rlStore,
		recorder:    recorder,
		redisCache:  redisCache,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// OSMDomain returns the OSM domain
func (c *Client) OSMDomain() string {
	return c.baseURL
}
