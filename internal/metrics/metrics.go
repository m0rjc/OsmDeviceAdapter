package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus metrics for monitoring OSM Device Adapter

// Registry is a custom Prometheus registry that excludes Go runtime metrics
// to reduce the number of metrics sent to Grafana
var Registry = prometheus.NewRegistry()

var (
	// Rate limiting metrics (per-user)
	OSMRateLimitRemaining = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "osm_rate_limit_remaining",
		Help: "Remaining OSM API requests for user",
	}, []string{"user_id"})

	OSMRateLimitTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "osm_rate_limit_total",
		Help: "Total OSM API requests allowed per period",
	}, []string{"user_id"})

	OSMRateLimitResetSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "osm_rate_limit_reset_seconds",
		Help: "Seconds until the OSM API rate limit resets for user",
	}, []string{"user_id"})

	// Blocking metrics (X-Blocked indicates complete service block, not per-user)
	OSMServiceBlocked = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "osm_service_blocked",
		Help: "OSM service block status (0=unblocked, 1=blocked by X-Blocked header)",
	})

	OSMBlockCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "osm_block_events_total",
		Help: "Total number of times OSM blocking was detected",
	})

	// OAuth metrics
	DeviceAuthRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "device_auth_requests_total",
		Help: "Device authorization requests by client and status",
	}, []string{"client_id", "status"})

	// API latency metrics
	OSMAPILatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "osm_api_request_duration_seconds",
		Help:    "OSM API request latency",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"endpoint", "status_code"})

	// Cache metrics
	CacheOperations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_operations_total",
		Help: "Cache operations by operation type and result",
	}, []string{"operation", "result"}) // operation: get|set, result: hit|miss|error

	// HTTP metrics
	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by method, path, and status",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by method, path, and status",
	}, []string{"method", "path", "status"})

	// Score outbox metrics
	ScoreOutboxEntriesCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "score_outbox_entries_created_total",
		Help: "Total number of score outbox entries created",
	})

	ScoreOutboxEntriesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "score_outbox_entries_processed_total",
		Help: "Total number of score outbox entries processed by final status",
	}, []string{"status"}) // status: completed|failed|auth_revoked

	ScoreOutboxSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "score_outbox_sync_duration_seconds",
		Help:    "Duration of score outbox sync operations",
		Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 30},
	})

	ScoreOutboxPendingEntries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "score_outbox_pending_entries",
		Help: "Number of pending entries in the score outbox",
	})

	// User credentials metrics
	UserCredentialsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "user_credentials_active",
		Help: "Number of active user credentials stored",
	})

	UserCredentialsCleaned = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "user_credentials_cleaned_total",
		Help: "Total number of stale user credentials cleaned up",
	})
)

func init() {
	// Register all metrics with the custom registry
	Registry.MustRegister(OSMRateLimitRemaining)
	Registry.MustRegister(OSMRateLimitTotal)
	Registry.MustRegister(OSMRateLimitResetSeconds)
	Registry.MustRegister(OSMServiceBlocked)
	Registry.MustRegister(OSMBlockCount)
	Registry.MustRegister(DeviceAuthRequests)
	Registry.MustRegister(OSMAPILatency)
	Registry.MustRegister(CacheOperations)
	Registry.MustRegister(HTTPRequestDuration)
	Registry.MustRegister(HTTPRequestsTotal)
	Registry.MustRegister(ScoreOutboxEntriesCreated)
	Registry.MustRegister(ScoreOutboxEntriesProcessed)
	Registry.MustRegister(ScoreOutboxSyncDuration)
	Registry.MustRegister(ScoreOutboxPendingEntries)
	Registry.MustRegister(UserCredentialsActive)
	Registry.MustRegister(UserCredentialsCleaned)
}
