# OSM OAuth 2.0 Documentation

## Overview

OSM uses OAuth 2.0 for authorization. OAuth is a standard mechanism that allows users to grant access to their account without sharing their password with third-party applications.

**Recommendation:** Use an OAuth client library for your programming language and configure it with the URLs and scopes documented below.

### OAuth Flows

- **Authorization Code Flow**: Use this for applications that will be used by multiple users
- **Client Credentials Flow**: Use this if you are the only user of the application (uses the user account that created the application)

## OAuth Endpoints

### Authorization Endpoint
```
https://www.onlinescoutmanager.co.uk/oauth/authorize
```
For the authorization code flow, your OAuth client library will build a link based on this URL. When users click this link, they will be taken to OSM to log in and authorize your application. Upon successful authorization, they will be redirected to your specified Redirect URL.

### Token Endpoint
```
https://www.onlinescoutmanager.co.uk/oauth/token
```
After the user is redirected back to your application, your client library will request this URL to obtain an **access token** and **refresh token**. **Store these tokens securely in your database.**

### Resource Owner Endpoint
```
https://www.onlinescoutmanager.co.uk/oauth/resource
```
This endpoint provides the user's full name, email address, and a list of sections your application can access.

## Scopes

**Scope format:** `section:{scope_name}:{permission_level}`

Where `{permission_level}` is:
- `:read` - Read-only access
- `:write` - Read and write access
- `:admin` - Administrative access (only for `administration` and `finance` scopes)

**Important:** Request the minimum permissions required. Users must have all the permissions your application requests, or they won't be able to see any sections.

### Available Scopes

| Scope | Description |
|-------|-------------|
| `administration` | Administration and settings areas |
| `attendance` | Attendance Register |
| `badge` | Badge records |
| `event` | Events |
| `finance` | Financial areas (online payments, invoices, etc) |
| `flexirecord` | Flexi-records |
| `member` | Personal details, adding/removing/transferring members, emailing, contact details |
| `programme` | Programme |
| `quartermaster` | Quartermaster's area |


## API Usage

### Discovering API Endpoints

The OSM API is **unsupported and undocumented**, but your application can perform any action available on the website.

**Discovery method:**
1. Open your browser's developer console
2. Monitor network requests while performing actions you want to automate
3. Note whether requests are GET or POST
4. Use your OAuth client library to create authenticated requests (with Bearer token in the Authorization header)

---

## ⚠️ Rate Limiting (CRITICAL)

**Applications that frequently exceed rate limits will be permanently blocked.**

### Rate Limit Headers

Monitor these headers in every API response:

| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Maximum requests per hour (per authenticated user) |
| `X-RateLimit-Remaining` | Requests remaining before being blocked |
| `X-RateLimit-Reset` | Seconds until the rate limit resets |

### HTTP 429 - Too Many Requests

When the rate limit is exceeded:
- **Status Code:** `429 Too Many Requests`
- **Header:** `Retry-After` - seconds until you can retry
- **Action Required:** Stop making requests and wait for the specified time

### Best Practices

- **Implement your own rate limiting** - especially for unauthenticated user actions (e.g., waiting list signups)
- **Monitor rate limit headers** proactively and throttle requests before hitting the limit
- **Add exponential backoff** when approaching limits

---

## ⚠️ Error Handling and Blocking (CRITICAL)

### X-Deprecated Header

**Warning:** APIs with this header will be removed after the specified date.
- Monitor for `X-Deprecated` header in responses
- Update your application before the removal date

### X-Blocked Header

**CRITICAL:** Your application has been blocked.

**Reasons for blocking:**
- Using invalid data
- Attempting to access unauthorized resources
- Frequently making invalid requests
- Ignoring rate limits

**⚠️ WARNING:** Continuing to use the API after being blocked will result in a **PERMANENT BLOCK**.

### HTTP Status Codes

| Status Code | Meaning | Action Required |
|-------------|---------|-----------------|
| `200` | Success | Continue normal operation |
| `429` | Too Many Requests | Wait for `Retry-After` seconds |
| `4xx` | Client Error | Check request validity and authorization |
| `5xx` | Server Error | Implement retry with exponential backoff |

---

## Security and Validation Requirements

### Input Validation
- **Always sanitize and validate** all data before sending to OSM
- Invalid data will result in your application being blocked

### Response Validation
- **Check all responses** and abort if they are not as expected
- Frequently performing invalid requests will result in blocking

### Bot Protection
- If allowing unauthenticated access, **verify users are not bots**
- Implement CAPTCHA or similar protection mechanisms

---

## Summary of Critical Requirements

1. **Monitor rate limit headers** - implement throttling before hitting limits
2. **Handle HTTP 429** - respect `Retry-After` header
3. **Check for X-Blocked** - stop immediately if blocked
4. **Watch for X-Deprecated** - update before removal dates
5. **Validate all input** - sanitize data before sending
6. **Verify responses** - ensure they match expectations
7. **Protect from bots** - if allowing unauthenticated access
