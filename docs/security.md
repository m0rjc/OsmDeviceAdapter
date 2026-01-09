# Security Architecture

## Overview

OsmDeviceAdapter implements a two-tier OAuth 2.0 architecture that bridges IoT devices (public clients) with the Online Scout Manager API (confidential client integration).

## Architecture Flow

```
IoT Device → [Device Flow] → OsmDeviceAdapter → [Authorization Code Flow] → OSM API
  (Public)                    (Bridge Service)                              (Resource Server)
```

## OAuth Flow Separation

### 1. Device Authorization Flow (RFC 8628)

**Endpoints:**
- `POST /device/authorize` - Request device and user codes
- `POST /device/token` - Poll for token issuance

**Purpose:** Allow IoT devices and input-constrained devices to authenticate users

**Client Type:** Public Client

**Security Model:**
- No client authentication (devices cannot keep secrets)
- Security comes from user authorization on a separate trusted device
- Time-limited device codes
- One-time use codes

### 2. OSM Authorization Code Flow (RFC 6749)

**Endpoints:**
- `GET /oauth/authorize` - Initiate OSM authorization
- `GET /oauth/callback` - Receive authorization code from OSM

**Purpose:** OsmDeviceAdapter authenticates with OSM as a confidential client

**Client Type:** Confidential Client

**Security Model:**
- Client authenticated with client secret
- Authorization code exchanged server-side
- Tokens never exposed to end devices

## Credential Secrecy Levels

### Public (Not Secret)

These identifiers are embedded in device applications and should be considered public once deployed:

| Credential | Location | Purpose | Security Impact |
|------------|----------|---------|-----------------|
| Device Client ID | Device application code | Identifies which application is requesting access | Used for access control and metrics only. Can be extracted from devices. |

**Implementation:** See `internal/handlers/device_oauth.go:70-81`

The device client ID is validated against a whitelist (`Config.AllowedClientIDs`) to control which applications can use the service, but this is access control, not authentication security.
It helps to prevent denial-of-service attacks by which a client without knowledge of a valid client ID can repeatedly request access.

### Confidential (Secret)

These credentials must be protected and stored only on the server:

| Credential | Location | Purpose | Security Impact |
|------------|----------|---------|-----------------|
| `OSMClientSecret` | Server environment variable | Authenticates OsmDeviceAdapter to OSM | **CRITICAL** - If compromised, attackers can impersonate the service to OSM |
| `SessionSecret` | Server environment variable | Signs session cookies | If compromised, attackers can forge user sessions |
| Device Code | Database, returned once | Authorizes device to poll for tokens | Short-lived (configurable expiry), one-time use |
| User Code | Database, displayed to user | Links device authorization to user action | Short-lived, one-time use |
| OSM Access Token | Database | Accesses OSM API on behalf of user | **CRITICAL** - Provides full API access with user's permissions |
| OSM Refresh Token | Database | Obtains new access tokens | **CRITICAL** - Long-lived credential for token renewal |

**Implementation:** See `internal/config/config.go:21,44,61`

### Ephemeral Secrets

These are generated per-session and have limited lifetime:

| Credential | Lifetime | Purpose |
|------------|----------|---------|
| OAuth State Parameter | Single authorization flow | CSRF protection for OAuth callback |
| Device Code | Configurable (e.g., 15 minutes) | Binds device to authorization session |
| User Code | Configurable (e.g., 15 minutes) | User-friendly code for authorization |

## Security Boundaries

### Boundary 1a: Device ↔ OsmDeviceAdapter - OAuth

**Trust Model:** Zero trust - devices are untrusted public clients

**Protection Mechanisms:**
- Client ID whitelist (access control)
- Rate limiting (TODO: verify implementation). Use a mixture of Cloudflare and Redis rate limiting.
- Device code expiry
- User authorization required
- No client authentication (by design of device flow)

**Threat Considerations:**
- Attacker can extract client ID from device firmware → Device must be removed from the whitelist.
- Attacker can intercept device codes → Mitigated by short expiry and user authorization requirement
- Attacker can poll with fake device codes → Mitigated by rate limiting

This is, at least at first, a personal project with only one device in the wild. Attack of the device would require 
physical access to the Scout Hut in which it is installed. If I allow other scout troops to use the service, then they
will gain access to the device code. I can allocate each troop their own device code and switch to database storage
to manage the whitelist.

### Boundary 1b: Device ↔ OsmDeviceAdapter - API

The API is deliberately highly constrained. The only information exposed is a list of patrol names and their scores.
For example:

```json
[
  {"patrol":"Lions","score":100},
  {"patrol":"Tigers","score":80},
  {"patrol":"Bears","score":60},
  {"patrol":"Oh My!","score":0}
]
```

This does not expose any sensitive information or allow access to personal data from OSM.

### Boundary 2: OsmDeviceAdapter ↔ OSM API

**Trust Model:** Confidential client - server is trusted to keep secrets

**Protection Mechanisms:**
- Client secret authentication
- HTTPS transport (enforced by OSM)
- Authorization code flow (codes are single-use)
- Token storage in database
- Limited (but not limited enough) OAuth scope

OSM's most restrictive scope allows read-only access to all user data. This is personal data and so a risk should
the server or token be compromised.

The development server relies on the limited security of a private network, with the only ingress being the Cloudflare
Tunnel. Nothing else is routable from the Internet.

Future work could harden the Kubernetes setup, for example, by configuring Network Policies and ISTIO. The database
containing keys remains unencrypted. It lives outside the cluster on the same machine as the cluster. The database
is not exposed to the Internet and uses PostgreSQL's Host-Based Access Control to limit access to the cluster and
my trusted VLAN. Connection is not encrypted, though authentication uses cryptographic challenges. 

A production system, should this ever grow to wide use, would need to implement additional security measures.
Ultimately, I grant access to this application to read user data from my Scout Section through OAuth Web Flow.
That is a conscious choice by anybody who uses this system.

**Threat Considerations:**
- TODO: Server compromise exposes OSM client secret → I need to manually review the code.
- Database compromise exposes user tokens → Currently protected by limited access to the server.
- Token leakage in logs → Tokens are NOT logged (see `device_oauth.go:139` - only truncated hash) - Code Review TODO
- Secrets leakage through GitHub -> TODO:
  - Consider Git Secrets tool to police commits.
  - Switch to using Kubernets Secrets over a local values.yaml.
    These secrets are manually entered into the cluster and never appear in code.

### Boundary 3: User ↔ OsmDeviceAdapter Web Interface

**Trust Model:** Authenticated sessions via OSM OAuth

**Protection Mechanisms:**
- OAuth state parameter (CSRF protection)
- Session cookies (signed with SessionSecret)
- HTTPS required for production
- User must authenticate with OSM

**Threat Considerations:**
- Session hijacking → Use secure, httpOnly, sameSite cookies
- CSRF attacks → OAuth state parameter validation
- XSS attacks → Input sanitization, Content-Security-Policy headers

These sessions are short-lived, typically minutes, long enough for the user to complete the authorization flow.
TODO: Check that the system invalidates the session once authentication flow and section selection is complete.

A future story may allow the user to configure their device. This will require proper security design at that point.

## Configuration Security

### Required Environment Variables

```bash
# Confidential - NEVER commit to version control
OSM_CLIENT_SECRET=<secret>
SESSION_SECRET=<secret>

# Public/Semi-Public - Can be in config
OSM_CLIENT_ID=<id>
ALLOWED_CLIENT_IDS=device-app-v1,device-app-v2

# Infrastructure
DATABASE_URL=<connection-string>  # Confidential
REDIS_URL=<connection-string>     # Confidential
```

### Configuration Validation

The application validates that required secrets are present at startup (see `internal/config/config.go:61`).

## Logging and Monitoring

### What Gets Logged

- Client IDs (for metrics and access control auditing)
- User codes (for debugging authorization flows)
- Device code hashes (truncated - first 8 characters only)
- Authorization events (success/denied/expired)

### What Must NOT Be Logged

- Full device codes (when we move to a device code database, we can log the name of the device instead.)
- OSM access tokens
- OSM refresh tokens
- Client secrets
- Session secrets

**Implementation:** See `device_oauth.go:139` for example of safe logging with truncation.

## Security Improvement Plan

### Phase 1: Critical Security Review (Do First)

1. **Manual code review for credential exposure**
   - Review entire codebase for OSM client secret handling
   - Verify no secrets are logged or exposed in error messages
   - Audit `device_oauth.go:139` and similar locations for token logging practices
   - Ensure truncated hashes only, never full tokens

2. **Verify rate limiting implementation**
   - Confirm Cloudflare rate limiting is active and properly configured
   - Validate Redis-based rate limiting on `/device/authorize` and `/device/token`
   - Test rate limit thresholds are appropriate
   - Document current configuration

3. **Session lifecycle validation**
   - Verify sessions are invalidated after OAuth flow completion
   - Check session cleanup after section selection is complete
   - Ensure no orphaned sessions persist unnecessarily

### Phase 2: Secrets Management Hardening

1. **Implement Git secrets protection**
   - Install and configure git-secrets or similar tool
   - Add pre-commit hooks to prevent credential commits
   - Scan existing commit history for accidentally committed secrets

2. **Migrate to Kubernetes Secrets**
   - Move from local `values.yaml` to Kubernetes Secrets for:
     - `OSM_CLIENT_SECRET`
     - `SESSION_SECRET`
     - `DATABASE_URL`
     - `REDIS_URL`
   - Document secret creation process (manual entry, never in code)
   - Update deployment manifests to reference secrets
   - Remove secrets from version control completely (they should not currently be in version control)

3. **Configure secure session cookies**
   - Set `httpOnly: true` flag (prevent XSS access)
   - Set `secure: true` flag (HTTPS only)
   - Set `sameSite: Strict` or `Lax` (CSRF protection)
   - Verify implementation in session middleware

### Phase 3: Production Readiness

1. **HTTPS enforcement**
   - Configure HTTPS-only mode in production environment
   - Add HTTP to HTTPS redirect
   - Set HSTS headers (Strict-Transport-Security)
   - Verify Cloudflare Tunnel is using HTTPS

2. **Database security hardening**
   - Document current PostgreSQL HBA configuration
   - Review and tighten access controls if needed
   - Consider encrypted connections between cluster and database
   - Evaluate encryption-at-rest for database containing tokens

3. **Monitoring and alerting**
   - Implement alerts for repeated denied client IDs (potential abuse)
   - Monitor failed authorization attempts
   - Track token issuance patterns
   - Set up dashboards for security metrics

### Phase 4: Operational Security

1. **Secret rotation procedures**
   - Document process for rotating `OSMClientSecret`
   - Document process for rotating `SessionSecret`
   - Implement periodic rotation schedule (quarterly recommended)
   - Test rotation process in staging environment

2. **Token lifecycle management**
   - Implement cleanup job for expired device codes
   - Review and configure appropriate device code TTL
   - Verify user codes are properly cleaned up
   - Monitor for token leakage or abnormal patterns

3. **Database access controls**
   - Restrict direct access to `device_codes` table
   - Use least-privilege database roles
   - Audit who has access to production database
   - Consider database encryption for token storage

### Phase 5: Future Enhancements

1. **Advanced security features**
   - Device fingerprinting: Additional device identification beyond client ID
   - User consent tracking: Log which scopes users authorized
   - Token revocation: Implement endpoint for users to revoke device access (low priority if provided by OSM)
   - Audit logging: Track all token issuances and API calls per device (already partially implemented)

2. **Kubernetes hardening** (if scaling to production)
   - Configure Network Policies to limit pod communication
   - Consider service mesh (ISTIO) for mTLS between services
   - Implement pod security policies/standards
   - Regular security scanning of container images

3. **Scalability considerations**
   - Move client ID whitelist to database (currently in config)
   - Implement per-leader device code allocation
   - Design multi-tenancy model if expanding beyond personal use

## References

- [RFC 8628: OAuth 2.0 Device Authorization Grant](https://datatracker.ietf.org/doc/html/rfc8628)
- [RFC 6749: OAuth 2.0 Authorization Framework](https://datatracker.ietf.org/doc/html/rfc6749)
- [OSM OAuth Documentation](./research/OSM-OAuth-Doc.md)
