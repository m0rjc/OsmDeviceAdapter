# WebSocket Bridge: Real-Time Admin-to-Scoreboard Communication

## Problem Statement

Currently, scoreboards poll the server every 5 seconds for score updates. When an admin updates scores via the web UI, the scoreboard won't see the change until its next cache-miss poll cycle. This creates a noticeable delay during live events where immediate feedback matters.

Additionally, there is no mechanism for the admin to send commands to a specific scoreboard (e.g., refresh now, display a timer). A real-time bidirectional channel between the admin web client and the scoreboard would enable immediate score refresh notifications and future interactive features like countdown timers (Story 006).

**Important context:** Scores can change outside the admin interface (e.g., other users editing via the OSM website directly). The normal polling cycle must continue at its configured interval to pick up external changes. The WebSocket bridge is an *enhancement* for admin-initiated changes, not a replacement for polling.

## Overview

Introduce a WebSocket-based real-time bridge between the admin web client and scoreboard devices. The admin initiates a connection request; the scoreboard picks it up on its next poll and establishes a WebSocket connection. Once both sides are connected, the server relays messages between them with low latency.

The system is designed for horizontal scaling from day one using Redis pub/sub for cross-instance message routing.

### User Experience

**Admin Web Client:**
- When viewing scores for a section, the admin sees scoreboards registered to that section
- Admin can request a scoreboard open a real-time connection
- A status indicator shows connection state: disconnected (grey), requested (amber), connected (blue)
- When connected, score updates submitted via the admin UI trigger an immediate refresh notification to the scoreboard
- Admin can disconnect when finished

**Scoreboard Device:**
- Normal polling continues at the configured interval (currently 5s) regardless of WebSocket state
- On a poll response, the device sees a `websocketRequested: true` flag if a connection has been requested
- The device opens a WebSocket connection to `/ws/device`
- While connected, the device shows a blue status indicator alongside the existing green one
- On receiving a `refresh-scores` message, the device immediately polls for fresh data
- If the WebSocket disconnects, the device continues with normal polling (graceful degradation)

### Connection Lifecycle

```
Admin clicks "Connect" on a scoreboard
  → POST /api/admin/scoreboards/{id}/connect
  → Server stores connection request flag in Redis (TTL 5 minutes)

Scoreboard polls GET /api/v1/patrols (normal cycle)
  ← Response includes: { "websocket": { "requested": true } }

Scoreboard opens WebSocket: /ws/device?token={deviceAccessToken}
  → Server validates device token
  → Server registers device in local connection registry
  → Server publishes "device-connected" event via Redis pub/sub
  → Admin's WebSocket receives: { type: "device-connected", deviceId: "..." }
  → UI updates to show blue connected indicator

Admin updates scores via existing POST endpoint
  → Score update handler updates OSM + refreshes cache
  → Handler publishes "refresh-scores" via Redis pub/sub
  → Instance holding device WebSocket relays to device
  → Device immediately polls, gets fresh cached data

Admin clicks "Disconnect" (or closes browser / session expires / idle timeout)
  → Server sends close frame to device WebSocket
  → Device falls back to polling-only mode
```

## Architectural Decisions

### Decision 1: WebSocket Library

**Option A: gorilla/websocket** ← Recommended

- Battle-tested, widely used in Go ecosystem
- Good control over connection lifecycle, ping/pong, close codes
- Not in Go stdlib but extremely stable

**Option B: nhooyr.io/websocket (coder/websocket)**

- More modern API, uses `context.Context` natively
- Supports `io.Reader`/`io.Writer` interface
- Smaller dependency

**Recommendation:** gorilla/websocket for its maturity and ecosystem support.

### Decision 2: Connection Registry & Horizontal Scaling

**Design: In-memory registry + Redis pub/sub** ← Recommended

WebSocket connections are inherently local to the Go process that accepted them. However, in a multi-replica deployment, the admin and device may connect to different instances.

- **In-memory map**: Each instance tracks its own active WebSocket connections
- **Redis pub/sub channels**: Used for cross-instance message routing
  - `ws:device:{deviceCode}` — messages to a specific device
  - `ws:admin:{sessionId}` — messages to a specific admin session
  - `ws:section:{sectionId}` — broadcast to all admins viewing a section
- **Redis keys**: Used for connection request state and device connection tracking
  - `ws:request:{deviceCode}` — connection request flag (TTL 5 min)
  - `ws:connected:{deviceCode}` — which instance holds the connection

**Flow for cross-instance messaging:**
1. Admin on instance A sends "refresh-scores" for device X
2. Instance A publishes to Redis channel `ws:device:{deviceCode}`
3. Instance B (holding device X's WebSocket) receives via subscription
4. Instance B sends the message over the local WebSocket to device X

This works for single-replica today and scales to multiple replicas without code changes.

### Decision 3: Admin WebSocket Strategy

**Option A: Dedicated admin WebSocket** ← Recommended

- Admin connects to `/ws/admin` after authenticating
- Server pushes device status changes and relayed messages
- Clean separation: admin WS for control plane, device WS for data plane
- Session cookie auth on upgrade (consistent with existing admin auth)

**Option B: Server-Sent Events (SSE) for admin, WebSocket for device**

- SSE is simpler for server→client only
- But we need bidirectional for admin commands (connect, disconnect, timer controls)
- Would require SSE + POST endpoints, more complex client code

**Recommendation:** WebSocket for both admin and device for symmetry and bidirectional support.

### Decision 4: Authentication on WebSocket Upgrade

**Device WebSocket (`/ws/device`):**
- Device access token passed as query parameter: `/ws/device?token={deviceAccessToken}`
- Query param necessary because WebSocket API doesn't support custom headers on upgrade
- Token validated on upgrade; connection rejected if invalid
- Same auth logic as existing device middleware

**Admin WebSocket (`/ws/admin`):**
- Session cookie sent automatically on upgrade (same-origin)
- Origin header checked to prevent cross-origin WebSocket hijacking
- Session validated on upgrade; connection rejected if invalid/expired
- Session ID used to route messages to the correct admin connection

### Decision 5: Message Protocol

JSON messages with a `type` field for routing:

```typescript
// Admin → Server
{ type: "connect-device", deviceId: string }
{ type: "disconnect-device", deviceId: string }

// Server → Admin
{ type: "device-connected", deviceId: string }
{ type: "device-disconnected", deviceId: string, reason: string }
{ type: "device-status", deviceId: string, connected: boolean }
{ type: "error", message: string }

// Server → Device
{ type: "refresh-scores" }
{ type: "disconnect", reason: string }

// Device → Server
{ type: "status", uptime: number }
```

The protocol is extensible — Story 006 (Countdown Timer) adds timer-related message types without changing the transport layer.

### Decision 6: Polling Endpoint Integration

The existing `GET /api/v1/patrols` response gains an optional `websocket` field:

```json
{
  "patrols": [...],
  "fromCache": true,
  "websocket": {
    "requested": true
  }
}
```

- The `websocket` field is only present when a connection has been requested (Redis key exists)
- The device is responsible for deciding whether to open the WebSocket
- The request flag is cleared once the device connects (or expires after 5 minutes)
- Normal polling behaviour is completely unchanged

### Decision 7: Score Refresh Flow

When the admin updates scores, the propagation to the scoreboard is:

1. Admin submits score update via `POST /api/admin/sections/{id}/scores` (existing endpoint)
2. Handler updates scores in OSM (existing behaviour)
3. **New:** Handler re-fetches patrol scores from OSM and writes to Redis cache with standard TTL
4. **New:** Handler publishes a `refresh-scores` event to Redis pub/sub for all devices on that section
5. Instance holding the device WebSocket relays `refresh-scores` to the device
6. Device immediately calls `GET /api/v1/patrols`
7. Response comes from the freshly-updated cache — sub-second total latency

If no WebSocket is connected, step 4-6 are skipped and the device picks up changes on its next normal poll cycle (unchanged behaviour).

## Components

### Backend — New Package: `internal/websocket/`

**`hub.go`** — Connection registry and message router
- In-memory maps: `deviceConns map[string]*DeviceConn`, `adminConns map[string]*AdminConn`
- Redis pub/sub subscriber for cross-instance messages
- Methods: `RegisterDevice()`, `UnregisterDevice()`, `RegisterAdmin()`, `UnregisterAdmin()`
- Methods: `SendToDevice()`, `SendToAdmin()`, `BroadcastToSection()`

**`device_handler.go`** — WebSocket upgrade + read/write loops for devices
- Validates device access token on upgrade
- Read pump: receives status messages from device
- Write pump: sends commands to device (refresh, timer, disconnect)
- Ping/pong heartbeat management

**`admin_handler.go`** — WebSocket upgrade + read/write loops for admin
- Validates session cookie on upgrade
- Read pump: receives commands from admin (connect-device, disconnect-device)
- Write pump: sends status updates to admin

**`messages.go`** — Message type definitions and JSON serialisation

**`redis_pubsub.go`** — Redis pub/sub integration for horizontal scaling
- Subscribe to channels for locally-connected clients
- Publish events for remote delivery

### Backend — Modified Packages

**`internal/handlers/admin_api.go`** — New endpoints
- `GET /api/admin/scoreboards` — List scoreboards for user's sections (device codes in `authorized` status)
- `POST /api/admin/scoreboards/{id}/connect` — Set connection request flag in Redis
- `POST /api/admin/scoreboards/{id}/disconnect` — Clear connection, close WebSocket

**`internal/handlers/api.go`** — Modified
- Add `websocket` field to patrol scores response when Redis connection request key exists

**`internal/server/server.go`** — Modified
- Register WebSocket upgrade endpoints: `/ws/device`, `/ws/admin`
- Initialise WebSocket hub on server start, shut down on graceful stop
- Inject hub dependency into handlers

**`internal/handlers/admin_api.go`** — Modified score update handler
- After successful score update, re-fetch and cache patrol scores
- Publish `refresh-scores` event via hub

### Frontend — Admin Web Client

**`web/admin/src/ui/state/websocketSlice.ts`** — New Redux slice
- Admin WebSocket connection state (connecting, connected, disconnected)
- Per-scoreboard connection state map

**`web/admin/src/ui/components/ScoreboardPanel.tsx`** — New component
- Lists scoreboards for current section
- Status indicator (grey/amber/blue dot) per scoreboard
- Connect/disconnect buttons

**`web/admin/src/ui/hooks/useAdminWebSocket.ts`** — New hook
- Manages admin WebSocket lifecycle (connect, reconnect with backoff, close)
- Dispatches Redux actions on incoming messages
- Provides `sendMessage()` function

**`web/admin/src/ui/components/ConnectionIndicator.tsx`** — New component
- Small coloured dot component reusable across UI

### Device Side (Separate Repository)

The scoreboard device firmware would need updating to:
1. Check for `websocket.requested` in poll response
2. Open WebSocket to server when requested
3. Handle `refresh-scores` by triggering immediate poll
4. Handle `disconnect` by closing WebSocket gracefully
5. Show blue status indicator when WebSocket is active
6. Continue normal polling regardless of WebSocket state

## Non-Functional Requirements

- **Graceful degradation**: WebSocket is optional; polling always works
- **Horizontal scaling**: Redis pub/sub ensures messages route across instances
- **Connection limits**: Max 1 WebSocket per device, max 5 admin WebSocket connections per session
- **Heartbeat**: Ping every 30 seconds; close after 60s without pong
- **Idle timeout**: Close device WebSocket after 30 minutes of no admin-initiated messages
- **Request expiry**: Connection request flags in Redis expire after 5 minutes
- **Shutdown**: Graceful WebSocket close on server shutdown (close frame with reason)

## Testing Strategy

- Unit tests for hub registration/unregistration and message routing
- Unit tests for auth validation on WebSocket upgrade
- Integration tests using gorilla/websocket client for full upgrade + message exchange
- Redis pub/sub integration tests for cross-instance routing
- Mock WebSocket connections for handler tests
- End-to-end test with mock OSM server: admin updates score → device receives refresh notification
