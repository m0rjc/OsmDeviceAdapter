# OSM Device Adapter Secrets Chart

Helm chart for managing Kubernetes secrets for the OSM Device Adapter application.

## Purpose

This chart creates Kubernetes Secret resources containing sensitive credentials required by the OSM Device Adapter. By separating secret management from the application deployment, you can:

- **Initialize secrets once** during initial setup
- **Manage secrets independently** from application updates
- **Keep secrets out of git** while maintaining infrastructure-as-code
- **Use Helm's resource retention** to preserve secrets during uninstalls

## Prerequisites

- Kubernetes cluster
- Helm 3.x
- The main `osm-device-adapter` chart (deployed separately)

## Installation

### Step 1: Create Your Values File

Copy the example values file and fill in your actual secrets:

```bash
cp values-example.yaml values-production.yaml
# Edit values-production.yaml with your secrets
# NEVER commit this file to git!
```

Add `values-production.yaml` to your `.gitignore`:

```bash
echo "chart-secrets/values-production.yaml" >> .gitignore
```

### Step 2: Install the Secrets Chart

Install the chart to create the secrets in your Kubernetes cluster:

```bash
# Install to a specific namespace
helm install osm-secrets ./chart-secrets \
  -f chart-secrets/values-production.yaml \
  -n osm-adapter

# Or use --set for individual values (less secure, visible in shell history)
helm install osm-secrets ./chart-secrets \
  --set osm.clientId="your-client-id" \
  --set osm.clientSecret="your-secret" \
  --set database.url="postgres://user:pass@host:5432/db" \
  --set redis.url="redis://host:6379"
```

### Step 3: Deploy the Main Application

Configure the main chart to use the secrets created by this chart:

```bash
# For unified secret strategy (default)
helm install osm-adapter ./chart \
  --set osm.existingSecret=osm-device-adapter-secrets \
  --set database.existingSecret=osm-device-adapter-secrets \
  --set redis.existingSecret=osm-device-adapter-secrets \
  -n osm-adapter

# For separate secrets strategy
helm install osm-adapter ./chart \
  --set osm.existingSecret=osm-oauth-credentials \
  --set database.existingSecret=database-credentials \
  --set redis.existingSecret=redis-credentials \
  -n osm-adapter
```

## Configuration

### Secret Strategies

#### Unified Secret (Recommended)

Creates a single secret containing all credentials. Simpler to manage and reference.

```yaml
secretStrategy: "unified"
unifiedSecretName: "osm-device-adapter-secrets"

osm:
  clientId: "your-client-id"
  clientSecret: "your-client-secret"

database:
  url: "postgres://user:pass@host:5432/db?sslmode=require"

redis:
  url: "redis://host:6379"
```

**Main chart configuration:**

```yaml
osm:
  existingSecret: "osm-device-adapter-secrets"

database:
  existingSecret: "osm-device-adapter-secrets"

redis:
  existingSecret: "osm-device-adapter-secrets"
```

#### Separate Secrets

Creates individual secrets for each component. Useful for advanced use cases like different RBAC policies per secret.

```yaml
secretStrategy: "separate"

secretNames:
  osm: "osm-oauth-credentials"
  database: "database-credentials"
  redis: "redis-credentials"

osm:
  clientId: "your-client-id"
  clientSecret: "your-client-secret"

database:
  url: "postgres://user:pass@host:5432/db"

redis:
  url: "redis://host:6379"
```

**Main chart configuration:**

```yaml
osm:
  existingSecret: "osm-oauth-credentials"

database:
  existingSecret: "database-credentials"

redis:
  existingSecret: "redis-credentials"
```

### Configuration Values

| Parameter | Description | Required | Default |
|-----------|-------------|----------|---------|
| `namespace` | Namespace where secrets will be created | No | Release namespace |
| `secretStrategy` | Secret creation strategy: `unified` or `separate` | No | `unified` |
| `unifiedSecretName` | Name for unified secret | No | Release name |
| `secretNames.osm` | Name for OSM secret (separate mode) | No | `osm-oauth-credentials` |
| `secretNames.database` | Name for database secret (separate mode) | No | `database-credentials` |
| `secretNames.redis` | Name for Redis secret (separate mode) | No | `redis-credentials` |
| `osm.clientId` | OSM OAuth client ID | **Yes** | — |
| `osm.clientSecret` | OSM OAuth client secret | **Yes** | — |
| `database.url` | PostgreSQL connection string | **Yes** | — |
| `redis.url` | Redis connection URL | **Yes** | — |

### Secret Key Names

The following key names are created in the secrets and must match what the main application chart expects:

| Secret Key | Environment Variable | Description |
|------------|---------------------|-------------|
| `osm-client-id` | `OSM_CLIENT_ID` | OSM OAuth client identifier |
| `osm-client-secret` | `OSM_CLIENT_SECRET` | OSM OAuth client secret |
| `database-url` | `DATABASE_URL` | PostgreSQL connection string |
| `redis-url` | `REDIS_URL` | Redis connection URL |

## Updating Secrets

To update secrets after initial deployment:

```bash
# Update your values file, then upgrade
helm upgrade osm-secrets ./chart-secrets \
  -f chart-secrets/values-production.yaml \
  -n osm-adapter

# Restart application pods to pick up new secrets
kubectl rollout restart deployment/osm-device-adapter -n osm-adapter
```

## Secret Persistence

Secrets created by this chart have the `helm.sh/resource-policy: keep` annotation, which means they will **not be deleted** when you uninstall the chart. This prevents accidental data loss.

To delete secrets manually:

```bash
# For unified strategy
kubectl delete secret osm-device-adapter-secrets -n osm-adapter

# For separate strategy
kubectl delete secret osm-oauth-credentials database-credentials redis-credentials -n osm-adapter
```

## Verifying Secrets

Check that secrets were created correctly:

```bash
# List secrets
kubectl get secrets -n osm-adapter

# View secret keys (without values)
kubectl describe secret osm-device-adapter-secrets -n osm-adapter

# Decode a specific value (use with caution!)
kubectl get secret osm-device-adapter-secrets -n osm-adapter \
  -o jsonpath='{.data.osm-client-id}' | base64 -d
```

## Security Best Practices

1. **Never commit values files with real secrets** to version control
   - Use `.gitignore` to exclude `values-production.yaml`
   - Use `values-example.yaml` as a template only

2. **Restrict access** to the values files and Kubernetes secrets
   - Store values files in a secure location (e.g., encrypted vault)
   - Use Kubernetes RBAC to limit secret access

3. **Consider external secret management** for production
   - [External Secrets Operator](https://external-secrets.io/)
   - [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
   - [HashiCorp Vault](https://www.vaultproject.io/)
   - For these tools, skip this chart and create secrets directly

4. **Rotate credentials regularly**
   - Update secrets periodically
   - Restart application after rotation

5. **Use secure channels** when passing secrets
   - Avoid `--set` flags in CI/CD logs
   - Use secure secret injection in pipelines

## Troubleshooting

### Error: "osm.clientId is required"

All four values must be set: `osm.clientId`, `osm.clientSecret`, `database.url`, and `redis.url`.

**Solution:** Check your values file has all required fields populated.

### Application can't find secrets

Ensure the secret names in your main chart configuration match the names created by this chart.

**For unified strategy:**

```bash
# Check main chart is using the correct secret name
helm get values osm-adapter -n osm-adapter | grep existingSecret

# Should show:
#   osm.existingSecret: "osm-device-adapter-secrets"
#   database.existingSecret: "osm-device-adapter-secrets"
#   redis.existingSecret: "osm-device-adapter-secrets"
```

### Secrets not updating

Kubernetes doesn't automatically reload secrets in running pods.

**Solution:** Restart the application after updating secrets:

```bash
kubectl rollout restart deployment/osm-device-adapter -n osm-adapter
```

## Examples

### Example 1: Development Environment

```yaml
# values-dev.yaml
secretStrategy: "unified"
unifiedSecretName: "osm-dev-secrets"

osm:
  clientId: "dev-client-id"
  clientSecret: "dev-client-secret"

database:
  url: "postgres://osmadapter:devpass@postgresql:5432/osmadapter_dev"

redis:
  url: "redis://redis-service:6379"
```

```bash
helm install osm-secrets-dev ./chart-secrets -f values-dev.yaml -n osm-dev
```

### Example 2: Production with Separate Secrets

```yaml
# values-prod.yaml
secretStrategy: "separate"

secretNames:
  osm: "prod-osm-oauth"
  database: "prod-database"
  redis: "prod-redis"

osm:
  clientId: "prod-client-id"
  clientSecret: "prod-client-secret-from-vault"

database:
  url: "postgres://osmadapter:complexpass@db.prod.example.com:5432/osmadapter?sslmode=require"

redis:
  url: "redis://:redispass@redis.prod.example.com:6379/0"
```

```bash
helm install osm-secrets ./chart-secrets -f values-prod.yaml -n osm-production
```

## Integration with External Secret Managers

If you're using an external secret management solution, you typically **don't need this chart**. Instead:

1. Configure your secret manager to create secrets directly in Kubernetes
2. Ensure the secret keys match: `osm-client-id`, `osm-client-secret`, `database-url`, `redis-url`
3. Reference the externally-managed secrets in the main chart

Example with External Secrets Operator:

```yaml
# external-secret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: osm-device-adapter-secrets
spec:
  secretStoreRef:
    name: vault-backend
  target:
    name: osm-device-adapter-secrets
  data:
    - secretKey: osm-client-id
      remoteRef:
        key: osm/oauth
        property: client_id
    - secretKey: osm-client-secret
      remoteRef:
        key: osm/oauth
        property: client_secret
    - secretKey: database-url
      remoteRef:
        key: osm/database
        property: url
    - secretKey: redis-url
      remoteRef:
        key: osm/redis
        property: url
```

## Related Documentation

- Main application chart: `../chart/README.md`
- Security documentation: `../docs/security.md`
- Project README: `../README.md`
