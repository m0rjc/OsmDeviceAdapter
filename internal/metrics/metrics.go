package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for monitoring OSM Device Adapter

var (
	// Rate limiting metrics (per-user)
	OSMRateLimitRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "osm_rate_limit_remaining",
		Help: "Remaining OSM API requests for user",
	}, []string{"user_id"})

	OSMRateLimitTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "osm_rate_limit_total",
		Help: "Total OSM API requests allowed per period",
	}, []string{"user_id"})

	// Blocking metrics (X-Blocked indicates complete service block, not per-user)
	OSMServiceBlocked = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "osm_service_blocked",
		Help: "OSM service block status (0=unblocked, 1=blocked by X-Blocked header)",
	})

	OSMBlockCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "osm_block_events_total",
		Help: "Total number of times OSM blocking was detected",
	})

	// OAuth metrics
	DeviceAuthRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "device_auth_requests_total",
		Help: "Device authorization requests by client and status",
	}, []string{"client_id", "status"})

	// API latency metrics
	OSMAPILatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "osm_api_request_duration_seconds",
		Help:    "OSM API request latency",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"endpoint", "status_code"})

	// Cache metrics
	CacheOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_operations_total",
		Help: "Cache operations by operation type and result",
	}, []string{"operation", "result"}) // operation: get|set, result: hit|miss|error

	// HTTP metrics
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by method, path, and status",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by method, path, and status",
	}, []string{"method", "path", "status"})
)
