# Patrol Scores Fetcher - Implementation Plan

## Overview

This document outlines the implementation plan for the patrol scores fetcher service. The work is organized into phases to ensure systematic delivery of functionality with proper testing and monitoring at each stage.

## Implementation Phases

### Phase 1: Database Foundation & Term Management

**Goal:** Enable term discovery and tracking for devices.

**Tasks:**
1. Create database migration for device_codes table extensions
   - Add columns: user_id, term_id, term_checked_at, term_end_date
   - Add indexes on user_id and term_end_date
   - Test migration rollback capability

2. Implement term discovery service
   - Create service to fetch OAuth resource endpoint
   - Parse user_id and section terms from response
   - Find active term based on current date
   - Store term information in device_codes table

3. Add term refresh logic
   - Implement 24-hour term check expiry
   - Add term cache invalidation on errors
   - Handle "not in term" scenario (HTTP 409)

**Testing:**
- Unit tests for term discovery logic
- Integration tests with mocked OSM API
- Test term expiration scenarios

**Deliverable:** Devices can discover and track their active term.

---

### Phase 2: Basic Patrol Fetching

**Goal:** Fetch and return patrol scores from OSM.

**Tasks:**
1. Implement patrol fetching from OSM API
   - Create endpoint handler: GET /api/v1/patrols
   - Call OSM patrols API with section_id and term_id
   - Parse JSON response with patrol IDs as keys

2. Add patrol data filtering
   - Filter out negative patrol IDs
   - Filter out "unallocated" key
   - Filter out patrols with empty members arrays
   - Convert points string to integer
   - Sort patrols by name

3. Implement basic response format
   - Return patrol array: {id, name, score}
   - Add basic error handling

**Testing:**
- Unit tests for patrol filtering logic
- Integration tests with captured OSM payloads
- Test with various patrol configurations

**Deliverable:** Basic patrol scores endpoint working without caching.

---

### Phase 3: Rate Limiting Infrastructure

**Goal:** Monitor and respond to OSM rate limits.

**Tasks:**
1. Extend Redis rate limit storage
   - Implement `osm_ratelimit:{user_id}` cache keys
   - Store: limit, remaining, reset_at, last_updated
   - Set TTL based on X-RateLimit-Reset header
   - ✅ Already have Prometheus metrics (internal/metrics/metrics.go)

2. Implement rate limit header parsing
   - ✅ Already implemented in internal/osm/client.go:274-352
   - Verify X-RateLimit-Limit extraction
   - Verify X-RateLimit-Remaining extraction
   - Verify X-RateLimit-Reset extraction

3. Add rate limit state determination
   - Calculate state: NONE (>200), DEGRADED (100-200), CRITICAL (<20)
   - Log warnings at appropriate thresholds
   - Update Prometheus metrics after each OSM call

**Testing:**
- Unit tests for rate limit parsing
- Integration tests simulating varying rate limit states
- Test Redis storage and retrieval

**Deliverable:** System monitors and logs rate limit status.

---

### Phase 4: Intelligent Caching Strategy

**Goal:** Implement two-tier caching with dynamic TTL.

**Tasks:**
1. Implement dynamic cache TTL calculation
   - Create calculateCacheTTL function
   - Base TTL: 5 minutes (normal operation)
   - Extended TTL: 10/15/30 minutes (based on rate limit state)

2. Implement two-tier cache storage
   - Cache key: `patrol_scores:{device_code}`
   - Store: patrols, cached_at, valid_until timestamps
   - Redis TTL: 8 days (CACHE_FALLBACK_TTL)
   - Valid until: current time + dynamic TTL (5-30 minutes)

3. Add cache hit/miss logic
   - Check cache on every request
   - Validate cache freshness using valid_until
   - Return cached data if still valid
   - Fetch fresh data if expired

4. Add cache metadata to responses
   - Include: from_cache, cached_at, cache_expires_at
   - Add rate_limit_state to response

**Testing:**
- Unit tests for TTL calculation
- Integration tests for cache behavior
- Test stale data identification
- Load tests for cache effectiveness

**Deliverable:** Intelligent caching reduces OSM API calls significantly.

---

### Phase 5: Blocking Detection & Handling

**Goal:** Handle both temporary (HTTP 429) and permanent (X-Blocked) blocking.

**Tasks:**
1. Implement HTTP 429 handling
   - Parse Retry-After header
   - Store temporary block: `osm_blocked:{user_id}`
   - Set Redis TTL to retry_after duration
   - Serve stale cache if available
   - Return 429 with Retry-After if no cache

2. Implement X-Blocked header detection
   - ✅ Already detected in internal/osm/client.go:100-116
   - Store global block: `osm_service_blocked` (no TTL)
   - Update Prometheus metric: osm_service_blocked = 1
   - Stop ALL OSM requests immediately
   - Return HTTP 503 to all devices

3. Add pre-request blocking checks
   - Check global service block first
   - Check per-user temporary block
   - Short-circuit requests if blocked
   - Serve stale cache during blocking

4. Document recovery procedures
   - Manual investigation steps
   - Redis key removal process
   - OSM support contact procedures

**Testing:**
- Integration tests for HTTP 429 scenarios
- Integration tests for X-Blocked scenarios
- Test stale cache serving during blocks
- Test global vs per-user blocking

**Deliverable:** System handles all blocking scenarios gracefully.

---

### Phase 6: Observability & Monitoring

**Goal:** Comprehensive logging, metrics, and alerting.

**Tasks:**
1. Complete structured logging implementation
   - ✅ Already using slog (internal/osm/client.go)
   - Add severity levels to all log entries
   - Ensure device_code_hash truncation (first 8 chars only)
   - Log rate limit state transitions
   - Add cache hit/miss logging

2. Add additional Prometheus metrics
   - patrol_fetch_success_total counter
   - patrol_fetch_errors_total counter by error_type
   - patrol_cache_hit_total / patrol_cache_miss_total counters
   - patrol_cache_age_seconds histogram
   - patrol_rate_limit_state gauge (0=NONE, 1=DEGRADED, 2=BLOCKED)

3. Configure alerting rules
   - Critical: osm_service_blocked == 1
   - Critical: osm_rate_limit_remaining < 10
   - Warning: osm_rate_limit_remaining < 100
   - Warning: patrol_cache_miss_rate > 0.5
   - Info: X-Deprecated header detected

4. Create monitoring dashboard
   - Rate limit status per user
   - Cache effectiveness metrics
   - Error rates and types
   - API latency trends

**Testing:**
- Verify all metrics are exposed
- Test alerting rules in staging
- Validate log format and content

**Deliverable:** Production-ready monitoring and alerting.

---

### Phase 7: Security Hardening & Documentation

**Goal:** Ensure security best practices and complete documentation.

**Tasks:**
1. Security audit
   - Verify no tokens in logs (following docs/security.md)
   - Validate section_id/term_id before OSM calls
   - Verify OSM response structure validation
   - Review user isolation (per-user rate limiting)
   - Test block enforcement

2. Add X-Deprecated header handling
   - Parse deprecation date
   - Log structured warning
   - Store in monitoring for alerting

3. Configuration management
   - Add CACHE_FALLBACK_TTL environment variable
   - Add RATE_LIMIT_* threshold environment variables
   - Document all configuration options

4. Complete documentation
   - Update API documentation with new endpoint
   - Document error responses
   - Document monitoring and alerting
   - Update README with patrol scores feature
   - Create operator runbook for blocking scenarios

**Testing:**
- Security testing (no credential leakage)
- Configuration validation
- Documentation review

**Deliverable:** Production-ready, secure, and well-documented service.

---

## Dependencies & Prerequisites

**External Dependencies:**
- OSM API availability
- Redis instance
- PostgreSQL database
- Prometheus/Grafana stack

**Internal Dependencies:**
- OAuth flow must be complete (device authorization)
- Section selection must be implemented
- Existing metrics infrastructure (✅ in place)

**Already Completed:**
- ✅ Rate limit header parsing (internal/osm/client.go:274-352)
- ✅ X-Blocked header detection (internal/osm/client.go:100-116)
- ✅ Prometheus metrics setup (internal/metrics/metrics.go)
- ✅ Structured logging with slog (internal/osm/client.go)
- ✅ Security guidelines documented (docs/security.md)

---

## Risk Assessment

**High Risk:**
- Global service blocking (X-Blocked header) - affects all users
  - Mitigation: Aggressive caching, monitoring, manual recovery procedures

**Medium Risk:**
- Rate limit exhaustion - temporary service degradation
  - Mitigation: Dynamic TTL extension, per-user isolation, stale cache serving

**Low Risk:**
- Term boundary transitions - brief unavailability during term gaps
  - Mitigation: Clear error messaging (HTTP 409)

---

## Testing Strategy

**Unit Tests:**
- Term discovery logic
- Patrol filtering (special patrol removal)
- Cache TTL calculation
- Rate limit state determination
- Response formatting

**Integration Tests:**
- Full patrol fetch flow with mocked OSM
- Cache hit/miss scenarios
- Rate limiting state transitions
- Blocking scenarios (429 and X-Blocked)
- Term expiration handling
- Stale cache serving

**Load Tests:**
- Concurrent device requests
- Cache effectiveness under load
- Redis performance
- Rate limiting behavior at scale

---

## Rollout Plan

**Stage 1: Internal Testing**
- Deploy to development environment
- Test with single device/section
- Verify all metrics and logs

**Stage 2: Limited Beta**
- Deploy to staging with 2-3 real devices
- Monitor rate limits closely
- Validate caching effectiveness
- Test during actual scout meetings

**Stage 3: Production Rollout**
- Enable for all authorized devices
- Monitor for 24 hours with enhanced logging
- Validate rate limit consumption patterns
- Adjust cache TTLs if needed

**Rollback Plan:**
- Return HTTP 503 from patrol endpoint
- Keep OAuth and device authorization working
- Investigate issues offline
- Re-enable after fixes

---

## Success Metrics

**Performance:**
- API response time < 200ms (cache hit)
- API response time < 2s (cache miss)
- Cache hit rate > 80%

**Reliability:**
- Uptime > 99.5%
- Error rate < 1%
- Zero global service blocks

**API Usage:**
- OSM API calls < 100/hour per user (well under rate limit)
- Average requests per device per week < 50

---

## Future Enhancements

1. Multi-section support with automatic switching
2. Historical patrol scores tracking
3. Patrol score trend analysis
4. Push notifications for score updates
5. Manual refresh capability (with rate limiting)

---

## Implementation Timeline Considerations

This plan is organized by dependency and complexity, not by time estimates. Each phase should be:
- Completed fully before moving to the next
- Tested thoroughly (unit, integration, load)
- Reviewed for security compliance
- Documented as it's built

Work can begin immediately on Phase 1, with phases 2-3 running in parallel after Phase 1 completion.
