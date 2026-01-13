# OSM Device Adapter

A Go service that bridges the OAuth Device Flow for scoreboard devices with the Online Scout Manager (OSM) OAuth Web Flow, enabling secure access to patrol scores.

*This document is out of date. I will ask Claude to update it with all that has happened since this very early version.*

## Architecture Overview

This service implements a complete OAuth bridge that:

1. **Device OAuth Flow**: Accepts device authorization requests from scoreboards
2. **Web OAuth Flow**: Completes OAuth authorization with Online Scout Manager
3. **Token Management**: Stores and refreshes OSM access tokens
4. **Caching Layer**: Uses Redis to cache patrol scores and reduce OSM API load
5. **REST API**: Provides a simple JSON endpoint for scoreboards to fetch patrol scores

### Flow Diagram

**Out of Date**

```
┌──────────────┐          ┌─────────────────────┐          ┌─────────────┐
│  Scoreboard  │◄────────►│  OSM Device Adapter │◄────────►│     OSM     │
│   Device     │          │                     │          │  OAuth API  │
└──────────────┘          └─────────────────────┘          └─────────────┘
                                    │
                          ┌─────────┴─────────┐
                          │                   │
                    ┌─────▼─────┐      ┌─────▼─────┐
                    │ PostgreSQL│      │   Redis   │
                    │           │      │   Cache   │
                    └───────────┘      └───────────┘
```

## Device Authorization Flow

1. Scoreboard requests device authorization: `POST /device/authorize`
2. Service returns a `user_code` and `verification_uri`
3. User visits verification URL and enters the code
4. Service redirects user to OSM for OAuth authorization
5. User authorizes the application on OSM
6. OSM redirects back with authorization code
7. Service exchanges code for OSM access token
8. Scoreboard polls `POST /device/token` until authorized
9. Service creates an access token valid for this API and returns that token to the scoreboard
10. The scoreboard uses its token to access the Patrol Scores API on this server.

## API Endpoints

### Device OAuth

- `POST /device/authorize` - Initiate device authorization
- `POST /device/token` - Poll for access token
- `GET /device` - User verification page

### OAuth Web Flow

- `GET /oauth/authorize` - Start OSM OAuth flow
- `GET /oauth/callback` - OAuth callback from OSM

### Scoreboard API

- `GET /api/v1/patrols` - Get patrol scores (requires Bearer token)

### Health Checks

- `GET /health` - Basic health check
- `GET /ready` - Readiness check (includes database/Redis status)

## Documentation

- [Helm Chart Documentation](docs/HELM.md) - Detailed Helm chart usage and configuration
- [Cloudflare Tunnel Setup](docs/CLOUDFLARE_SETUP.md) - Cloudflare Tunnel integration guide

## Configuration

All configuration is provided via environment variables:

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `PORT` | HTTP server port | No | `8080` |
| `HOST` | HTTP server host | No | `0.0.0.0` |
| `EXPOSED_DOMAIN` | Public domain where service is exposed | Yes | - |
| `OSM_DOMAIN` | Online Scout Manager domain | No | `https://www.onlinescoutmanager.co.uk` |
| `OSM_CLIENT_ID` | OSM OAuth client ID | Yes | - |
| `OSM_CLIENT_SECRET` | OSM OAuth client secret | Yes | - |
| `OSM_REDIRECT_URI` | OAuth redirect URI | No | `{EXPOSED_DOMAIN}/oauth/callback` |
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection URL | No | `redis://localhost:6379` |
| `DEVICE_CODE_EXPIRY` | Device code expiry in seconds | No | `600` |
| `DEVICE_POLL_INTERVAL` | Recommended polling interval | No | `5` |
| `ALLOWED_CLIENT_IDS` | Comma-separated list of allowed OAuth client IDs | Yes | - |

## Database Schema

### PostgreSQL Tables

**device_codes**
```sql
CREATE TABLE device_codes (
    device_code VARCHAR(255) PRIMARY KEY,
    user_code VARCHAR(255) UNIQUE NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(50) DEFAULT 'pending',
    osm_access_token TEXT,
    osm_refresh_token TEXT,
    osm_token_expiry TIMESTAMP
);
```

**device_sessions**
```sql
CREATE TABLE device_sessions (
    session_id VARCHAR(255) PRIMARY KEY,
    device_code VARCHAR(255) REFERENCES device_codes(device_code),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);
```

## Local Development

### Prerequisites

- Go 1.22 or later
- PostgreSQL
- Redis
- OSM OAuth application credentials

### Setup

1. Clone the repository
2. Install dependencies:
   ```bash
   make deps
   ```

3. Set up environment variables:
   ```bash
   export EXPOSED_DOMAIN=https://localhost:8080
   export OSM_CLIENT_ID=your-client-id
   export OSM_CLIENT_SECRET=your-client-secret
   export DATABASE_URL=postgres://user:pass@localhost:5432/osmdeviceadapter
   export REDIS_URL=redis://localhost:6379
   export ALLOWED_CLIENT_IDS=scoreboard-123,scoreboard-456
   ```

4. Run the service:
   ```bash
   make run
   ```

### Building

```bash
make build
```

The binary will be created at `bin/server`.

### Testing

```bash
make test
```

## Kubernetes Deployment

### Prerequisites

- Kubernetes cluster
- Helm 3.x installed
- External PostgreSQL and Redis services
- (Optional) Cloudflare Tunnel for ingress

### Deployment with Helm (Recommended)

1. **Create a values file** (e.g., `values-production.yaml`):
   ```bash
   cp chart/values-example.yaml values-production.yaml
   ```

2. **Edit your values file** with your configuration:
   ```yaml
   image:
     repository: your-registry/osm-device-adapter
     tag: "1.0.0"

   config:
     exposedDomain: "https://osm-adapter.your-domain.com"

   database:
     url: "postgres://user:pass@postgres-host:5432/dbname"

   redis:
     url: "redis://redis-host:6379"

   osm:
     clientId: "your-osm-client-id"
     clientSecret: "your-osm-client-secret"
   ```

3. **Configure Cloudflare Tunnel** (if using):
   - See [docs/CLOUDFLARE_SETUP.md](docs/CLOUDFLARE_SETUP.md) for detailed instructions
   - Add route to your existing Cloudflare Tunnel configuration
   - Keep `ingress.enabled: false` in values file

4. **Build and Push Docker Image**:
   ```bash
   export DOCKER_REGISTRY=your-registry
   export DOCKER_TAG=v1.0.0
   make docker-push
   ```

5. **Install with Helm**:
   ```bash
   # Install to default namespace
   helm install osm-device-adapter ./chart -f values-production.yaml

   # Or install to a specific namespace
   helm install osm-device-adapter ./chart \
     --namespace osm-adapter \
     --create-namespace \
     -f values-production.yaml
   ```

6. **Verify Deployment**:
   ```bash
   # Check pods
   kubectl get pods -l app.kubernetes.io/name=osm-device-adapter

   # View logs
   kubectl logs -l app.kubernetes.io/name=osm-device-adapter

   # Check service
   kubectl get svc osm-device-adapter
   ```

### Helm Commands

```bash
# Upgrade deployment
make helm-upgrade

# Uninstall
make helm-uninstall

# Lint chart
make helm-lint

# View templated manifests (dry-run)
make helm-template

# Show default values
make helm-values
```

### Alternative: Deploy with kubectl (Legacy)

If you prefer not to use Helm, you can still deploy with kubectl:

1. Edit files in `deployments/k8s/`
2. Run `make k8s-deploy`

See the original deployment manifests in `deployments/k8s/` directory.

### Monitoring

Check health and readiness:
```bash
# For Helm deployments
kubectl get pods -l app.kubernetes.io/name=osm-device-adapter
kubectl describe pod <pod-name>

# For kubectl deployments
kubectl get pods -l app=osm-device-adapter
```

View logs:
```bash
# For Helm deployments
kubectl logs -f -l app.kubernetes.io/name=osm-device-adapter

# For kubectl deployments
kubectl logs -f deployment/osm-device-adapter
```

## Usage Example

### Scoreboard Device Flow

1. **Request Device Authorization**:
   ```bash
   curl -X POST https://your-domain.com/device/authorize \
     -H "Content-Type: application/json" \
     -d '{"client_id": "scoreboard-123"}'
   ```

   Response:
   ```json
   {
     "device_code": "abc123...",
     "user_code": "ABCD-EFGH",
     "verification_uri": "https://your-domain.com/device",
     "verification_uri_complete": "https://your-domain.com/device?user_code=ABCD-EFGH",
     "expires_in": 600,
     "interval": 5
   }
   ```

2. **User Authorization**: Navigate to `verification_uri` and enter the `user_code`

3. **Poll for Token**:
   ```bash
   curl -X POST https://your-domain.com/device/token \
     -H "Content-Type: application/json" \
     -d '{
       "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
       "device_code": "abc123...",
       "client_id": "scoreboard-123"
     }'
   ```

   Response (when authorized):
   ```json
   {
     "access_token": "xyz789...",
     "token_type": "Bearer",
     "expires_in": 3600
   }
   ```

4. **Fetch Patrol Scores**:
   ```bash
   curl https://your-domain.com/api/v1/patrols \
     -H "Authorization: Bearer xyz789..."
   ```

   Response:
   ```json
   {
     "patrols": [
       {"id": "1", "name": "Eagles", "score": 150},
       {"id": "2", "name": "Hawks", "score": 142}
     ],
     "cached_at": "2024-01-15T10:30:00Z",
     "expires_at": "2024-01-15T10:35:00Z"
   }
   ```

## Caching Strategy

- Patrol scores are cached in Redis for 5 minutes
- Cache key format: `patrol_scores:{device_code}`
- Automatic cache invalidation on expiry
- Cache hit/miss indicated in `X-Cache` header

## Security Considerations

1. **HTTPS Required**: All communication must use HTTPS
2. **Client ID Validation**: Only registered client IDs (configured via `ALLOWED_CLIENT_IDS`) can initiate device authorization
3. **Token Storage**: Access tokens are stored securely in PostgreSQL
4. **Token Refresh**: Automatic refresh before expiry
5. **Rate Limiting**: Consider adding rate limiting for device authorization requests
6. **Secrets Management**: Use Kubernetes secrets for sensitive configuration

## OSM API Integration

The service integrates with Online Scout Manager's OAuth API. You'll need to:

1. Register an OAuth application with OSM
2. Configure the redirect URI to match your domain: `https://your-domain.com/oauth/callback`
3. Obtain client ID and secret
4. Update the OSM API client (`internal/osm/client.go`) based on actual OSM API endpoints

**Note**: The OSM client implementation is a placeholder. Refer to [OSM API documentation](https://www.onlinescoutmanager.co.uk/api/) for actual endpoints and request formats.

## Troubleshooting

### Database Connection Issues
```bash
kubectl logs deployment/osm-device-adapter | grep "database"
```

### Redis Connection Issues
```bash
kubectl exec -it deployment/osm-device-adapter -- sh
# Test Redis connection manually
```

### OAuth Flow Issues
- Check that `EXPOSED_DOMAIN` matches your actual domain
- Verify OSM redirect URI configuration
- Review OSM client ID and secret

## License

[Your License Here]

## Contributing

[Your Contributing Guidelines Here]
