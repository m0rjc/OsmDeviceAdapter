# OSM Device Adapter

A production-ready Go service that bridges OAuth Device Flow (RFC 8628) for IoT scoreboard devices with the Online Scout Manager (OSM) OAuth Web Flow. Provides secure, token-isolated API access to patrol scores without exposing OSM credentials to devices.

## Architecture Overview

OSM Device Adapter implements a **two-tier OAuth security architecture** that creates a trust boundary between untrusted IoT devices and the OSM API:

1. **Device OAuth Flow (RFC 8628)**: Public client authentication for input-constrained devices
   - Devices initiate authorization without requiring a client secret
   - User authorizes on a separate trusted device (phone/computer)
   - Device receives a server-generated access token (NOT the OSM token)

2. **OSM Web Flow (RFC 6749)**: Confidential client authentication with OSM
   - Service authenticates to OSM with client secret (server-side only)
   - OSM access/refresh tokens stored in database, never exposed to devices
   - Service acts as secure proxy for OSM API calls

3. **Token Isolation Layer**: Security boundary protecting OSM credentials
   - Device access tokens are server-generated and separate from OSM tokens
   - All OSM API calls made server-side using stored credentials
   - Automatic token refresh handled transparently
   - See [docs/security.md](docs/security.md) for comprehensive security architecture

### Architecture Diagram

```
┌──────────────┐                    ┌─────────────────────┐                    ┌─────────────┐
│  Scoreboard  │ Device Flow        │  OSM Device Adapter │  Authorization     │     OSM     │
│   Device     │ (RFC 8628)         │                     │  Code Flow         │  OAuth API  │
│              │◄──────────────────►│  ┌───────────────┐  │  (RFC 6749)        │             │
│  (Public     │ Device Access Token│  │ Main Server   │  │◄──────────────────►│  (Resource  │
│   Client)    │                    │  │ Port 8080     │  │ OSM Tokens         │   Server)   │
└──────────────┘                    │  │               │  │ (Server-Side Only) └─────────────┘
                                    │  │ • Device Auth │  │
                                    │  │ • Web OAuth   │  │
     User                           │  │ • API Proxy   │  │
  ┌──────────┐                      │  └───────────────┘  │
  │ Browser  │ Authorization        │                     │
  │          │◄────────────────────►│  ┌───────────────┐  │
  └──────────┘ Device Confirmation  │  │ Metrics Server│  │
               Flow                 │  │ Port 9090     │  │
                                    │  │ (Internal)    │  │
                                    │  │               │  │
                                    │  │ • /health     │  │
                                    │  │ • /ready      │  │
                                    │  │ • /metrics    │  │
                                    │  └───────────────┘  │
                                    │         │           │
                                    │  ┌──────▼──────┐    │
                                    │  │ PostgreSQL  │    │
                                    │  │  • Tokens   │    │
                                    │  │  • Sessions │    │
                                    │  └─────────────┘    │
                                    │         │           │
                                    │  ┌──────▼──────┐    │
                                    │  │   Redis     │    │
                                    │  │  • Rate Limit│   │
                                    │  │  • Caching  │    │
                                    │  └─────────────┘    │
                                    └─────────────────────┘
```

## Key Features

### Security (Production-Ready)
- ✅ **Token Isolation**: Device access tokens completely separate from OSM credentials
- ✅ **Rate Limiting**: Multi-layer protection (Cloudflare + Redis-based application limits)
- ✅ **Device Confirmation Flow**: Phishing protection with geographic anomaly detection
- ✅ **HTTPS Enforcement**: Automatic HTTP→HTTPS redirect with HSTS headers
- ✅ **CSRF Protection**: Session validation and OAuth state parameter verification
- ✅ **Automated Cleanup**: Scheduled removal of expired codes/sessions and inactive devices
- ✅ **Sliding Expiration**: Inactive devices automatically revoked after 30 days (configurable)

### OAuth Flows
- **Device Authorization (RFC 8628)**: For IoT devices without browser/keyboard
- **Web Authorization (RFC 6749)**: Secure OSM OAuth integration
- **Automatic Token Refresh**: Transparent OSM token renewal before expiry

### Observability
- **Structured Logging**: JSON logs with `log/slog`
- **Prometheus Metrics**: Request latency, rate limits, API performance
- **Health Checks**: Liveness (`/health`) and readiness (`/ready`) endpoints
- **Separate Metrics Server**: Internal-only port for monitoring (9090)

### Deployment
- **Kubernetes-Ready**: Helm chart with configurable values
- **Cloudflare Tunnel**: Integrated ingress with automatic HTTPS
- **High Availability**: Multi-replica deployment with health checks
- **Automated Maintenance**: Kubernetes CronJob for database cleanup

## Device Authorization Flow

1. **Device requests authorization**: `POST /device/authorize`
   - Returns `user_code` (e.g., "ABCD-EFGH") and `verification_uri`
   - Device displays code to user
   - Rate limited to prevent abuse (default: 6/minute per IP)

2. **User visits verification URL** and enters code
   - System shows **device confirmation page** with:
     - Original device IP address and country
     - Current user IP address and country
     - Warning if countries don't match (phishing detection)
     - Timestamp of device request

3. **User confirms or cancels authorization**
   - If confirmed: Redirected to OSM for OAuth authorization
   - If cancelled: Device code marked as "denied"
   - Session validated to prevent CSRF attacks

4. **OSM authorization completes**
   - Service exchanges authorization code for OSM access token (server-side)
   - User selects scout section
   - Service generates device access token (separate from OSM token)
   - OSM tokens stored in database, never exposed to device

5. **Device polls for token**: `POST /device/token`
   - Poll interval enforced (default: 5 seconds) with OAuth-compliant `slow_down` error
   - Rate limited per device code
   - Returns device access token when authorized

6. **Device uses token to access API**: `GET /api/v1/patrols`
   - Provides `Authorization: Bearer <device_access_token>` header
   - Service validates device token and makes OSM API call using server-side OSM token
   - Automatic OSM token refresh when near expiry (5-minute threshold)
   - Device last-used timestamp updated for sliding expiration tracking

## API Endpoints

### Device OAuth Flow

- `POST /device/authorize` - Initiate device authorization
  - **Rate Limited**: 6 requests/minute per IP (configurable)
  - Request: `{"client_id": "your-client-id"}`
  - Response: `device_code`, `user_code`, `verification_uri`, `expires_in`, `interval`

- `POST /device/token` - Poll for access token
  - **Rate Limited**: Enforces minimum poll interval (5 seconds)
  - Request: `{"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "device_code": "...", "client_id": "..."}`
  - Response: `access_token`, `token_type`, `expires_in` (when authorized)
  - Errors: `authorization_pending`, `slow_down`, `expired_token`, `access_denied`

- `GET /device` - User verification page
  - Query param: `user_code` (optional)
  - Displays form to enter user code

### OAuth Web Flow

- `GET /oauth/authorize` - Start OSM OAuth flow
  - **Rate Limited**: 1 request/10 seconds per IP when user_code provided (configurable)
  - Query param: `user_code`
  - Shows device confirmation page with security metadata

- `POST /oauth/confirm` - User confirms device authorization
  - **CSRF Protected**: Session validation
  - Redirects to OSM for authorization

- `GET /oauth/cancel` - User cancels authorization
  - Marks device code as "denied"
  - Shows cancellation confirmation

- `GET /oauth/callback` - OAuth callback from OSM
  - Exchanges authorization code for OSM tokens (server-side)
  - Creates secure session for section selection

- `POST /device/select-section` - User selects scout section
  - Completes authorization flow
  - Generates device access token

### Scoreboard API

- `GET /api/v1/patrols` - Get patrol scores
  - **Authentication Required**: `Authorization: Bearer <device_access_token>`
  - Returns patrol names and scores for authorized section
  - Response: `[{"patrol":"Lions","score":100}, ...]`
  - Updates device last-used timestamp

### Health & Monitoring

- `GET /health` - Basic health check (liveness probe)
  - Always returns 200 OK if server is running

- `GET /ready` - Readiness check
  - Verifies database and Redis connectivity
  - Returns 200 OK if all dependencies are healthy

- `GET /metrics` - Prometheus metrics (port 9090, internal only)
  - HTTP request metrics (duration, count by status/path)
  - OSM API latency metrics
  - Rate limit tracking metrics

## Configuration

All configuration is provided via environment variables. See [chart/values.yaml](chart/values.yaml) for Helm deployment configuration.

### Required Configuration

| Variable | Description | Example |
|----------|-------------|---------|
| `EXPOSED_DOMAIN` | Public domain where service is exposed | `https://osm-adapter.example.com` |
| `OSM_CLIENT_ID` | OSM OAuth client ID | `your-client-id` |
| `OSM_CLIENT_SECRET` | OSM OAuth client secret (**CRITICAL SECRET**) | `your-client-secret` |
| `DATABASE_URL` | PostgreSQL connection string (**CONFIDENTIAL**) | `postgres://user:pass@host:5432/dbname` |
| `ALLOWED_CLIENT_IDS` | Comma-separated list of allowed device client IDs | `scoreboard-v1,scoreboard-v2` |

### Optional Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Main HTTP server port | `8080` |
| `HOST` | HTTP server bind address | `0.0.0.0` |
| `OSM_DOMAIN` | Online Scout Manager base URL | `https://www.onlinescoutmanager.co.uk` |
| `OSM_REDIRECT_URI` | OAuth redirect URI | `{EXPOSED_DOMAIN}/oauth/callback` |
| `REDIS_URL` | Redis connection URL | `redis://localhost:6379` |
| `REDIS_KEY_PREFIX` | Redis key namespace | `osm_device_adapter:` |
| `DEVICE_CODE_EXPIRY` | Device code TTL in seconds | `600` (10 minutes) |
| `DEVICE_POLL_INTERVAL` | Recommended polling interval in seconds | `5` |
| `DEVICE_AUTHORIZE_RATE_LIMIT` | Rate limit for `/device/authorize` (requests/minute) | `6` |
| `DEVICE_ENTRY_RATE_LIMIT` | Rate limit for user code entry (format: `requests/seconds`) | `1/10` |
| `OAUTH_PATH_PREFIX` | OAuth web flow path prefix (for security obscurity) | `/oauth` |
| `DEVICE_PATH_PREFIX` | Device flow path prefix (for security obscurity) | `/device` |
| `API_PATH_PREFIX` | API endpoints path prefix (for security obscurity) | `/api` |

### Security Configuration

See [docs/security.md](docs/security.md) for comprehensive security documentation.

**Critical Secrets** (never commit to version control):
- `OSM_CLIENT_SECRET`: Authenticates service to OSM
- `DATABASE_URL`: Contains database credentials
- `REDIS_URL`: Contains Redis credentials (if using authentication)

**Public Configuration** (can be in version control):
- `OSM_CLIENT_ID`: Public OAuth client identifier
- `ALLOWED_CLIENT_IDS`: Device client IDs (extractable from device firmware)
- All other optional configuration

## Database Schema

### PostgreSQL Tables

#### `device_codes` Table

Tracks the complete OAuth device authorization lifecycle from initial request through to fully authorized API access.

```sql
CREATE TABLE device_codes (
    -- Core OAuth Device Flow Fields
    device_code VARCHAR(255) PRIMARY KEY,
    user_code VARCHAR(255) UNIQUE NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(50) DEFAULT 'pending',

    -- Token Isolation Architecture
    device_access_token VARCHAR(255) UNIQUE,  -- Server-generated token for device
    osm_access_token TEXT,                    -- OSM token (server-side only)
    osm_refresh_token TEXT,                   -- OSM refresh token (server-side only)
    osm_token_expiry TIMESTAMP,

    -- User Context
    section_id INTEGER,
    osm_user_id INTEGER,
    term_id INTEGER,
    term_checked_at TIMESTAMP,
    term_end_date TIMESTAMP,

    -- Device Confirmation Flow (Phishing Protection)
    device_request_ip VARCHAR(255),           -- Device IP at authorization time
    device_request_country VARCHAR(10),       -- Device country (CF-IPCountry)
    device_request_time TIMESTAMP,

    -- Sliding Expiration
    last_used_at TIMESTAMP,                   -- Last API request timestamp

    -- Indexes
    INDEX idx_device_codes_expires_at (expires_at),
    INDEX idx_device_codes_user_id (osm_user_id),
    INDEX idx_device_codes_last_used (last_used_at),
    INDEX idx_device_codes_term_end_date (term_end_date)
);
```

**Status Values**:
- `pending`: Waiting for user authorization
- `awaiting_section`: User authorized, needs to select section
- `authorized`: Fully authorized, device can access API
- `denied`: User explicitly denied authorization
- `revoked`: OSM access revoked (token refresh failed with 401)

#### `device_sessions` Table

Temporary web sessions during OAuth flow. Automatically deleted when device code is deleted (CASCADE).

```sql
CREATE TABLE device_sessions (
    session_id VARCHAR(255) PRIMARY KEY,      -- Also used as OAuth state parameter
    device_code VARCHAR(255) REFERENCES device_codes(device_code) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL             -- 15 minutes from creation
);
```

### Redis Data

Redis is used for:
- **Rate Limiting**: Per-IP and per-device-code rate limit counters
- **OSM API Metrics**: Rate limit tracking from OSM response headers
- **Caching**: Patrol scores and term information (optional, with TTL)

## Local Development

### Prerequisites

- Go 1.22 or later
- PostgreSQL 12+
- Redis 6+
- OSM OAuth application credentials

### Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/yourusername/OsmDeviceAdapter.git
   cd OsmDeviceAdapter
   ```

2. **Install dependencies**
   ```bash
   make deps
   ```

3. **Set up PostgreSQL and Redis** (using Docker)
   ```bash
   docker run -d --name postgres -p 5432:5432 \
     -e POSTGRES_PASSWORD=devpassword \
     -e POSTGRES_DB=osmdeviceadapter \
     postgres:15

   docker run -d --name redis -p 6379:6379 redis:7
   ```

4. **Set up environment variables**
   ```bash
   export EXPOSED_DOMAIN=http://localhost:8080
   export OSM_CLIENT_ID=your-client-id
   export OSM_CLIENT_SECRET=your-client-secret
   export DATABASE_URL=postgres://postgres:devpassword@localhost:5432/osmdeviceadapter?sslmode=disable
   export REDIS_URL=redis://localhost:6379
   export ALLOWED_CLIENT_IDS=dev-client-1
   ```

5. **Run the service**
   ```bash
   make run
   ```

   The service starts two HTTP servers:
   - **Main server**: http://localhost:8080 (device auth, web OAuth, API)
   - **Metrics server**: http://localhost:9090 (health checks, Prometheus metrics)

### Development Commands

```bash
make build          # Build binary to bin/server
make test           # Run all tests
make fmt            # Format code
make lint           # Lint code (requires golangci-lint)
go test -v ./...    # Run tests with verbose output
go test -cover ./...  # Run tests with coverage
```

### Testing the Flow

1. **Request device authorization**:
   ```bash
   curl -X POST http://localhost:8080/device/authorize \
     -H "Content-Type: application/json" \
     -d '{"client_id": "dev-client-1"}'
   ```

2. **Visit verification URL** in browser (from response)

3. **Poll for token** (in another terminal):
   ```bash
   curl -X POST http://localhost:8080/device/token \
     -H "Content-Type: application/json" \
     -d '{
       "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
       "device_code": "<device_code_from_step_1>",
       "client_id": "dev-client-1"
     }'
   ```

4. **Fetch patrol scores** (after authorization):
   ```bash
   curl http://localhost:8080/api/v1/patrols \
     -H "Authorization: Bearer <access_token_from_step_3>"
   ```

## Kubernetes Deployment

### Prerequisites

- Kubernetes cluster (1.24+)
- Helm 3.x
- External PostgreSQL and Redis (or deploy in-cluster)
- (Optional) Cloudflare Tunnel for ingress

### Deployment with Helm (Recommended)

1. **Build and push Docker image**
   ```bash
   export DOCKER_REGISTRY=your-registry.example.com
   export DOCKER_TAG=v1.0.0
   make docker-push
   ```

2. **Create production values file**
   ```bash
   cp chart/values-example.yaml values-production.yaml
   ```

3. **Edit `values-production.yaml`** with your configuration:
   ```yaml
   image:
     repository: your-registry.example.com/osm-device-adapter
     tag: "v1.0.0"

   config:
     exposedDomain: "https://osm-adapter.your-domain.com"
     allowedClientIds:
       - "scoreboard-device-v1"

   database:
     url: "postgres://user:pass@postgres-host:5432/dbname"

   redis:
     url: "redis://redis-host:6379"

   osm:
     clientId: "your-osm-client-id"
     clientSecret: "your-osm-client-secret"  # Better: use Kubernetes Secret

   # Automated cleanup configuration
   cleanup:
     enabled: true
     schedule: "0 2 * * *"  # Daily at 2 AM
     unusedThresholdDays: 30  # Revoke devices unused for 30 days
   ```

4. **Install with Helm**
   ```bash
   helm install osm-device-adapter ./chart \
     --namespace osm-adapter \
     --create-namespace \
     -f values-production.yaml
   ```

5. **Verify deployment**
   ```bash
   # Check pods
   kubectl get pods -n osm-adapter -l app.kubernetes.io/name=osm-device-adapter

   # View logs
   kubectl logs -n osm-adapter -l app.kubernetes.io/name=osm-device-adapter

   # Check service
   kubectl get svc -n osm-adapter osm-device-adapter

   # Check CronJob (automated cleanup)
   kubectl get cronjob -n osm-adapter
   ```

### Helm Commands

```bash
# Upgrade deployment
helm upgrade osm-device-adapter ./chart \
  --namespace osm-adapter \
  -f values-production.yaml

# Uninstall
helm uninstall osm-device-adapter --namespace osm-adapter

# Lint chart
make helm-lint

# View templated manifests (dry-run)
make helm-template

# Show default values
make helm-values
```

### Monitoring

The deployment includes health checks and Prometheus metrics:

```bash
# Check readiness
kubectl exec -n osm-adapter deployment/osm-device-adapter -- wget -qO- http://localhost:9090/ready

# View metrics
kubectl port-forward -n osm-adapter svc/osm-device-adapter 9090:9090
# Visit http://localhost:9090/metrics

# View cleanup job logs
kubectl logs -n osm-adapter job/osm-device-adapter-cleanup-<timestamp>
```

### Cloudflare Tunnel Integration

See [docs/CLOUDFLARE_SETUP.md](docs/CLOUDFLARE_SETUP.md) for detailed Cloudflare Tunnel configuration.

**Quick setup**:
1. Keep `ingress.enabled: false` in Helm values
2. Add route to your Cloudflare Tunnel configuration:
   ```yaml
   ingress:
     - hostname: osm-adapter.your-domain.com
       service: http://osm-device-adapter.osm-adapter.svc.cluster.local:80
   ```
3. Cloudflare provides automatic HTTPS termination

## Security

### Production-Ready Security Features

This service implements comprehensive security controls suitable for production deployment:

- **Multi-Layer Rate Limiting**: Cloudflare ingress + Redis-based application limits
- **Token Isolation Architecture**: Device access tokens completely separate from OSM credentials
- **HTTPS Enforcement**: Automatic redirect with HSTS headers (1-year max-age)
- **Device Confirmation Flow**: Phishing protection with geographic anomaly detection
- **CSRF Protection**: Session validation and OAuth state parameter verification
- **Audit Logging**: All security events logged with structured logging
- **Automated Cleanup**: Scheduled removal of expired data and inactive devices
- **Sliding Expiration**: Devices unused for 30 days automatically revoked

### Security Model

**Token Isolation** (Core Security Property):
- Devices receive server-generated access tokens, NOT OSM tokens
- OSM access/refresh tokens stored in database, never exposed to devices
- Service makes OSM API calls server-side using stored credentials
- If device is compromised, attacker gains only limited API access, not OSM OAuth credentials

**Client ID Validation**:
- `ALLOWED_CLIENT_IDS` whitelist controls which applications can request authorization
- Device client IDs are public (extractable from firmware) - this is by design
- Whitelist provides access control and DoS mitigation, not authentication security

**Rate Limiting**:
- Layer 1: Cloudflare rate limiting at ingress
- Layer 2: Redis-based application rate limiting
- Configurable limits on all sensitive endpoints
- OAuth-compliant error responses (`slow_down` for polling)

### Credential Security

**Critical Secrets** (server-side only):
- `OSM_CLIENT_SECRET`: Authenticates service to OSM API
- `DATABASE_URL`: Database connection credentials
- OSM access/refresh tokens: Stored in database, never logged or exposed

**Safe Logging**:
- Device codes: Only first 8 characters logged (truncated hash)
- Access tokens: Never logged
- OSM tokens: Never logged
- Client IDs and user codes: Logged for audit trail (public/ephemeral)

### Security Documentation

For comprehensive security architecture, threat model, and improvement roadmap, see:
- **[docs/security.md](docs/security.md)** - Complete security documentation

### Security Status

**Production Ready** for personal and small-scale deployments. All critical security features implemented and tested.

**Outstanding Tasks** (lower priority):
- OSM token revocation handling (graceful handling when user revokes access)
- Git secrets pre-commit hooks
- Migration to Kubernetes Secrets (currently using values file)
- Advanced monitoring and alerting

## Automated Maintenance

### Database Cleanup

The deployment includes an automated cleanup CronJob that runs daily to maintain database hygiene and security:

**What gets cleaned up**:
1. **Expired device codes**: Codes past their expiry time (`expires_at`)
2. **Expired sessions**: OAuth web sessions past 15-minute expiry
3. **Unused devices**: Devices with no API activity for configurable period (default: 30 days)

**Configuration** (in Helm values):
```yaml
cleanup:
  enabled: true
  schedule: "0 2 * * *"  # Daily at 2 AM (cron format)
  unusedThresholdDays: 30  # Days of inactivity before device revocation
```

**Security Benefits**:
- Limits exposure window for compromised device tokens
- Automatic revocation of abandoned devices
- Prevents database bloat and improves query performance
- Reduces attack surface by removing stale credentials

**Manual Cleanup** (for testing or emergency):
```bash
kubectl create job --from=cronjob/osm-device-adapter-cleanup manual-cleanup-1 -n osm-adapter
kubectl logs -n osm-adapter job/manual-cleanup-1
```

## Observability

### Structured Logging

All logs are output as structured JSON using Go's `log/slog`:

```json
{
  "time": "2024-01-15T10:30:00Z",
  "level": "INFO",
  "msg": "device.token.issued",
  "device_code_hash": "abc12345",
  "client_id": "scoreboard-v1",
  "osm_user_id": 12345
}
```

**Log Event Patterns**:
- `http.request`: HTTP request handling
- `device.authorize`: Device authorization initiated
- `device.token.issued`: Device token successfully issued
- `device.confirmation.shown`: Confirmation page displayed to user
- `device.confirmation.accepted`: User confirmed authorization
- `device.confirmation.cancelled`: User cancelled authorization
- `osm.api.request`: OSM API call made
- `osm.token.refresh`: OSM access token refreshed

### Prometheus Metrics

Available on metrics server (port 9090) at `/metrics`:

**HTTP Metrics**:
- `http_request_duration_seconds`: Request latency histogram (by method, path, status)
- `http_requests_total`: Request counter (by method, path, status)

**OSM API Metrics**:
- `osm_api_latency_seconds`: OSM API call latency histogram
- `osm_rate_limit_remaining`: Current rate limit remaining for user

**Custom Metrics**:
- Device authorization attempts
- Token issuance counts
- Rate limit hits

### Health Checks

**Liveness Probe** (`/health`):
- Always returns 200 OK if process is running
- Used by Kubernetes to restart crashed pods

**Readiness Probe** (`/ready`):
- Checks database connectivity
- Checks Redis connectivity
- Returns 200 OK only if all dependencies are healthy
- Used by Kubernetes to route traffic only to healthy pods

## Troubleshooting

### Common Issues

**OAuth Flow Not Completing**:
```bash
# Check service logs
kubectl logs -n osm-adapter -l app.kubernetes.io/name=osm-device-adapter

# Verify configuration
kubectl get configmap -n osm-adapter osm-device-adapter -o yaml

# Check EXPOSED_DOMAIN matches actual public URL
```

**Database Connection Issues**:
```bash
# Check database connectivity from pod
kubectl exec -n osm-adapter deployment/osm-device-adapter -- \
  wget -qO- http://localhost:9090/ready

# View database connection errors
kubectl logs -n osm-adapter -l app.kubernetes.io/name=osm-device-adapter | grep database
```

**Redis Connection Issues**:
```bash
# Check Redis connectivity
kubectl exec -n osm-adapter deployment/osm-device-adapter -- \
  wget -qO- http://localhost:9090/ready

# If Redis is down, service will log warnings but continue operating
# (rate limiting will be disabled as graceful degradation)
```

**Rate Limiting Too Aggressive**:
```bash
# Adjust rate limits in values file and upgrade:
# config:
#   deviceAuthorizeRateLimit: 10  # Increase from default 6

helm upgrade osm-device-adapter ./chart -f values-production.yaml
```

**Device Token Expired/Revoked**:
```bash
# Check device code status in database
kubectl exec -n osm-adapter deployment/osm-device-adapter -- psql $DATABASE_URL \
  -c "SELECT device_code, status, last_used_at FROM device_codes WHERE device_access_token = 'token...';"

# Status will be:
# - "authorized": Active and valid
# - "revoked": OSM access was revoked
# - Device may have been cleaned up if unused for 30+ days
```

**Cleanup Job Not Running**:
```bash
# Check CronJob configuration
kubectl get cronjob -n osm-adapter osm-device-adapter-cleanup -o yaml

# Check recent job executions
kubectl get jobs -n osm-adapter

# Manually trigger cleanup
kubectl create job --from=cronjob/osm-device-adapter-cleanup manual-test -n osm-adapter
kubectl logs -n osm-adapter job/manual-test
```

### Debug Mode

Enable verbose logging by setting environment variable:
```yaml
# In Helm values
env:
  - name: LOG_LEVEL
    value: "DEBUG"
```

## Documentation

- **[docs/security.md](docs/security.md)** - Comprehensive security architecture, threat model, and roadmap
- **[docs/HELM.md](docs/HELM.md)** - Detailed Helm chart usage and configuration
- **[docs/CLOUDFLARE_SETUP.md](docs/CLOUDFLARE_SETUP.md)** - Cloudflare Tunnel integration guide
- **[docs/OBSERVABILITY_IMPLEMENTATION.md](docs/OBSERVABILITY_IMPLEMENTATION.md)** - Monitoring setup details
- **[docs/research/OSM-OAuth-Doc.md](docs/research/OSM-OAuth-Doc.md)** - OSM API documentation research
- **[CLAUDE.md](CLAUDE.md)** - Development guide for Claude Code AI assistant

## License

[Your License Here]

## Contributing

[Your Contributing Guidelines Here]

## Support

For issues and questions:
- GitHub Issues: [https://github.com/yourusername/OsmDeviceAdapter/issues](https://github.com/yourusername/OsmDeviceAdapter/issues)
- Security Issues: Please report privately to [your-email@example.com]

## Acknowledgments

Built with:
- Go 1.22+
- PostgreSQL 12+
- Redis 6+
- Kubernetes & Helm
- Cloudflare Tunnel
- Prometheus & Grafana (optional monitoring stack)

OAuth standards:
- [RFC 8628: OAuth 2.0 Device Authorization Grant](https://datatracker.ietf.org/doc/html/rfc8628)
- [RFC 6749: OAuth 2.0 Authorization Framework](https://datatracker.ietf.org/doc/html/rfc6749)
