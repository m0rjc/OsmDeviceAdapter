# Mock Admin Server

A development server that mocks the OSM Device Adapter admin API for frontend development.

## Features

- Pre-authenticated session (no OAuth required)
- Two mock sections with 3 patrols each
- **Configurable rate limiting** to test error handling
- CORS enabled for local development

## Usage

### Basic Usage

```bash
make mock-server
```

Server runs on http://localhost:8081

### With Vite Dev Server

```bash
make dev
```

Starts both mock server (8081) and Vite dev server (5173).

## Configuration

### Rate Limiting

By default, the mock server enforces a **1 update per 60 seconds** rate limit per patrol. This allows you to test the UI's error handling for temporary errors.

#### Configure Rate Limit

Set `MOCK_RATE_LIMIT_SECONDS` environment variable:

```bash
# 5 second rate limit (fast testing)
MOCK_RATE_LIMIT_SECONDS=5 make mock-server

# 1 minute rate limit (default)
MOCK_RATE_LIMIT_SECONDS=60 make mock-server

# Disable rate limiting
MOCK_RATE_LIMIT_SECONDS=0 make mock-server
```

#### Testing Error Icons

To test the patrol error icon UI:

1. Start mock server with short rate limit:
   ```bash
   MOCK_RATE_LIMIT_SECONDS=5 make dev
   ```

2. In the UI, add points to a patrol and submit
3. Try to update the same patrol again within 5 seconds
4. You should see:
   - **Orange warning triangle** icon appears next to patrol name
   - Tooltip shows error message and countdown timer
   - After 5 seconds, the error clears automatically

#### Rate Limit Response

When rate limited, the server returns:

```json
{
  "success": true,
  "patrols": [
    {
      "id": "patrol_1",
      "name": "Eagles",
      "success": false,
      "isTemporaryError": true,
      "retryAfter": "2026-02-04T21:45:00Z",
      "errorMessage": "Rate limit exceeded. Please wait 60 seconds between updates for this patrol."
    }
  ]
}
```

## Endpoints

- `GET /api/admin/session` - Returns authenticated session
- `GET /api/admin/sections` - Returns list of sections
- `GET /api/admin/sections/{id}/scores` - Returns patrol scores for a section
- `POST /api/admin/sections/{id}/scores` - Update patrol scores (rate limited)
- `GET /health` - Health check

## Mock Data

**Sections:**
- 1001: "1st Anytown Scouts" (1st Anytown Group)
- 1002: "2nd Anytown Scouts" (2nd Anytown Group)

**Patrols (Section 1001):**
- patrol_1: Eagles (42 points)
- patrol_2: Hawks (38 points)
- patrol_3: Owls (45 points)

**Patrols (Section 1002):**
- patrol_4: Panthers (51 points)
- patrol_5: Tigers (47 points)
- patrol_6: Wolves (49 points)

## Authentication

The mock server simulates an authenticated session with:
- User ID: 12345
- User Name: "Test User"
- CSRF Token: "mock-csrf-token"
- Selected Section: 1001 (1st Anytown Scouts)

No OAuth flow required - perfect for rapid frontend development.
