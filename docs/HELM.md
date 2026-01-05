# Helm Chart Documentation

This document provides detailed information about the OSM Device Adapter Helm chart.

## Chart Structure

```
chart/
├── Chart.yaml                 # Chart metadata
├── values.yaml               # Default values
├── values-example.yaml       # Example values for production
├── templates/
│   ├── _helpers.tpl         # Template helpers
│   ├── deployment.yaml      # Deployment manifest
│   ├── service.yaml         # Service manifest
│   ├── configmap.yaml       # ConfigMap manifest
│   ├── secret.yaml          # Secret manifest
│   ├── ingress.yaml         # Ingress manifest (optional)
│   ├── serviceaccount.yaml  # ServiceAccount (optional)
│   ├── hpa.yaml            # HorizontalPodAutoscaler (optional)
│   └── NOTES.txt           # Post-install notes
└── .helmignore             # Files to ignore when packaging
```

## Installation

### Basic Installation

```bash
helm install osm-device-adapter ./chart \
  --set config.exposedDomain="https://osm-adapter.example.com" \
  --set osm.clientId="your-client-id" \
  --set osm.clientSecret="your-client-secret" \
  --set database.url="postgres://..." \
  --set image.repository="your-registry/osm-device-adapter" \
  --set image.tag="1.0.0"
```

### Installation with Values File

Create a `values-production.yaml`:

```yaml
image:
  repository: ghcr.io/your-org/osm-device-adapter
  tag: "1.0.0"

config:
  exposedDomain: "https://osm-adapter.example.com"

database:
  url: "postgres://osmadapter:password@postgres.db.svc.cluster.local:5432/osmdeviceadapter"

redis:
  url: "redis://redis.cache.svc.cluster.local:6379"

osm:
  clientId: "your-osm-client-id"
  clientSecret: "your-osm-client-secret"

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi
```

Then install:

```bash
helm install osm-device-adapter ./chart -f values-production.yaml
```

## Configuration

### Required Values

| Parameter | Description | Example |
|-----------|-------------|---------|
| `config.exposedDomain` | Public domain where service is exposed | `https://osm-adapter.example.com` |
| `osm.clientId` | OSM OAuth client ID | `your-client-id` |
| `osm.clientSecret` | OSM OAuth client secret | `your-secret` |
| `database.url` | PostgreSQL connection string | `postgres://user:pass@host:5432/db` |

### Optional Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `2` |
| `image.repository` | Container image repository | `your-registry/osm-device-adapter` |
| `image.tag` | Container image tag | Chart `appVersion` |
| `config.osmDomain` | OSM API domain | `https://www.onlinescoutmanager.co.uk` |
| `config.deviceCodeExpiry` | Device code expiry (seconds) | `600` |
| `redis.url` | Redis connection URL | `redis://redis-service:6379` |
| `ingress.enabled` | Enable Ingress resource | `false` |
| `autoscaling.enabled` | Enable HPA | `false` |

### Using Existing Secrets

Instead of storing secrets in values files, you can reference existing Kubernetes secrets:

```yaml
osm:
  existingSecret: "osm-oauth-credentials"

database:
  existingSecret: "database-credentials"

redis:
  existingSecret: "redis-credentials"
```

Your existing secrets must contain the following keys:

**OSM Secret** (`osm-oauth-credentials`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: osm-oauth-credentials
stringData:
  osm-client-id: "your-client-id"
  osm-client-secret: "your-client-secret"
```

**Database Secret** (`database-credentials`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: database-credentials
stringData:
  database-url: "postgres://..."
```

**Redis Secret** (`redis-credentials`):
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: redis-credentials
stringData:
  redis-url: "redis://..."
```

## Upgrade

### Upgrade with New Values

```bash
helm upgrade osm-device-adapter ./chart -f values-production.yaml
```

### Upgrade Specific Values

```bash
helm upgrade osm-device-adapter ./chart \
  --set image.tag="1.1.0" \
  --reuse-values
```

### Rolling Update Strategy

The deployment uses a rolling update strategy by default:
- Pods are updated one at a time
- New pods must pass readiness checks before old pods are terminated
- Zero-downtime deployment

## Uninstall

```bash
helm uninstall osm-device-adapter
```

To delete all resources including PVCs:

```bash
helm uninstall osm-device-adapter --namespace your-namespace
```

## Customization

### Multiple Environments

Create separate values files for each environment:

```bash
# Development
helm install osm-dev ./chart -f values-dev.yaml --namespace dev

# Staging
helm install osm-staging ./chart -f values-staging.yaml --namespace staging

# Production
helm install osm-prod ./chart -f values-production.yaml --namespace production
```

### Custom Resource Limits

```yaml
resources:
  limits:
    cpu: 2000m
    memory: 2Gi
  requests:
    cpu: 500m
    memory: 512Mi
```

### Enable Autoscaling

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80
```

### Node Selection

```yaml
nodeSelector:
  workload-type: application

tolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "app"
    effect: "NoSchedule"

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
              - key: app.kubernetes.io/name
                operator: In
                values:
                  - osm-device-adapter
          topologyKey: kubernetes.io/hostname
```

## Cloudflare Tunnel Integration

When using Cloudflare Tunnel, keep ingress disabled:

```yaml
ingress:
  enabled: false
```

After deploying, the chart's NOTES.txt will show you the service URL to add to your Cloudflare Tunnel configuration:

```yaml
ingress:
  - hostname: osm-adapter.example.com
    service: http://osm-device-adapter.default.svc.cluster.local:80
```

## Troubleshooting

### Check Template Output

Before installing, preview the rendered templates:

```bash
helm template osm-device-adapter ./chart -f values-production.yaml
```

### Validate Chart

```bash
helm lint ./chart
```

### Debug Installation

```bash
helm install osm-device-adapter ./chart -f values-production.yaml --debug --dry-run
```

### View Current Values

```bash
helm get values osm-device-adapter
```

### View All Resources

```bash
helm get manifest osm-device-adapter
```

### Common Issues

**Missing Required Values:**
```
Error: values don't meet the specifications of the schema(s)
```
Solution: Ensure all required values are set (exposedDomain, osm.clientId, etc.)

**ImagePullBackOff:**
```
kubectl describe pod <pod-name>
```
Solution: Check image repository and tag, ensure image exists

**CrashLoopBackOff:**
```
kubectl logs <pod-name>
```
Solution: Check database connectivity, Redis connectivity, and configuration

## Advanced Usage

### Using with ArgoCD

Create an Application manifest:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: osm-device-adapter
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/m0rjc/OsmDeviceAdapter
    targetRevision: main
    path: chart
    helm:
      valueFiles:
        - values-production.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: osm-adapter
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### Using with Flux

Create a HelmRelease:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: osm-device-adapter
  namespace: osm-adapter
spec:
  interval: 5m
  chart:
    spec:
      chart: ./chart
      sourceRef:
        kind: GitRepository
        name: osm-device-adapter
  valuesFrom:
    - kind: Secret
      name: osm-adapter-values
```

## Chart Development

### Testing Changes

```bash
# Lint the chart
helm lint ./chart

# Test installation
helm install test-release ./chart --dry-run --debug

# Package the chart
helm package ./chart

# Test the packaged chart
helm install test-release ./osm-device-adapter-0.1.0.tgz --dry-run
```

### Versioning

Update version in `Chart.yaml`:

```yaml
version: 0.2.0  # Chart version
appVersion: "1.1.0"  # Application version
```
