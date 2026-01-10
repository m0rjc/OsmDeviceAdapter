package osm

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
)

// PrometheusRateLimitDecorator is a decorator for RateLimitStore that records block metrics to Prometheus.
type PrometheusRateLimitDecorator struct {
	next RateLimitStore
}

func NewPrometheusRateLimitDecorator(next RateLimitStore) *PrometheusRateLimitDecorator {
	return &PrometheusRateLimitDecorator{next: next}
}

func (p *PrometheusRateLimitDecorator) MarkOsmServiceBlocked(ctx context.Context) {
	p.next.MarkOsmServiceBlocked(ctx)
	metrics.OSMServiceBlocked.Set(1)
}

func (p *PrometheusRateLimitDecorator) IsOsmServiceBlocked(ctx context.Context) bool {
	return p.next.IsOsmServiceBlocked(ctx)
}

func (p *PrometheusRateLimitDecorator) MarkUserTemporarilyBlocked(ctx context.Context, userId int, retryAfter time.Duration) {
	p.next.MarkUserTemporarilyBlocked(ctx, userId, retryAfter)
	metrics.OSMBlockCount.Inc()
}

func (p *PrometheusRateLimitDecorator) IsUserTemporarilyBlocked(userId int) bool {
	return p.next.IsUserTemporarilyBlocked(userId)
}

// PrometheusLatencyRecorder is LatencyRecorder that records latency metrics to Prometheus.
type PrometheusLatencyRecorder struct {
}

func NewPrometheusLatencyRecorder() *PrometheusLatencyRecorder {
	return &PrometheusLatencyRecorder{}
}

func (p *PrometheusLatencyRecorder) RecordOsmLatency(endpoint string, statusCode int, latency time.Duration) {
	status := "error"
	if statusCode > 0 {
		status = strconv.Itoa(statusCode)
	}
	metrics.OSMAPILatency.WithLabelValues(endpoint, status).Observe(latency.Seconds())

	// This should not happen unless the administrator has manually cleared the block so allowing the client to make
	// a request.
	if statusCode == 200 {
		metrics.OSMServiceBlocked.Set(0)
	}
}

func (p *PrometheusLatencyRecorder) RecordRateLimit(userId *int, remaining int, limitTotal int, limitResetSeconds int) {
	var userIdString string
	if userId != nil {
		userIdString = strconv.Itoa(*userId)
	} else {
		userIdString = "unknown"
	}

	metrics.OSMRateLimitRemaining.WithLabelValues(userIdString).Add(float64(remaining))
	if limitTotal > 0 {
		metrics.OSMRateLimitTotal.WithLabelValues(userIdString).Add(float64(limitTotal))
	}
	if limitResetSeconds > 0 {
		metrics.OSMRateLimitResetSeconds.WithLabelValues(userIdString).Add(float64(limitResetSeconds))
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
		"rate_limit_remaining", remaining,
		"rate_limit_limit", limitTotal,
		"rate_limit_reset_seconds", limitResetSeconds,
		"severity", severity,
	}

	slog.Log(context.Background(), logLevel, "osm.api.rate_limit", logAttrs...)
}
