# Prometheus Metrics Reference

OSM Device Adapter exposes Prometheus metrics on a separate internal server (`port 9090`) that should not be publicly accessible.

**Endpoint**: `GET http://<host>:9090/metrics`

## Custom Registry

The service uses a **custom Prometheus registry** (`metrics.Registry`) that excludes Go runtime metrics. This reduces the volume of data sent to Grafana Cloud, keeping only application-relevant metrics.

## Metrics

### HTTP Metrics

#### `http_request_duration_seconds` (Histogram)
HTTP request latency. Uses the actual URL path (high cardinality — prefer classified variant below for dashboards).

| Label | Values |
|-------|--------|
| `method` | HTTP method (GET, POST, …) |
| `path` | Request URL path |
| `status` | HTTP status code |

#### `http_requests_total` (Counter)
Total HTTP requests. Same labels as above (high cardinality).

#### `http_request_duration_classified_seconds` (Histogram)
HTTP request latency with **reduced cardinality**. Uses the matched route pattern (e.g. `/device/token`) rather than the raw URL path, and includes authentication context. **Preferred for dashboards and alerts.**

| Label | Values |
|-------|--------|
| `method` | HTTP method |
| `route` | Matched route pattern from `http.ServeMux` |
| `status` | HTTP status code |
| `auth_kind` | `none`, `device`, `session` |
| `auth_result` | `none`, `success`, `failure` |

Buckets: Prometheus defaults (5ms–10s).

#### `http_requests_classified_total` (Counter)
Total HTTP requests. Same labels as `http_request_duration_classified_seconds`.

---

### OSM API Metrics

#### `osm_api_request_duration_seconds` (Histogram)
Latency of outbound calls to the OSM API.

| Label | Values |
|-------|--------|
| `endpoint` | OSM API action name (e.g. `getPatrolScores`) |
| `status_code` | HTTP status code returned by OSM, or `error` on network failure |

Buckets: 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s.

#### `osm_rate_limit_remaining` (Gauge, per user)
Remaining OSM API requests in the current rate-limit window, as reported by the `X-RateLimit-Remaining` header. Updated after every OSM API call.

| Label | Values |
|-------|--------|
| `user_id` | OSM user ID, or `unknown` if not available |

#### `osm_rate_limit_total` (Gauge, per user)
Total OSM API requests allowed per rate-limit period (`X-RateLimit-Limit`).

| Label | Values |
|-------|--------|
| `user_id` | OSM user ID, or `unknown` |

#### `osm_rate_limit_reset_seconds` (Gauge, per user)
Seconds until the rate-limit window resets (`X-RateLimit-Reset`).

| Label | Values |
|-------|--------|
| `user_id` | OSM user ID, or `unknown` |

#### `osm_service_blocked` (Gauge)
Whether OSM has blocked the entire service via the `X-Blocked` response header.

| Value | Meaning |
|-------|---------|
| `0` | Service is unblocked |
| `1` | Service is blocked (set when `X-Blocked` is received; cleared on next successful 200 response) |

#### `osm_block_events_total` (Counter)
Incremented each time a per-user temporary block is recorded (i.e. when OSM returns a rate-limit block for a specific user).

---

### Device OAuth Metrics

#### `device_auth_requests_total` (Counter)
Device authorization flow events.

| Label | Values |
|-------|--------|
| `client_id` | Device client application ID |
| `status` | `denied` (disallowed client), `success` (token issued), `user_denied` (user rejected), `authorized` (device code authorized by user) |

---

### WebSocket Metrics

#### `websocket_connections_active` (Gauge)
Current number of live device WebSocket connections to this server instance.

#### `websocket_connections_total` (Counter)
Total WebSocket connection attempts since startup.

| Label | Values |
|-------|--------|
| `status` | `success`, `failure` |

#### `websocket_disconnections_total` (Counter)
Total WebSocket disconnections since startup.

| Label | Values |
|-------|--------|
| `reason` | `normal` (clean close), `read_error` (unexpected close from client), `write_error` (failed to write to client) |

---

### Cache Metrics

#### `cache_operations_total` (Counter)
Redis cache operations. Currently instrumented but not yet wired to any specific cache path — reserved for future use.

| Label | Values |
|-------|--------|
| `operation` | `get`, `set` |
| `result` | `hit`, `miss`, `error` |

---

## Structured Log Events Alongside Metrics

Rate-limit status is also emitted as structured log events (in addition to updating Prometheus gauges):

| Event | Log Level | Trigger |
|-------|-----------|---------|
| `osm.api.rate_limit` / `rate_limit.info` | INFO | `remaining >= 100` |
| `osm.api.rate_limit` / `rate_limit.warning` | WARN | `remaining < 100` |
| `osm.api.rate_limit` / `rate_limit.critical` | ERROR | `remaining < 20` |

Log fields: `rate_limit_remaining`, `rate_limit_limit`, `rate_limit_reset_seconds`, `severity`.

---

## Infrastructure

### Scraping

The Helm chart creates a `ServiceMonitor` resource (when `observability.metrics.enabled` and `observability.serviceMonitor.enabled` are both true) for the Prometheus Operator to discover and scrape the metrics port.

```yaml
# charts/osm-device-adapter/templates/servicemonitor.yaml
endpoints:
  - port: metrics
    interval: <observability.serviceMonitor.interval>
    path: <observability.metrics.path>   # default: /metrics
```

### Remote Write to Grafana Cloud

Prometheus is configured to remote-write metrics to Grafana Cloud. Only OSM Device Adapter metrics are forwarded — a relabel filter keeps only metrics matching:

```
^(osm_|device_auth_|cache_operations_|http_request|websocket_).*
```

This avoids sending Kubernetes infrastructure metrics to Grafana Cloud.

Credentials are read from the `grafana-cloud-credentials` Kubernetes secret (`username` and `password` keys).

See `k8s/monitoring/kube-prometheus-stack-values.yaml` for the full Prometheus stack configuration.

### Grafana Dashboards

Pre-built dashboards are in `k8s/monitoring/dashboards/`:

| File | Title | Focus |
|------|-------|-------|
| `security.json` | Security & Probing Detection | Probing and scanning detection |
| `rate-limiting.json` | OSM Rate Limiting & Blocking | OSM quota tracking and service-block monitoring |
| `websocket.json` | WebSocket Health & Mobile Connectivity | Connection lifecycle and mobile connectivity quality |
| `osm-api.json` | OSM API Performance | API latency, error rates, and cache behaviour |
| `oauth-health.json` | OAuth Service Health | Device auth flow request rates and HTTP latency |

---

## Dashboard Details

### Security & Probing Detection (`security.json`)

Designed to surface scanning and probing activity. Uses `http_requests_classified_total` (with `auth_kind` and `auth_result` labels) rather than the raw path-based metrics, giving lower cardinality and better signal-to-noise.

**Key panels:**

| Panel | What to look for |
|-------|-----------------|
| Unauthenticated Probes | `auth_kind="none"` requests returning 401/403 — the classic scanner fingerprint |
| 401/403 Rate by Route | Which specific routes are being targeted |
| Auth Failure Rate by Kind & Route | Distinguishes anonymous probes (`auth_kind=none`) from forged/expired tokens (`auth_kind=device/session`) |
| Denied Device Auth by Client ID | Unknown firmware attempting to register via device flow |
| Total Traffic Rate by Route | Spike detection — sudden volume on unusual routes |
| Top Probed Routes (table) | Sorted 4xx count per route in the last hour |
| Device Auth Flow Outcomes | `user_denied` spikes may indicate auth codes being phished and users spotting them |

**Note**: `device_auth_requests_total` status values are `denied` (unknown client ID), `success`, `user_denied`, and `authorized`. The previous dashboard queried `invalid_client` and `expired` which do not exist.

---

### OSM Rate Limiting & Blocking (`rate-limiting.json`)

Tracks quota consumption and service-block events. Threshold colours on the "remaining" panels are now correctly oriented: red (critical, <20) → yellow (warning, 20–99) → green (safe, ≥100).

**Key panels:**

| Panel | What to look for |
|-------|-----------------|
| Rate Limit Remaining by User | Line with threshold overlay — watch for users trending toward zero |
| Rate Limit Utilization % | Complementary view: how much of the quota is consumed |
| Time Until Rate Limit Reset | When capacity is restored per user — useful alongside the critical-state table |
| Users Approaching Limit (table) | Live view of users with <100 remaining |
| Users in Critical State (table) | Live view of users with <20 remaining — needs immediate attention |
| OSM API Latency Correlation | Correlate P95/P99 latency against remaining quota — rising latency before the block header arrives is a useful early warning |

---

### WebSocket Health & Mobile Connectivity (`websocket.json`)

Purpose-built for understanding WebSocket behaviour under varying mobile network conditions. The key insight: on poor mobile (3G, frequent handovers), expect **high churn** (devices reconnecting repeatedly) and **elevated read_error disconnect ratio**.

**Key panels:**

| Panel | What to look for |
|-------|-----------------|
| Active Connections | Baseline. Sharp drops = mass disconnect event (restart / network partition) |
| Connection Success Rate | Should be ≈100%. Any failures = WebSocket upgrade problems |
| Read Error Drops | Device dropped TCP without clean close — primary poor-mobile signal |
| Write Error Drops | Server couldn't write to device — connection silently stalled |
| Disconnections by Reason | `read_error` / `write_error` vs `normal` ratio over time |
| Connection Churn | New connections/min vs disconnections/min plotted together. When both are elevated and tracking each other, devices are cycling rapidly — characteristic of poor mobile connectivity |
| Error Disconnect Ratio | Single line: (read_error + write_error) / total disconnects. Near zero = good. >10% = noticeable mobile issues. >50% = significant connectivity problems |

**Reading the churn panel**: On stable connectivity (WiFi / good 4G), connections are long-lived — both lines sit near zero with only occasional steps. On poor mobile, you'll see both lines elevated and correlated, with spikes during handovers. The gap between them represents net connection change (positive = growing, negative = shrinking).
