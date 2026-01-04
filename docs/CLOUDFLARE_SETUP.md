# Cloudflare Tunnel Integration

Since you're using Cloudflare Tunnel running in Kubernetes in its own namespace, you'll need to configure your existing Cloudflare Tunnel to route traffic to the OSM Device Adapter service.

## Service Accessibility

The OSM Device Adapter service is deployed as a ClusterIP service:
- **Service Name**: `osm-device-adapter`
- **Namespace**: `default` (or whichever namespace you deploy to)
- **Port**: `80`
- **Internal URL**: `http://osm-device-adapter.default.svc.cluster.local:80`

## Cloudflare Tunnel Configuration

### Option 1: ConfigMap Update (Recommended)

If your Cloudflare Tunnel uses a ConfigMap for route configuration, add a new ingress rule:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cloudflared-config
  namespace: cloudflare  # Your Cloudflare namespace
data:
  config.yaml: |
    tunnel: <your-tunnel-id>
    credentials-file: /etc/cloudflared/creds/credentials.json

    ingress:
      # Add this route for OSM Device Adapter
      - hostname: osm-adapter.your-domain.com
        service: http://osm-device-adapter.default.svc.cluster.local:80

      # Your existing routes...
      - hostname: other-app.your-domain.com
        service: http://other-service.default.svc.cluster.local:80

      # Catch-all rule (must be last)
      - service: http_status:404
```

After updating the ConfigMap, restart the Cloudflare Tunnel pods:
```bash
kubectl rollout restart deployment/cloudflared -n cloudflare
```

### Option 2: Using Ingress Resource

If your Cloudflare Tunnel watches for Ingress resources, you can use the provided `ingress.yaml`:

1. Update the hostname in `deployments/k8s/ingress.yaml`
2. Ensure the `ingressClassName` matches your Cloudflare Tunnel ingress class
3. Apply the ingress: `kubectl apply -f deployments/k8s/ingress.yaml`

### Option 3: Cloudflare Dashboard

Configure the route via the Cloudflare Zero Trust dashboard:

1. Navigate to **Zero Trust** > **Access** > **Tunnels**
2. Select your tunnel
3. Go to **Public Hostname** tab
4. Add a new public hostname:
   - **Subdomain**: `osm-adapter`
   - **Domain**: `your-domain.com`
   - **Type**: `HTTP`
   - **URL**: `osm-device-adapter.default.svc.cluster.local:80`

## DNS Configuration

Ensure DNS is configured for your chosen subdomain:
- If using Cloudflare-managed DNS, the tunnel will automatically create DNS records
- Otherwise, create a CNAME record pointing to your tunnel

## Required Environment Variables

Don't forget to update the ConfigMap with your actual domain:

```yaml
# deployments/k8s/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: osm-device-adapter-config
data:
  exposed-domain: "https://osm-adapter.your-domain.com"  # Update this!
  osm-domain: "https://www.onlinescoutmanager.co.uk"
  device-code-expiry: "600"
  device-poll-interval: "5"
```

## OSM OAuth Configuration

When registering your OAuth application with Online Scout Manager, use:
- **Redirect URI**: `https://osm-adapter.your-domain.com/oauth/callback`

Make sure this matches the `EXPOSED_DOMAIN` environment variable.

## Testing the Integration

After configuring Cloudflare Tunnel:

1. Test external access:
   ```bash
   curl https://osm-adapter.your-domain.com/health
   ```

   Expected response:
   ```json
   {"status":"ok"}
   ```

2. Test readiness:
   ```bash
   curl https://osm-adapter.your-domain.com/ready
   ```

   Should show database and Redis connection status.

## Troubleshooting

### Service Not Reachable

If the Cloudflare Tunnel can't reach the service:

1. **Verify service is running**:
   ```bash
   kubectl get svc osm-device-adapter
   kubectl get pods -l app=osm-device-adapter
   ```

2. **Test internal connectivity** from Cloudflare namespace:
   ```bash
   kubectl run test-pod --image=curlimages/curl --rm -it -n cloudflare -- \
     curl http://osm-device-adapter.default.svc.cluster.local/health
   ```

3. **Check NetworkPolicies**: Ensure there are no NetworkPolicies blocking traffic between namespaces

### HTTPS/TLS Issues

Cloudflare Tunnel handles TLS termination, so:
- The service communicates via HTTP internally (port 80)
- Cloudflare provides HTTPS externally
- The `EXPOSED_DOMAIN` environment variable should use `https://`

### OAuth Callback Issues

If OAuth callbacks fail:
1. Verify `EXPOSED_DOMAIN` exactly matches your Cloudflare hostname
2. Check OSM OAuth app configuration has the correct redirect URI
3. Review logs: `kubectl logs -l app=osm-device-adapter`

## Security Considerations

When using Cloudflare Tunnel:

1. **Cloudflare Access**: Consider adding Cloudflare Access policies for admin endpoints
2. **Network Policies**: Optionally restrict which namespaces can access the service
3. **TLS**: Cloudflare handles TLS, but you can enable end-to-end encryption if needed

## Example: Full Cloudflare Tunnel Config

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cloudflared-config
  namespace: cloudflare
data:
  config.yaml: |
    tunnel: abc123-your-tunnel-id
    credentials-file: /etc/cloudflared/creds/credentials.json

    # Metrics endpoint
    metrics: 0.0.0.0:2000

    ingress:
      # OSM Device Adapter
      - hostname: osm-adapter.your-domain.com
        service: http://osm-device-adapter.default.svc.cluster.local:80
        originRequest:
          connectTimeout: 30s
          noTLSVerify: false

      # Other applications
      - hostname: "*.your-domain.com"
        service: http://default-backend.default.svc.cluster.local:80

      # Catch-all
      - service: http_status:404
```
