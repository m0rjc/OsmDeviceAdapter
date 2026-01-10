# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

OSM Device Adapter is a Go service that bridges OAuth Device Flow (RFC 8628) for IoT scoreboard devices with the Online Scout Manager (OSM) OAuth Web Flow. It provides secure, token-isolated API access to patrol scores without exposing OSM credentials to devices.

## Common Commands

### Build & Run
```bash
make build          # Build binary to bin/server
make run            # Run locally (requires env vars)
make test           # Run all tests
go test -v ./...    # Run tests with verbose output
```

### Development
```bash
make deps           # Install/update Go dependencies
make fmt            # Format code
make lint           # Lint code (requires golangci-lint)
```

### Docker & Kubernetes
```bash
# Docker
make docker-build   # Build Docker image
make docker-push    # Build and push to registry

# Helm
make helm-install   # Install chart to K8s
make helm-upgrade   # Upgrade existing deployment
make helm-lint      # Validate Helm chart
make helm-template  # Preview rendered manifests
make helm-values    # Show default values

# Monitoring
make monitoring-deploy  # Deploy Prometheus stack
```

### Running Tests
```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./internal/osm

# Run a single test
go test -run TestSpecificFunction ./internal/osm

# Run with coverage
go test -cover ./...
```

## Code Architecture

### Two-Tier OAuth Bridge

The service implements a security boundary between untrusted IoT devices and the OSM API:

1. **Device Flow (RFC 8628)**: Public client OAuth for input-constrained devices
   - Endpoints: `/device/authorize`, `/device/token`, `/device`
   - No client authentication (devices cannot keep secrets)
   - Security via user authorization on separate trusted device

2. **Web Flow (RFC 6749)**: Confidential client OAuth with OSM
   - Endpoints: `/oauth/authorize`, `/oauth/callback`, `/device/select-section`
   - Client secret authentication
   - Tokens stored server-side, never exposed to devices

3. **Device Access Tokens**: Security isolation layer
   - Devices receive a server-generated access token (not the OSM token)
   - OSM access/refresh tokens remain server-side only
   - Service acts as a proxy, making OSM API calls on behalf of devices
   - See `docs/security.md` for detailed architecture

### Key Components

**`cmd/server/main.go`** - Application entry point
- Initializes two HTTP servers:
  - Main server (port 8080): Device/OAuth/API endpoints
  - Metrics server (port 9090): Health checks and Prometheus metrics (internal only)
- Graceful shutdown handling
- Database migrations run automatically via GORM AutoMigrate

**`internal/deviceauth/service.go`** - Device authentication service
- `AuthenticateRequest()`: Validates device access tokens and refreshes OSM tokens
- Implements automatic OSM token refresh when near expiry (5-minute threshold)
- Returns `types.User` interface for authenticated requests

**`internal/osm/`** - OSM API client
- `client.go`: HTTP client with rate limiting and latency recording
- `request.go`: Low-level request handling with functional options pattern
  - `WithApiAction()`: Standard OSM API calls to `/api.php?action=...`
  - `WithUser()`: User-specific authenticated requests
  - `WithSensitive()`: Marks request for secure logging (redacts response bodies)
- `patrol_scores.go`: Fetches patrol scores from OSM
- `prometheus.go`: Rate limit store and latency recorder using Redis + Prometheus
- Handles OSM rate limiting via `X-RateLimit-*` headers and `X-Blocked` header

**`internal/db/`** - Database layer
- `models.go`: GORM models for `DeviceCode` and `DeviceSession`
- `device_code_store.go`: CRUD operations for device codes
- `device_session_store.go`: Web session management for OAuth flow
- `redis.go`: Redis client with configurable key prefix
- `connections.go`: Wrapper providing both PostgreSQL and Redis connections

**`internal/handlers/`** - HTTP handlers
- `device_oauth.go`: Device flow endpoints (`/device/authorize`, `/device/token`)
- `oauth_web.go`: Web OAuth flow (`/oauth/authorize`, `/oauth/callback`)
- `api.go`: Scoreboard API (`/api/v1/patrols`)
- `health.go`: Health and readiness checks
- `dependencies.go`: Shared handler dependencies struct

**`internal/server/server.go`** - HTTP server setup
- `NewServer()`: Main application server with logging middleware
- `NewMetricsServer()`: Internal-only metrics and health server
- Middleware records request duration and status code metrics

### Database Schema

**`device_codes` table** - Tracks OAuth device authorization lifecycle
- `device_code`: Primary key, unique identifier for device authorization
- `user_code`: Human-readable code (e.g., "ABCD-EFGH")
- `status`: "pending" → "awaiting_section" → "authorized" (or "denied")
- `device_access_token`: Token returned to device (server-generated, isolates OSM token)
- `osm_access_token`, `osm_refresh_token`, `osm_token_expiry`: OSM credentials (server-side only)
- `section_id`, `osm_user_id`: User context after authorization

**`device_sessions` table** - Temporary web sessions during OAuth flow
- `session_id`: Also used as OAuth state parameter for CSRF protection
- `device_code`: Foreign key linking to device authorization
- Expires after 15 minutes

### Configuration

All configuration via environment variables (see `internal/config/config.go`):

**Critical Secrets** (never commit):
- `OSM_CLIENT_SECRET`: OSM OAuth client secret
- `DATABASE_URL`: PostgreSQL connection string
- `REDIS_URL`: Redis connection string (default: `redis://localhost:6379`)

**Required Config**:
- `OSM_CLIENT_ID`: OSM OAuth client ID
- `EXPOSED_DOMAIN`: Public domain (e.g., `https://osm-adapter.example.com`)
- `ALLOWED_CLIENT_IDS`: Comma-separated list of allowed device client IDs

**Optional**:
- `PORT`: HTTP port (default: 8080)
- `HOST`: Bind address (default: 0.0.0.0)
- `OSM_DOMAIN`: OSM base URL (default: https://www.onlinescoutmanager.co.uk)
- `DEVICE_CODE_EXPIRY`: Device code TTL in seconds (default: 600)
- `DEVICE_POLL_INTERVAL`: Recommended polling interval in seconds (default: 5)
- `REDIS_KEY_PREFIX`: Redis key namespace (default: "osm_device_adapter:")

### Observability

**Structured Logging** (`internal/logging/logger.go`):
- Uses `log/slog` for structured JSON logs
- Log events follow pattern: `component.event` (e.g., `osm.api.request`, `http.request`)

**Prometheus Metrics** (`internal/metrics/metrics.go`):
- `http_request_duration_seconds`: HTTP request latency histogram
- `http_requests_total`: HTTP request counter by method/path/status
- `osm_api_latency_seconds`: OSM API call latency
- `osm_rate_limit_remaining`: Per-user rate limit tracking
- Exposed on metrics server at `:9090/metrics`

**Health Checks**:
- `GET /health`: Basic liveness check (always returns 200)
- `GET /ready`: Readiness check (verifies database and Redis connectivity)

### Security Model

**Token Isolation Architecture**:
- Device access tokens are independent from OSM tokens
- OSM credentials (`osm_access_token`, `osm_refresh_token`) never leave the server
- Service acts as a proxy, making OSM API calls using server-side tokens
- See `docs/security.md` for comprehensive security documentation

**Client ID Validation**:
- `ALLOWED_CLIENT_IDS` whitelist controls which applications can use the service
- Device client IDs are public (extractable from device firmware)
- Whitelist provides access control and DoS mitigation, not authentication

**Rate Limiting**:
- OSM API rate limits tracked via `X-RateLimit-*` headers
- Per-user temporary blocks stored in Redis
- Service-wide blocks detected via `X-Blocked` header
- Cloudflare rate limiting on ingress (see `docs/CLOUDFLARE_SETUP.md`)

## Development Guidelines

### Error Handling
- Return clear error messages to users (devices receive OAuth-compliant error responses)
- Use structured logging with `slog` for all errors
- Redact sensitive data in logs (tokens, secrets) - see `request.go:306` for pattern

### Testing Patterns
- Unit tests should not require external dependencies (mock databases/Redis)
- See `internal/osm/request_test.go` for test structure examples
- Use table-driven tests for multiple scenarios

### Adding New OSM API Endpoints
1. Add method to `internal/osm/client.go` or create new file in `internal/osm/`
2. Use `Request()` with functional options:
   ```go
   var result MyType
   _, err := c.Request(ctx, "GET", &result,
       WithApiAction("myAction"),
       WithUser(user),
   )
   ```
3. For sensitive endpoints (tokens, secrets), add `WithSensitive()` option

### Database Migrations
- Migrations run automatically via GORM AutoMigrate at startup (see `cmd/server/main.go:38`)
- Add new fields to models in `internal/db/models.go`
- GORM will create columns/indexes automatically

### Prometheus Metrics
- Import `_ "github.com/m0rjc/OsmDeviceAdapter/internal/metrics"` to initialize
- Define new metrics in `internal/metrics/metrics.go`
- Metrics automatically exposed via `/metrics` endpoint on port 9090

## Documentation References

- `README.md`: User-facing documentation, deployment instructions
- `docs/security.md`: Security architecture, threat model, improvement roadmap
- `docs/HELM.md`: Helm chart usage and configuration
- `docs/CLOUDFLARE_SETUP.md`: Cloudflare Tunnel integration
- `docs/OBSERVABILITY_IMPLEMENTATION.md`: Monitoring setup details
- `docs/research/OSM-OAuth-Doc.md`: OSM API documentation research

## Deployment

### Local Development
Requires PostgreSQL and Redis running locally. Set all required environment variables, then:
```bash
make run
```

### Kubernetes (Helm)
1. Build and push Docker image: `make docker-push`
2. Create values file from `chart/values-example.yaml`
3. Install: `helm install osm-device-adapter ./chart -f values-production.yaml`
4. Monitor: `kubectl logs -f -l app.kubernetes.io/name=osm-device-adapter`

### Kubernetes (kubectl - legacy)
1. Edit `deployments/k8s/` manifests
2. Deploy: `make k8s-deploy`

## Important Notes

- The service runs two HTTP servers: main (8080) and metrics (9090)
- Metrics server should not be exposed to the public internet
- OSM API client ID/secret must match OAuth application registered with OSM
- Device client IDs are public once deployed (designed for public clients)
- HTTPS is required in production (enforced by Cloudflare Tunnel in current setup)
- Database schema evolves automatically via GORM AutoMigrate
