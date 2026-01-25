# UI State Management Architecture

## Overview

This application uses a **hybrid thunk + listener middleware** architecture for managing async worker communication and side effects. This combines the best of both imperative and reactive patterns.

## Core Principles

### 1. Correlation IDs for Request/Response Matching

All worker requests include a unique `requestId` (UUID) that is echoed back in responses:

```typescript
// Request
worker.sendRefreshRequest(userId, sectionId) → returns requestId

// Response (success)
PatrolsChangeMessage { requestId, userId, sectionId, scores }

// Response (error)
ServiceErrorMessage { requestId, function, error }
```

**Benefits:**
- Know which section a `ServiceErrorMessage` relates to
- Prevent duplicate requests (check pending requests before sending)
- Handle rapid UI changes gracefully (user clicks A → B → A)
- Future: timeouts and request cancellation

### 2. Loading State Tracking

Each section tracks its patrol loading state:

```typescript
type PatrolsLoadingState = 'uninitialized' | 'loading' | 'ready' | 'error';
```

**State Transitions:**
- `uninitialized` → `loading`: User selects section / fetch triggered
- `loading` → `ready`: PatrolsChangeMessage received
- `loading` → `error`: ServiceErrorMessage received (no cached data)
- `error` → `loading`: User retries

**Note:** Worker may send both `PatrolsChangeMessage` (cached) and `ServiceErrorMessage` (refresh failed). This results in `ready` state + error dialog, which is correct behavior.

### 3. Pending Request Tracking

The `pendingRequestsSlice` tracks all in-flight worker requests:

```typescript
interface PendingRequest {
  requestId: string;
  type: 'get-profile' | 'refresh' | 'submit-scores';
  sectionId?: number;
  userId?: number;
  timestamp: number;
}
```

**Used for:**
- Correlating `ServiceErrorMessage` with the section that failed
- Preventing duplicate requests
- Future: timeouts (detect stale requests via timestamp)

## Hybrid Architecture Pattern

### Thunks (Imperative)

**Use for explicit user actions** where causality should be obvious.

```typescript
// User manually selects a section
dispatch(selectSectionWithPatrolFetch(sectionId))
```

**Advantages:**
- Clear, traceable flow: user action → thunk → worker call
- Explicit control over when side effects occur
- Easy to test (just test the thunk)
- No cascading effects

**Disadvantages:**
- Can be bypassed if someone dispatches `selectSection` directly
- Requires discipline ("use the thunk, not the action")

### Listeners (Reactive)

**Use as a safety net** for implicit state changes.

```typescript
// Listener auto-fetches when section is selected
listenerMiddleware.startListening({
  matcher: isAnyOf(setCanonicalSections, selectSection),
  effect: async (action, listenerApi) => {
    // Check guards: loading state, pending requests
    // Then: dispatch(fetchPatrolScores(sectionId))
  }
})
```

**Advantages:**
- Cannot be bypassed - always responds to state changes
- Declarative: "whenever X changes, do Y"
- Catches edge cases (e.g., `setCanonicalSections` auto-selecting)

**Disadvantages:**
- Can be harder to trace ("why did this API call happen?")
- Risk of cascading effects if not carefully guarded
- May trigger in unexpected situations

### Our Hybrid Approach

**Thunks for the happy path:**
- `selectSectionWithPatrolFetch(sectionId)` - user manually selects section
- `fetchPatrolScores(sectionId)` - explicit fetch/retry

**Listeners as safety nets:**
- Auto-fetch when `setCanonicalSections` selects first section on load
- Catch any edge cases where section changes through other means

**Guards prevent duplicate work:**
- Listener checks: already loaded? already loading? already pending request?
- Only triggers fetch if all guards pass

## Request/Response Flow

### 1. Sending a Request (fetchPatrolScores thunk)

```typescript
async (sectionId, { dispatch, getState }) => {
  // Generate correlation ID
  const requestId = worker.sendRefreshRequest(userId, sectionId);

  // Track pending request
  dispatch(addPendingRequest({
    requestId,
    type: 'refresh',
    sectionId,
    userId,
    timestamp: Date.now()
  }));

  // Set UI loading state
  dispatch(setPatrolsLoading({ sectionId }));
}
```

### 2. Receiving a Success Response (useWorkerBootstrap)

```typescript
case 'patrols-change':
  // If response includes requestId, remove from pending
  if (message.requestId) {
    const pendingRequest = state.pendingRequests.requests[message.requestId];
    if (pendingRequest) {
      dispatch(removePendingRequest(message.requestId));
    }
  }

  // Update patrol data (sets loading state to 'ready')
  dispatch(setCanonicalPatrols({ sectionId, patrols }));
  break;
```

### 3. Receiving an Error Response (useWorkerBootstrap)

```typescript
case 'service-error':
  // If response includes requestId, look up what failed
  if (message.requestId) {
    const pendingRequest = state.pendingRequests.requests[message.requestId];
    if (pendingRequest && pendingRequest.type === 'refresh') {
      // Set error state on the specific section
      dispatch(setPatrolsError({
        sectionId: pendingRequest.sectionId,
        error: message.error
      }));
      dispatch(removePendingRequest(message.requestId));
    }
  }

  // Always show error dialog for user feedback
  dispatch(showErrorDialog({ title: message.function, message: message.error }));
  break;
```

### 4. Unsolicited Updates (Background Sync)

```typescript
case 'patrols-change':
  // No requestId = background sync or update from another client
  if (!message.requestId) {
    // Still update the data, just don't look for pending request
  }
  dispatch(setCanonicalPatrols({ sectionId, patrols }));
  break;
```

## Error Display Strategy

We have two components for different error scenarios:

### ErrorDialog (Transient Errors)
- **Location:** `ui/components/ErrorDialog.tsx`
- **Use case:** Service errors from worker (e.g., "Failed to refresh scores")
- **Behavior:** Modal dialog, dismissable with "OK" button
- **Triggered by:** `ServiceErrorMessage` from worker

### MessageCard (Persistent Errors)
- **Location:** `ui/components/MessageCard.tsx`
- **Use case:** Persistent states that block UI (e.g., "No section selected", "Session mismatch")
- **Behavior:** Card in main content area, optional retry action
- **Triggered by:** Application state (no section selected, load failed with no cached data)

## Selectors

### Loading State Selectors

```typescript
// Get loading state for selected section
selectPatrolsLoadingStateForSelectedSection(state)
// → 'uninitialized' | 'loading' | 'ready' | 'error'

// Check if currently loading
selectIsPatrolsLoading(state) // → boolean

// Get error message if failed
selectPatrolsError(state) // → string | null

// Check if user can retry
selectCanRetryPatrolsLoad(state) // → boolean
```

### Pending Request Selectors

```typescript
// Check if refresh is pending for a section
selectHasPendingRefreshForSection(sectionId)(state) // → boolean

// Get specific pending request
selectPendingRequest(requestId)(state) // → PendingRequest | undefined
```

## Future Enhancements

### Request Timeout Handling

```typescript
// In a periodic effect or separate listener:
const staleRequests = Object.values(state.pendingRequests.requests)
  .filter(req => Date.now() - req.timestamp > 30000); // 30s timeout

staleRequests.forEach(req => {
  dispatch(removePendingRequest(req.requestId));
  dispatch(setPatrolsError({ sectionId: req.sectionId, error: 'Request timed out' }));
});
```

### Request Registry Pattern

As we add more request types, we may want a registry:

```typescript
interface RequestHandler {
  onSuccess: (response: WorkerMessage, pendingRequest: PendingRequest) => void;
  onError: (error: ServiceErrorMessage, pendingRequest: PendingRequest) => void;
  onTimeout: (pendingRequest: PendingRequest) => void;
}

const requestRegistry: Record<PendingRequestType, RequestHandler> = {
  'refresh': { onSuccess, onError, onTimeout },
  'submit-scores': { onSuccess, onError, onTimeout },
  // ...
};
```

This would centralize error handling logic and make it easier to add new request types.

## Best Practices

1. **Always use thunks for user actions** - Don't bypass them with direct action dispatch
2. **Use correlation IDs for all worker requests** - Required for proper error handling
3. **Check loading state before fetching** - Prevent duplicate requests
4. **Handle both cached and error responses** - Worker may send both
5. **Show transient errors in dialogs** - Use ErrorDialog for dismissable warnings
6. **Show persistent errors in cards** - Use MessageCard for blocking states
7. **Add comprehensive guards to listeners** - Prevent unwanted side effects

## Testing Strategy

### Unit Tests
- Test thunks in isolation (mock worker, check dispatched actions)
- Test reducers (ensure state transitions are correct)
- Test selectors (verify computed values)

### Integration Tests
- Test request/response flow with mock worker
- Verify correlation IDs are properly tracked
- Ensure loading states transition correctly

### E2E Tests
- Test real worker communication in browser
- Verify background sync updates work
- Test error recovery flows
