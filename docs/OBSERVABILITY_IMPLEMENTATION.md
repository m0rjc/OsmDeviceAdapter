# Observability Implementation Plan for OSM Device Adapter

## Current Status

### Phase 1: Core Observability - 60% Complete
- ✅ **Step 1**: Add Structured Logging with log/slog - COMPLETED
- ✅ **Step 2**: Add Prometheus Metrics - COMPLETED
- ⏸️ **Step 3**: Deploy Monitoring Infrastructure - PENDING
- ✅ **Step 4**: Create Grafana Dashboards - COMPLETED
- ⏸️ **Step 5**: Configure Basic Alerting - PENDING

### Next Steps
1. Deploy monitoring infrastructure (Prometheus + Grafana) via Helm - See Step 3
2. Import the created dashboards into Grafana
3. Configure alerting rules in Grafana - See Step 5
4. Set up email notifications (SMTP configuration)

### Phase 2: Log Aggregation (Optional - Future)
- ⏸️ Not started - Can be added later when log querying needs arise

### Phase 3: Distributed Tracing (Optional - Future)
- ⏸️ Not started - Only needed if expanding to multiple services

---

## Context

**Environment**: MicroK8s on Ubuntu Server (small development PC)
**Current State**: Basic `log` package, no structured logging, no metrics, no observability
**Goal**: Implement structured logging to monitor rate limiting, blocking situations, and attack signs
**Approach**: Start minimal with phased implementation suitable for resource-constrained environment

## Critical Constraint: Resource-Limited Environment

Since you're running on a small development PC with MicroK8s, we'll use **lightweight, monolithic deployments** instead of the full distributed LGTM stack. This provides excellent observability without overwhelming your hardware.

---

## Recommended Stack (Optimized for MicroK8s)

### Phase 1: Structured Logging + Metrics (Immediate - Week 1-2)
- **Application**: `log/slog` (Go standard library, zero overhead)
- **Metrics**: Prometheus client library + `/metrics` endpoint
- **Infrastructure**: Single Prometheus instance (lightweight)
- **Visualization**: Single Grafana instance (self-hosted) OR Grafana Cloud free tier (hosted)
  - **Grafana Cloud Option**: Zero infrastructure cost, 10k metrics series, 50GB logs/month, built-in alerting
  - **Self-Hosted Option**: Full control, unlimited metrics, but requires ~256Mi RAM and 5Gi storage
- **Storage**: Local persistent volumes (no S3/MinIO needed for self-hosted)

### Phase 2: Log Aggregation (Optional - Month 2)
- **Option A - Self-Hosted**: Loki monolithic mode (single binary, ~200MB RAM) + Alloy for collection
- **Option B - Grafana Cloud**: Use hosted Loki (50GB/month free, zero infrastructure)
- **Storage**: Local filesystem for self-hosted OR cloud for Grafana Cloud option

### Phase 3: Distributed Tracing (Future - If Needed)
- **Tempo**: Monolithic mode
- **OpenTelemetry**: Add when application complexity increases

---

## Implementation Plan

## PHASE 1: Core Observability (Start Here)

### Step 1: Add Structured Logging with log/slog ✅ COMPLETED

**Why slog**: Zero dependencies, officially supported, excellent performance, OpenTelemetry-ready

**Files to Modify**:
1. ✅ `cmd/server/main.go`
   - Initialize slog with JSON handler
   - Replace all `log.Println()` calls with structured slog calls
   - Add log levels (Info, Warn, Error)
   - Configure log output format based on environment

2. ✅ `internal/handlers/device_oauth.go`
   - Add structured logging for device authorization flow:
     - `device.authorize.start` - Client ID, device code, user code
     - `device.authorize.pending` - Polling attempts
     - `device.authorize.approved` - User approval events
     - `device.authorize.denied` - Denial events with reasons
   - Log security-relevant fields: client_id, user_code, ip_address (if available)

3. ✅ `internal/handlers/oauth.go`
   - Add structured logging for OSM OAuth flow:
     - `oauth.authorize.start` - OSM authorization initiated
     - `oauth.callback.success` - Token exchange successful
     - `oauth.callback.error` - OAuth errors with details
   - Log oauth_state, user_id, section_id

4. ✅ `internal/osm/client.go` ⚠️ **CRITICAL FOR MONITORING**
   - Add structured logging for rate limiting (per-user):
     - `osm.api.rate_limit.info` - Log remaining requests from headers
     - `osm.api.rate_limit.warning` - When remaining < 100 requests (per user)
     - `osm.api.rate_limit.critical` - When remaining < 20 requests (per user)
   - Add structured logging for blocking detection (complete service block):
     - `osm.api.blocked.detected` - X-Blocked header present (CRITICAL - entire service blocked, not per-user)
   - Add structured logging for API errors:
     - `osm.api.error` - Non-2xx responses with status code, endpoint
   - Log fields: user_id, section_id, endpoint, status_code, rate_limit_remaining, rate_limit_limit, blocked_header
   - **Important**: Rate limits are per-user; X-Blocked indicates complete service-wide block

5. ✅ `internal/handlers/patrol.go` (if exists, or wherever patrol fetching happens)
   - Add structured logging for patrol score fetching:
     - `patrol.fetch.start` - User ID, section ID
     - `patrol.fetch.cache_hit` - Cache hit with TTL remaining
     - `patrol.fetch.cache_miss` - Cache miss, fetching from OSM
     - `patrol.fetch.success` - Successful fetch with record count
     - `patrol.fetch.error` - Errors with details

6. ✅ `internal/server/server.go`
   - Replace the TODO comment and implement loggingMiddleware:
     ```go
     func loggingMiddleware(next http.Handler) http.Handler {
         return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
             start := time.Now()

             // Wrap ResponseWriter to capture status code
             sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

             next.ServeHTTP(sw, r)

             slog.Info("http.request",
                 "method", r.Method,
                 "path", r.URL.Path,
                 "status", sw.statusCode,
                 "duration_ms", time.Since(start).Milliseconds(),
                 "remote_addr", r.RemoteAddr,
                 "user_agent", r.UserAgent(),
             )
         })
     }

     type statusWriter struct {
         http.ResponseWriter
         statusCode int
     }

     func (sw *statusWriter) WriteHeader(code int) {
         sw.statusCode = code
         sw.ResponseWriter.WriteHeader(code)
     }
     ```

**New Files to Create**:
- ✅ `internal/logging/logger.go`
  - Centralized logger initialization
  - Environment-based configuration (JSON for production, human-readable for dev)
  - Log level configuration from environment variable
  - Helper functions for structured logging contexts

**Environment Variables to Add**:
```bash
LOG_LEVEL=info          # debug, info, warn, error
LOG_FORMAT=json         # json, text
```

### Step 2: Add Prometheus Metrics ✅

**Why Prometheus**: Industry standard, lightweight, excellent Grafana integration

**Note**: Go runtime metrics (go_memstats_*, process_*, etc.) are **disabled** to reduce metric cardinality and stay within Grafana Cloud free tier limits. Only application-specific metrics are exported.

**Files to Modify**:
1. ✅ `cmd/server/main.go`
   - Add Prometheus handler with custom registry: `promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})`
   - Custom registry excludes automatic Go runtime collectors

2. ✅ Create `internal/metrics/metrics.go`
   - Create custom registry: `var Registry = prometheus.NewRegistry()`
   - Define all Prometheus metrics (registered manually, not with promauto):
     ```go
     // Rate limiting metrics
     var (
         OSMRateLimitRemaining = prometheus.NewGaugeVec(prometheus.GaugeOpts{
             Name: "osm_rate_limit_remaining",
             Help: "Remaining OSM API requests for user",
         }, []string{"user_id"})

         OSMRateLimitTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
             Name: "osm_rate_limit_total",
             Help: "Total OSM API requests allowed",
         }, []string{"user_id"})

         // Blocking metrics (X-Blocked indicates complete service block)
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
             Help: "Device authorization requests",
         }, []string{"client_id", "status"})

         // API latency
         OSMAPILatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
             Name: "osm_api_request_duration_seconds",
             Help: "OSM API request latency",
             Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
         }, []string{"endpoint", "status_code"})

         // Cache metrics
         CacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
             Name: "cache_operations_total",
             Help: "Cache operations",
         }, []string{"operation", "result"}) // operation: get|set, result: hit|miss|error

         // HTTP metrics
         HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
             Name: "http_request_duration_seconds",
             Help: "HTTP request latency",
             Buckets: prometheus.DefBuckets,
         }, []string{"method", "path", "status"})
     )
     ```

3. ✅ `internal/osm/client.go`
   - Instrument OSM API calls:
     - Record latency: `OSMAPILatency.WithLabelValues(endpoint, strconv.Itoa(statusCode)).Observe(duration.Seconds())`
     - Update rate limit gauges after each call (per-user rate limits)
     - Set blocked gauge and increment counter when X-Blocked detected:
       - `OSMServiceBlocked.Set(1)` when X-Blocked present (complete block)
       - `OSMBlockCount.Inc()` to track frequency
       - Clear gauge `OSMServiceBlocked.Set(0)` when successful response without X-Blocked

4. ✅ `internal/handlers/device_oauth.go`
   - Increment `DeviceAuthRequests` counter with labels
   - Track approval/denial rates

5. ✅ `internal/db/redis.go` (or wherever Redis is used)
   - Instrument cache operations:
     - `CacheHits.WithLabelValues("get", "hit").Inc()`
     - `CacheHits.WithLabelValues("get", "miss").Inc()`
     - `CacheHits.WithLabelValues("set", "success").Inc()`

6. ✅ `internal/server/server.go`
   - Update loggingMiddleware to also record HTTP metrics:
     ```go
     HTTPRequestDuration.WithLabelValues(
         r.Method,
         r.URL.Path,
         strconv.Itoa(sw.statusCode),
     ).Observe(time.Since(start).Seconds())
     ```

**Update Helm Chart**:
1. ✅ `chart/values.yaml`
   - Add metrics configuration:
     ```yaml
     metrics:
       enabled: true
       port: 9090

     prometheus:
       enabled: true
       scrapeInterval: 30s
     ```

2. ✅ `chart/templates/service.yaml`
   - Add metrics port:
     ```yaml
     - name: metrics
       port: 9090
       targetPort: 9090
       protocol: TCP
     ```

3. ✅ `chart/templates/deployment.yaml`
   - Add metrics port to container spec:
     ```yaml
     ports:
       - name: http
         containerPort: 8080
       - name: metrics
         containerPort: 9090
     ```
   - Add Prometheus scrape annotations:
     ```yaml
     annotations:
       prometheus.io/scrape: "true"
       prometheus.io/port: "9090"
       prometheus.io/path: "/metrics"
     ```

4. ✅ Create `chart/templates/servicemonitor.yaml`
   - ServiceMonitor resource for Prometheus Operator (if using it):
     ```yaml
     {{- if and .Values.metrics.enabled .Values.prometheus.serviceMonitor.enabled }}
     apiVersion: monitoring.coreos.com/v1
     kind: ServiceMonitor
     metadata:
       name: {{ include "osm-device-adapter.fullname" . }}
     spec:
       selector:
         matchLabels:
           {{- include "osm-device-adapter.selectorLabels" . | nindent 6 }}
       endpoints:
       - port: metrics
         interval: {{ .Values.prometheus.scrapeInterval }}
     {{- end }}
     ```

**Dependencies to Add** (update `go.mod`):
```go
require (
    github.com/prometheus/client_golang v1.20.5
)
```

### Step 3: Deploy Monitoring Infrastructure (Using Helm) ⏸️ PENDING

**Two Deployment Options**: You can either self-host Grafana in your cluster OR use Grafana Cloud's free tier. See the comparison below to decide which approach fits your needs.

#### Option A: Self-Hosted Grafana (Full Control)

**Why Helm**: Following your existing infrastructure pattern, we'll use the community-maintained `kube-prometheus-stack` Helm chart instead of raw manifests. This provides Prometheus, Grafana, and Alertmanager with sensible defaults.

**Namespace Strategy**: Deploy monitoring to the same namespace as your application, or create a dedicated `monitoring` namespace. The choice is yours - using the same namespace simplifies networking.

**Install kube-prometheus-stack via Helm** (no need for raw Kubernetes YAML):

```bash
# Add Prometheus community Helm repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Enable MicroK8s addons if not already enabled
microk8s enable dns storage metallb

# Install kube-prometheus-stack to your chosen namespace
# Option 1: Same namespace as your application (simpler)
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace <your-app-namespace> \
  --values k8s/monitoring/kube-prometheus-stack-values.yaml

# Option 2: Dedicated monitoring namespace (recommended for production)
kubectl create namespace monitoring
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --values k8s/monitoring/kube-prometheus-stack-values.yaml

# Verify deployment
kubectl get pods -n <namespace> -l release=monitoring

# Get Grafana 'admin' user password by running:
kubectl --namespace monitoring get secrets monitoring-grafana -o jsonpath="{.data.admin-password}" | base64 -d ; echo

# Access Grafana local instance:

export POD_NAME=$(kubectl --namespace monitoring get pod -l "app.kubernetes.io/name=grafana,app.kubernetes.io/instance=monitoring" -oname)
kubectl --namespace monitoring port-forward $POD_NAME 3000

# Get your grafana admin user password by running:

kubectl get secret --namespace monitoring -l app.kubernetes.io/component=admin-secret -o jsonpath="{.items[0].data.admin-password}" | base64 --decode ; echo

# Access Grafana (LoadBalancer IP or port-forward)
kubectl get svc -n <namespace> monitoring-grafana
# Or port-forward:
kubectl port-forward -n <namespace> svc/monitoring-grafana 3000:80
```

**Note**: The kube-prometheus-stack Helm chart automatically creates and configures Prometheus, Grafana, and all necessary components. No need for separate YAML manifests!

#### Option B: Grafana Cloud Free Tier (Hosted Solution)

**Why Grafana Cloud**: Zero infrastructure maintenance, automatic updates, built-in alerting, and free tier includes 10,000 series for Prometheus metrics, 50GB logs, and 50GB traces. Perfect for small to medium deployments.

**Grafana Cloud Free Tier Limits** (as of 2025):
- **Metrics**: 10,000 active series (Prometheus metrics)
- **Logs**: 50GB ingestion per month
- **Traces**: 50GB ingestion per month
- **Dashboards**: Unlimited
- **Alerts**: Unlimited
- **Users**: 3 free users
- **Retention**: 14 days for metrics, 14 days for logs

**When to Use Grafana Cloud**:
- ✅ You want to minimize infrastructure management overhead
- ✅ Your metrics volume fits within 10,000 series (likely sufficient for this project)
- ✅ You want built-in alerting without SMTP configuration
- ✅ You prefer cloud-based access without port-forwarding or VPN
- ✅ You want automatic Grafana updates and security patches

**When to Use Self-Hosted**:
- ✅ You need complete data sovereignty and privacy
- ✅ You exceed free tier limits (> 10k series or > 50GB logs/month)
- ✅ You want zero external dependencies
- ✅ You have strict compliance requirements (data must stay on-premises)

**Setup Instructions for Grafana Cloud**:

1. **Create Grafana Cloud Account**:
   - Visit https://grafana.com/auth/sign-up/create-user
   - Sign up for free tier (no credit card required)
   - Create your stack (e.g., "osm-monitoring")

2. **Deploy Only Prometheus** (skip Grafana in cluster):

   Create `k8s/monitoring/prometheus-only-values.yaml`:
   ```yaml
   # kube-prometheus-stack with Grafana disabled
   # Prometheus will send metrics to Grafana Cloud

   prometheus:
     prometheusSpec:
       retention: 2h  # Keep only 2h locally, rely on cloud for long-term
       resources:
         requests:
           cpu: 100m
           memory: 256Mi
         limits:
           cpu: 500m
           memory: 512Mi
       storageSpec:
         volumeClaimTemplate:
           spec:
             storageClassName: microk8s-hostpath
             accessModes: ["ReadWriteOnce"]
             resources:
               requests:
                 storage: 5Gi  # Smaller storage since we keep only 2h

       # Remote write to Grafana Cloud
       remoteWrite:
       - url: https://prometheus-prod-XX-XXXX-XX.grafana.net/api/prom/push
         basicAuth:
           username:
             name: grafana-cloud-credentials
             key: username
           password:
             name: grafana-cloud-credentials
             key: password
         writeRelabelConfigs:
         - sourceLabels: [__name__]
           regex: 'up|osm_.*|device_auth_.*|cache_.*|http_request_.*'
           action: keep  # Only send relevant metrics to save on cardinality

   # Disable Grafana (using cloud instead)
   grafana:
     enabled: false

   # Keep Alertmanager for local alerting (optional)
   alertmanager:
     enabled: true
     alertmanagerSpec:
       resources:
         requests:
           cpu: 50m
           memory: 64Mi
         limits:
           cpu: 100m
           memory: 128Mi
   ```

3. **Get Your Grafana Cloud Credentials**:
   - In Grafana Cloud, go to "Connections" → "Add new connection"
   - Select "Hosted Prometheus"
   - Copy your remote write endpoint and credentials
   - Note the URL, username (instance ID), and password (API key)

4. **Create Kubernetes Secret for Grafana Cloud**:
   ```bash
   # Replace with your actual Grafana Cloud credentials
   kubectl create secret generic grafana-cloud-credentials \
     --namespace monitoring \
     --from-literal=username='YOUR_INSTANCE_ID' \
     --from-literal=password='YOUR_API_KEY'
   ```

5. **Deploy Prometheus with Remote Write**:
   ```bash
   # Add Prometheus community Helm repo (if not already added)
   helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
   helm repo update

   # Create monitoring namespace
   kubectl create namespace monitoring

   # Install kube-prometheus-stack (Prometheus only, no Grafana)
   helm install monitoring prometheus-community/kube-prometheus-stack \
     --namespace monitoring \
     --values k8s/monitoring/prometheus-only-values.yaml

   # Verify Prometheus is running
   kubectl get pods -n monitoring -l app.kubernetes.io/name=prometheus

   # Check remote write is working (should see no errors)
   kubectl logs -n monitoring -l app.kubernetes.io/name=prometheus -f | grep "remote_write"
   ```

6. **Configure Dashboards in Grafana Cloud**:
   - Log into your Grafana Cloud instance at `https://YOUR_STACK.grafana.net`
   - The Prometheus datasource should be automatically configured
   - Import your dashboards from `k8s/monitoring/dashboards/*.json`
   - Create alert rules in Grafana Cloud UI (no SMTP needed - uses built-in notifications)

7. **Set Up Alerting in Grafana Cloud**:
   - Go to Alerting → Contact Points
   - Add contact points: Email, Slack, PagerDuty, webhook, etc.
   - No SMTP configuration needed - Grafana Cloud handles email delivery
   - Create alert rules for:
     - Rate limit critical: `osm_rate_limit_remaining < 20`
     - OSM service blocked: `osm_service_blocked == 1`
     - High error rate: `rate(http_request_duration_seconds_count{status=~"5.."}[5m]) > 0.05`
     - Service down: `up{job="osm-device-adapter"} == 0`

8. **Verify Metrics Are Flowing**:
   - In Grafana Cloud, go to Explore
   - Select the Prometheus datasource
   - Query: `up{job="osm-device-adapter"}`
   - You should see your application metrics

**Cost Monitoring**:
- Check your usage in Grafana Cloud → "Usage & Billing"
- Monitor active series count: `count({__name__=~".+"})`
- If approaching 10k series limit, adjust `writeRelabelConfigs` to filter more aggressively

**Grafana Cloud Resource Savings** (compared to self-hosted):
- **No Grafana pod**: Saves ~128-256Mi memory, ~100-200m CPU
- **Reduced Prometheus storage**: 5Gi vs 10Gi (only 2h retention vs full retention)
- **Total savings**: ~128-256Mi memory, ~100-200m CPU, ~5Gi storage

**Migration Path**:
- Start with Grafana Cloud (free tier)
- If you outgrow limits (> 10k series), migrate to self-hosted Grafana
- Prometheus data can be queried from either Grafana Cloud or self-hosted Grafana
- Dashboards can be exported/imported easily

---

#### Comparison: Self-Hosted vs Grafana Cloud

| Feature | Self-Hosted Grafana | Grafana Cloud (Free Tier) |
|---------|-------------------|---------------------------|
| **Setup Complexity** | Medium (Helm chart) | Low (just remote write config) |
| **Infrastructure Cost** | ~256Mi RAM, ~200m CPU, 5-10Gi storage | ~0Mi RAM, ~0m CPU in cluster |
| **Maintenance** | You manage updates, backups | Fully managed by Grafana Labs |
| **Data Location** | On-premises (full control) | Grafana Cloud (EU/US regions) |
| **Metrics Limits** | Unlimited (limited by storage) | 10,000 active series |
| **Logs** | Optional (Loki setup required) | 50GB/month included |
| **Retention** | Configurable (limited by storage) | 14 days (metrics & logs) |
| **Alerting** | Need SMTP or webhook setup | Built-in (email, Slack, PagerDuty) |
| **Access** | Port-forward or LoadBalancer | HTTPS from anywhere |
| **Users** | Unlimited | 3 free users |
| **Dashboards** | Unlimited | Unlimited |
| **Cost** | Infrastructure cost only | Free (within limits) |
| **Best For** | Privacy, high cardinality, on-prem | Quick setup, low maintenance |

**Recommendation for OSM Device Adapter**:
- **Start with Grafana Cloud** if your metric cardinality is under 10k series and you want minimal setup
- **Use Self-Hosted** if you need complete data control or plan to exceed free tier limits

---

### Step 4: Create Grafana Dashboards ✅ COMPLETED

Create `k8s/monitoring/dashboards/`:

**Dashboard 1: OSM Rate Limiting & Blocking** (`rate-limiting.json`)
- Panels:
  - Rate limit remaining by user (gauge + time series)
  - Users approaching rate limit (< 100 requests)
  - Users in critical state (< 20 requests)
  - Blocked users counter (over time)
  - Rate limit utilization percentage
  - Top users by API usage

**Dashboard 2: OAuth Service Health** (`oauth-health.json`)
- Panels:
  - Device authorization request rate
  - OAuth approval/denial ratio
  - Device code expiry events
  - Token refresh events
  - Error rate by endpoint
  - HTTP request latency (P50, P95, P99)

**Dashboard 3: OSM API Performance** (`osm-api.json`)
- Panels:
  - OSM API latency by endpoint
  - OSM API error rate
  - Cache hit/miss ratio
  - Patrol fetch success rate
  - Request volume by endpoint

**Dashboard 4: Security Events** (`security.json`)
- Panels:
  - Blocked user alerts (X-Blocked header)
  - Failed authorization attempts
  - Unusual traffic patterns
  - Error rate spikes
  - Client ID validation failures

**Import Instructions**: Create JSON files and import via Grafana UI (Configuration → Dashboards → Import)

### Step 5: Configure Basic Alerting ⏸️ PENDING

For now, configure alerts in Grafana UI (Alerting → Alert Rules):

**Critical Alerts**:
1. **Rate Limit Critical (Per-User)**: `osm_rate_limit_remaining < 20`
   - Severity: Critical
   - Channel: Email (once configured)
   - Description: Specific user approaching rate limit

2. **OSM Service Blocked (Complete Block)**: `osm_service_blocked == 1`
   - Severity: Critical
   - Channel: Email
   - Description: X-Blocked header detected - entire service blocked by OSM, immediate action required

3. **High Error Rate**: `rate(http_request_duration_seconds_count{status=~"5.."}[5m]) > 0.05`
   - Severity: Warning
   - Channel: Email

4. **Service Down**: `up{job="osm-device-adapter"} == 0`
   - Severity: Critical
   - Channel: Email

**Email Configuration** (Grafana SMTP):
Update Grafana deployment environment variables:
```yaml
- name: GF_SMTP_ENABLED
  value: "true"
- name: GF_SMTP_HOST
  value: "smtp.gmail.com:587"  # Or your SMTP server
- name: GF_SMTP_USER
  valueFrom:
    secretKeyRef:
      name: grafana-smtp
      key: user
- name: GF_SMTP_PASSWORD
  valueFrom:
    secretKeyRef:
      name: grafana-smtp
      key: password
- name: GF_SMTP_FROM_ADDRESS
  value: "alerts@yourdomain.com"
```

Create secret:
```bash
kubectl create secret generic grafana-smtp \
  --from-literal=user='your-email@gmail.com' \
  --from-literal=password='your-app-password'
```

---

## PHASE 2: Log Aggregation (Optional - Future)

**When to add**: Once you have logs you want to query/search beyond basic kubectl logs

**Two Options**: Self-hosted Loki OR Grafana Cloud Logs (both covered below)

### Option A: Self-Hosted Loki

Deploy **Loki using Helm** (monolithic mode for resource efficiency):

### Step 1: Deploy Loki with Helm

Create `k8s/monitoring/loki-values.yaml`:
```yaml
# Custom values for Loki in monolithic mode
# Optimized for MicroK8s

loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1
  storage:
    type: 'filesystem'
  schemaConfig:
    configs:
      - from: 2024-01-01
        store: tsdb
        object_store: filesystem
        schema: v13
        index:
          prefix: index_
          period: 24h
  limits_config:
    retention_period: 30d

# Single instance deployment
deploymentMode: SingleBinary
singleBinary:
  replicas: 1
  persistence:
    enabled: true
    storageClassName: microk8s-hostpath
    size: 20Gi
  resources:
    requests:
      cpu: 100m
      memory: 200Mi
    limits:
      cpu: 500m
      memory: 512Mi

# Disable components not needed in monolithic mode
ingester:
  replicas: 0
distributor:
  replicas: 0
querier:
  replicas: 0
queryFrontend:
  replicas: 0
gateway:
  enabled: false

# Monitoring
monitoring:
  serviceMonitor:
    enabled: false
  selfMonitoring:
    enabled: false
    grafanaAgent:
      installOperator: false

test:
  enabled: false
```

**Install Loki**:
```bash
# Add Grafana Helm repo
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Install Loki
helm install loki grafana/loki \
  --namespace <your-namespace> \
  --values k8s/monitoring/loki-values.yaml
```

### Step 2: Deploy Grafana Alloy for Log Collection

Create `k8s/monitoring/alloy.yaml`:
```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: alloy-config
data:
  config.alloy: |
    discovery.kubernetes "pods" {
      role = "pod"
    }

    discovery.relabel "osm_adapter" {
      targets = discovery.kubernetes.pods.targets

      rule {
        source_labels = ["__meta_kubernetes_namespace"]
        target_label  = "namespace"
      }

      rule {
        source_labels = ["__meta_kubernetes_pod_name"]
        target_label  = "pod"
      }

      rule {
        source_labels = ["__meta_kubernetes_pod_container_name"]
        target_label  = "container"
      }
    }

    loki.source.kubernetes "pods" {
      targets    = discovery.relabel.osm_adapter.output
      forward_to = [loki.write.local.receiver]
    }

    loki.write "local" {
      endpoint {
        url = "http://loki:3100/loki/api/v1/push"
      }
    }

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: alloy
spec:
  selector:
    matchLabels:
      app: alloy
  template:
    metadata:
      labels:
        app: alloy
    spec:
      serviceAccountName: alloy
      containers:
      - name: alloy
        image: grafana/alloy:v1.5.2
        args:
          - run
          - /etc/alloy/config.alloy
          - --server.http.listen-addr=0.0.0.0:12345
          - --storage.path=/var/lib/alloy/data
        ports:
        - containerPort: 12345
        resources:
          requests:
            memory: "128Mi"
            cpu: "50m"
          limits:
            memory: "256Mi"
            cpu: "200m"
        volumeMounts:
        - name: config
          mountPath: /etc/alloy
        - name: varlog
          mountPath: /var/log
          readOnly: true
        - name: varlibdockercontainers
          mountPath: /var/lib/docker/containers
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: alloy-config
      - name: varlog
        hostPath:
          path: /var/log
      - name: varlibdockercontainers
        hostPath:
          path: /var/lib/docker/containers

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: alloy

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: alloy
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/proxy
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: alloy
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: alloy
subjects:
- kind: ServiceAccount
  name: alloy
  namespace: default
```

### Step 3: Add Loki Datasource to Grafana

Update Grafana datasources ConfigMap to include:
```yaml
- name: Loki
  type: loki
  access: proxy
  url: http://loki:3100
  editable: true
```

### Step 4: Create Log-Based Dashboards

**Dashboard: OSM API Logs** (`osm-logs.json`)
- Log panels with LogQL queries:
  - All rate limit warnings: `{app="osm-device-adapter"} |= "rate_limit" | json | severity="WARN"`
  - Blocked users: `{app="osm-device-adapter"} |= "X-Blocked" | json`
  - OAuth errors: `{app="osm-device-adapter"} |= "oauth" | json | severity="ERROR"`
  - Slow requests: `{app="osm-device-adapter"} | json | duration_ms > 1000`

---

### Option B: Grafana Cloud Logs (Hosted Loki)

**Why use Grafana Cloud for Logs**: If you're already using Grafana Cloud for metrics, adding logs is seamless. Free tier includes 50GB/month log ingestion and 14-day retention.

**Setup Instructions**:

1. **Enable Grafana Cloud Logs in Your Stack**:
   - In Grafana Cloud, your stack should already have Loki enabled
   - Go to "Connections" → "Add new connection" → "Hosted Logs"
   - Copy the Loki push URL and credentials

2. **Deploy Grafana Alloy to Send Logs to Cloud**:

   Create `k8s/monitoring/alloy-cloud-values.yaml`:
   ```yaml
   alloy:
     configMap:
       create: true
       content: |
         // Discover Kubernetes pods
         discovery.kubernetes "pods" {
           role = "pod"
           namespaces {
             names = ["default", "monitoring"]  // Adjust to your namespaces
           }
         }

         // Relabel discovered targets
         discovery.relabel "osm_adapter" {
           targets = discovery.kubernetes.pods.targets

           // Only scrape osm-device-adapter pods
           rule {
             source_labels = ["__meta_kubernetes_pod_label_app"]
             regex         = "osm-device-adapter"
             action        = "keep"
           }

           rule {
             source_labels = ["__meta_kubernetes_namespace"]
             target_label  = "namespace"
           }

           rule {
             source_labels = ["__meta_kubernetes_pod_name"]
             target_label  = "pod"
           }

           rule {
             source_labels = ["__meta_kubernetes_pod_container_name"]
             target_label  = "container"
           }
         }

         // Scrape logs from Kubernetes
         loki.source.kubernetes "pods" {
           targets    = discovery.relabel.osm_adapter.output
           forward_to = [loki.write.grafana_cloud.receiver]
         }

         // Send logs to Grafana Cloud
         loki.write "grafana_cloud" {
           endpoint {
             url = "https://logs-prod-XXX.grafana.net/loki/api/v1/push"

             basic_auth {
               username = env("LOKI_USERNAME")  // Instance ID
               password = env("LOKI_PASSWORD")  // API Key
             }
           }
         }

     # Resource configuration
     resources:
       limits:
         cpu: 200m
         memory: 256Mi
       requests:
         cpu: 50m
         memory: 128Mi

     # Environment variables for Grafana Cloud credentials
     extraEnv:
       - name: LOKI_USERNAME
         valueFrom:
           secretKeyRef:
             name: grafana-cloud-credentials
             key: loki-username
       - name: LOKI_PASSWORD
         valueFrom:
           secretKeyRef:
             name: grafana-cloud-credentials
             key: loki-password
   ```

3. **Update Grafana Cloud Credentials Secret**:
   ```bash
   # Add Loki credentials to existing secret
   kubectl delete secret grafana-cloud-credentials -n monitoring
   kubectl create secret generic grafana-cloud-credentials \
     --namespace monitoring \
     --from-literal=username='YOUR_PROMETHEUS_INSTANCE_ID' \
     --from-literal=password='YOUR_PROMETHEUS_API_KEY' \
     --from-literal=loki-username='YOUR_LOKI_INSTANCE_ID' \
     --from-literal=loki-password='YOUR_LOKI_API_KEY'
   ```

4. **Deploy Alloy**:
   ```bash
   # Add Grafana Helm repo
   helm repo add grafana https://grafana.github.io/helm-charts
   helm repo update

   # Install Alloy as DaemonSet
   helm install alloy grafana/alloy \
     --namespace monitoring \
     --values k8s/monitoring/alloy-cloud-values.yaml
   ```

5. **Verify Logs Are Flowing**:
   - In Grafana Cloud, go to "Explore"
   - Select the "Logs" datasource (Loki)
   - Query: `{namespace="default", app="osm-device-adapter"}`
   - You should see your application logs

6. **Create Log-Based Dashboards**:
   - Import or create dashboards in Grafana Cloud
   - Use the same LogQL queries from the self-hosted section
   - Example: `{app="osm-device-adapter"} |= "rate_limit" | json | severity="WARN"`

**Grafana Cloud Logs Benefits**:
- No Loki infrastructure to maintain
- 50GB/month free tier (monitor usage in "Usage & Billing")
- Automatic retention management (14 days)
- Integrated with metrics for correlated dashboards
- Built-in log aggregation and search

**Cost Monitoring for Logs**:
- Track ingestion in Grafana Cloud → "Usage & Billing" → "Logs"
- Reduce verbosity if approaching 50GB/month limit
- Filter logs in Alloy config to send only important logs (e.g., WARN and ERROR levels)

**Filtering Logs to Stay Under Limits**:

Update Alloy config to filter by log level:
```alloy
loki.process "filter_logs" {
  forward_to = [loki.write.grafana_cloud.receiver]

  stage.json {
    expressions = {
      level = "severity",
    }
  }

  // Only send WARN and ERROR logs to cloud
  stage.match {
    selector = "{level=~\"WARN|ERROR\"}"
    action   = "keep"
  }
}

loki.source.kubernetes "pods" {
  targets    = discovery.relabel.osm_adapter.output
  forward_to = [loki.process.filter_logs.receiver]  // Changed
}
```

This reduces log volume significantly by only sending warnings and errors to the cloud.

---

## PHASE 3: Distributed Tracing (Optional - Far Future)

**When to add**: If you need to trace requests across multiple services or debug complex performance issues

Deploy **Tempo in Monolithic Mode**:
- Add OpenTelemetry SDK to Go application
- Deploy single Tempo instance
- Configure trace-to-log correlation in Grafana

This can be implemented later if the need arises.

---

## Testing Strategy

### Unit Tests
Add unit tests for:
- Structured logging format validation
- Metrics registration (ensure all metrics are properly defined)
- Logger initialization with different configurations

### Integration Tests
Add integration tests for:
- Verify `/metrics` endpoint returns valid Prometheus format
- Verify structured logs contain expected fields
- Test rate limit gauge updates

### Manual Testing
1. Deploy updated application to MicroK8s
2. Verify logs appear in kubectl logs with JSON format
3. Verify `/metrics` endpoint is accessible
4. Verify Prometheus is scraping metrics
5. Verify Grafana can query Prometheus
6. Trigger rate limit scenarios and verify metrics update
7. Test email alerting (once SMTP configured)

---

## Deployment Steps

### 1. Application Changes
```bash
# Add Prometheus dependency
go get github.com/prometheus/client_golang@v1.20.5

# Run tests
go test ./...

# Build and push new Docker image
docker build -t your-registry/osm-device-adapter:v1.1.0 .
docker push your-registry/osm-device-adapter:v1.1.0

# Update Helm chart values
helm upgrade osm-device-adapter ./chart \
  --set image.tag=v1.1.0 \
  --set metrics.enabled=true \
  --set env.LOG_FORMAT=json \
  --set env.LOG_LEVEL=info
```

### 2. Deploy Monitoring Infrastructure (via Helm)
```bash
# Add Prometheus community Helm repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Deploy to your chosen namespace (same as app or dedicated 'monitoring' namespace)
helm install monitoring prometheus-community/kube-prometheus-stack \
  --namespace <your-namespace> \
  --values k8s/monitoring/kube-prometheus-stack-values.yaml \
  --create-namespace

# Wait for all pods to be ready
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=grafana -n <your-namespace> --timeout=300s

# Get Grafana service
kubectl get svc -n <your-namespace> monitoring-grafana

# Port-forward for local access
kubectl port-forward -n <your-namespace> svc/monitoring-grafana 3000:80
```

### 3. Configure Grafana
```bash
# Access Grafana at http://localhost:3000
# Default credentials: admin/admin (change immediately)

# Import dashboards from k8s/monitoring/dashboards/
# Configure alert notification channels
# Set up SMTP for email alerts
```

---

## Resource Requirements (MicroK8s)

### Option A: Self-Hosted (Full Stack)

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit | Storage |
|-----------|------------|-----------|----------------|--------------|---------|
| **Application** | 100m | 500m | 128Mi | 512Mi | - |
| **Prometheus** | 100m | 500m | 256Mi | 512Mi | 10Gi PVC |
| **Grafana** | 100m | 200m | 128Mi | 256Mi | 5Gi PVC |
| **Loki** (Phase 2) | 100m | 500m | 200Mi | 512Mi | 20Gi PVC |
| **Alloy** (Phase 2) | 50m | 200m | 128Mi | 256Mi | - |
| **TOTAL** | 450m-550m | 1.9-2.4 CPU | 612-940Mi | 1.5-2.2Gi | 35Gi |

**Minimum Hardware Recommendation**: 4 CPU cores, 8GB RAM, 50GB disk

### Option B: Grafana Cloud (Reduced Footprint)

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit | Storage |
|-----------|------------|-----------|----------------|--------------|---------|
| **Application** | 100m | 500m | 128Mi | 512Mi | - |
| **Prometheus** | 100m | 500m | 256Mi | 512Mi | 5Gi PVC (2h retention) |
| **Grafana Cloud** | - | - | - | - | - (hosted) |
| **Loki** (Phase 2) | - | - | - | - | - (use Grafana Cloud) |
| **TOTAL** | 200m | 1 CPU | 384Mi | 1Gi | 5Gi |

**Minimum Hardware Recommendation**: 2 CPU cores, 4GB RAM, 20GB disk

**Savings with Grafana Cloud**: ~250m CPU, ~556Mi memory, ~30Gi storage

---

## Key Structured Log Fields to Include

### Rate Limiting
- `component`: "osm_api"
- `event`: "rate_limit.warning" | "rate_limit.critical"
- `user_id`: string
- `section_id`: string
- `rate_limit_remaining`: int
- `rate_limit_limit`: int
- `rate_limit_reset`: timestamp

### Blocking Detection
- `component`: "osm_api"
- `event`: "blocked.detected"
- `blocked_header`: string (value of X-Blocked header - indicates complete service block)
- `severity`: "CRITICAL"
- `note`: X-Blocked indicates a complete service block (not per-user), all OSM API calls are blocked

### OAuth Flow
- `component`: "device_oauth" | "web_oauth"
- `event`: "authorize.start" | "authorize.approved" | "authorize.denied" | "token.issued"
- `client_id`: string
- `device_code`: string (hashed or truncated)
- `user_code`: string
- `user_id`: string (if available)

### API Performance
- `component`: "osm_api"
- `event`: "api.request"
- `endpoint`: string
- `method`: string
- `status_code`: int
- `duration_ms`: int
- `cache_hit`: bool

---

## Documentation Updates Needed

1. **README.md**: Add "Observability" section explaining:
   - How to access Grafana
   - How to view metrics
   - How to query logs
   - Available dashboards

2. **docs/MONITORING.md** (new file): Comprehensive guide covering:
   - Prometheus metrics reference
   - Grafana dashboard usage
   - Alert configuration
   - Troubleshooting observability issues
   - LogQL query examples

3. **docs/HELM.md**: Update with:
   - Metrics configuration options
   - Observability-related environment variables
   - ServiceMonitor configuration

---

## Critical Files Summary

### Must Modify (Phase 1):
1. ✅ `cmd/server/main.go` - Initialize slog and metrics
2. ✅ `internal/osm/client.go` - Rate limiting and blocking logs/metrics
3. ✅ `internal/handlers/device_oauth.go` - OAuth flow logs/metrics
4. ✅ `internal/server/server.go` - HTTP request logging
5. ✅ `chart/templates/deployment.yaml` - Prometheus annotations, metrics port
6. ✅ `chart/templates/service.yaml` - Expose metrics port
7. ✅ `chart/values.yaml` - Add observability config
8. ✅ `go.mod` - Add prometheus dependency

### Must Create (Phase 1):
1. ✅ `internal/logging/logger.go` - Logger initialization
2. ✅ `internal/metrics/metrics.go` - Prometheus metrics definitions
3. ⏸️ `k8s/monitoring/kube-prometheus-stack-values.yaml` - Helm values for monitoring stack
4. ✅ `k8s/monitoring/dashboards/*.json` - Grafana dashboards
5. ⏸️ `docs/MONITORING.md` - Monitoring documentation
6. ⏸️ `docs/HELM.md` - Update with monitoring stack deployment instructions

### Optional (Phase 2):
1. ⏸️ `k8s/monitoring/loki-values.yaml` - Loki Helm values for log aggregation
2. ⏸️ `k8s/monitoring/alloy-values.yaml` - Alloy Helm values for log collection

---

## Monitoring Your Specific Requirements

### Rate Limiting Monitoring
**Logs**:
```go
slog.Warn("rate limit approaching threshold",
    "component", "osm_api",
    "event", "rate_limit.warning",
    "user_id", userID,
    "section_id", sectionID,
    "rate_limit_remaining", remaining,
    "rate_limit_limit", limit,
    "rate_limit_reset", resetTime,
    "threshold", 100,
)
```

**Metrics**:
- `osm_rate_limit_remaining{user_id="123"}` - Current remaining requests
- Alert when `< 20` for critical, `< 100` for warning

**Grafana Panel**: Time series showing rate limit consumption per user

### Blocking Situations
**Logs** (X-Blocked indicates COMPLETE service block):
```go
// X-Blocked header means the entire service is blocked by OSM, not a specific user
slog.Error("OSM service completely blocked",
    "component", "osm_api",
    "event", "blocked.detected",
    "blocked_header", blockedHeaderValue,
    "severity", "CRITICAL",
    "action_required", "manual_investigation",
    "impact", "all_osm_api_calls_blocked",
)
```

**Metrics**:
- `osm_service_blocked` - Gauge (0=unblocked, 1=blocked)
- Alert immediately when blocked (critical severity)

**Grafana Panel**: Alert panel showing current block status + log panel showing block detection events

### Attack Detection
**Logs**:
```go
// Failed authorization attempts
slog.Warn("device authorization denied",
    "component", "device_oauth",
    "event", "authorize.denied",
    "client_id", clientID,
    "reason", "invalid_client_id",
    "remote_addr", r.RemoteAddr,
)

// Suspicious patterns (high frequency from same IP)
slog.Warn("high request rate detected",
    "component", "http_middleware",
    "event", "rate_spike",
    "remote_addr", addr,
    "request_count_5m", count,
    "threshold", 100,
)
```

**Metrics**:
- `device_auth_requests_total{status="denied"}` - Failed auth attempts
- `http_request_duration_seconds_count` grouped by remote_addr

**Grafana Panel**: Rate of failed attempts + heatmap of request sources

---

## Email Alerting Configuration

Once you're ready to set up email alerts:

1. **Choose SMTP Provider**:
   - Gmail (with app password)
   - SendGrid
   - AWS SES
   - Your own SMTP server

2. **Create Kubernetes Secret**:
```bash
kubectl create secret generic grafana-smtp \
  --from-literal=user='your-email@gmail.com' \
  --from-literal=password='your-app-password'
```

3. **Update Grafana Deployment** (add to env):
```yaml
- name: GF_SMTP_ENABLED
  value: "true"
- name: GF_SMTP_HOST
  value: "smtp.gmail.com:587"
- name: GF_SMTP_USER
  valueFrom:
    secretKeyRef:
      name: grafana-smtp
      key: user
- name: GF_SMTP_PASSWORD
  valueFrom:
    secretKeyRef:
      name: grafana-smtp
      key: password
- name: GF_SMTP_FROM_ADDRESS
  value: "alerts@yourdomain.com"
```

4. **Configure in Grafana**:
   - Alerting → Contact Points → New contact point
   - Type: Email
   - Addresses: your-email@example.com
   - Test and save

5. **Create Alert Rules** linking to email contact point

---

## Success Criteria

After Phase 1 implementation, you should be able to:

✅ View structured JSON logs via `kubectl logs`
✅ Query application metrics via Prometheus UI
✅ Visualize metrics in Grafana dashboards
✅ See rate limiting status per user in real-time
✅ Get alerted when users approach rate limits
✅ See blocked user events immediately
✅ Track OAuth authorization success/failure rates
✅ Monitor API latency and error rates
✅ View cache hit/miss ratios
✅ Receive email alerts for critical events (once SMTP configured)

---

## Next Steps After Phase 1

1. **Tune Alerts**: Adjust thresholds based on actual usage patterns
2. **Add Custom Dashboards**: Create dashboards for specific use cases
3. **Implement Phase 2**: Add Loki for log aggregation and advanced querying
4. **Consider Distributed Tracing**: If you expand to multiple services
5. **Export Dashboards**: Version control your Grafana dashboards as JSON
6. **Document Runbooks**: Create incident response procedures for common alerts

---

## Estimated Timeline

- **Phase 1 Application Changes**: 2-3 days
  - Day 1: Implement slog structured logging
  - Day 2: Add Prometheus metrics
  - Day 3: Testing and refinement

- **Phase 1 Infrastructure**: 1-2 days
  - Day 1: Deploy Prometheus and Grafana
  - Day 2: Create dashboards and configure alerts

- **Total Phase 1**: ~1 week for full implementation and testing

- **Phase 2** (Optional): 2-3 days when needed

---

## Resources & References

- **Go slog**: https://pkg.go.dev/log/slog
- **Prometheus Go Client**: https://github.com/prometheus/client_golang
- **Grafana Dashboards**: https://grafana.com/grafana/dashboards/
- **MicroK8s Monitoring**: https://microk8s.io/docs/addon-observability
- **LogQL (for Phase 2)**: https://grafana.com/docs/loki/latest/query/
- **PromQL**: https://prometheus.io/docs/prometheus/latest/querying/basics/
