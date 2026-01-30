# Service Worker Update Handling Implementation

This document describes the implementation of service worker update handling for the OSM Device Adapter admin PWA.

## Overview

The implementation adds user-visible notifications when a new version of the PWA is available, following the **separation of concerns** principle:

- **PWA lifecycle** (framework concern): Handled by `vite-plugin-pwa` and new hooks
- **Business logic worker** (app concern): Existing worker architecture remains unchanged
- Both use the same underlying service worker but manage different responsibilities

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  React UI Layer                                     │
│  ┌──────────────────┐  ┌─────────────────────────┐ │
│  │ UpdatePrompt     │  │ App (Scores UI)         │ │
│  │ (PWA Updates)    │  │ (Business Logic)        │ │
│  └────────┬─────────┘  └───────────┬─────────────┘ │
│           │                        │               │
│  ┌────────▼─────────┐  ┌───────────▼─────────────┐ │
│  │ useServiceWorker │  │ useWorkerBootstrap      │ │
│  │ Updates          │  │ (Business Worker Hook)  │ │
│  └────────┬─────────┘  └───────────┬─────────────┘ │
└───────────┼────────────────────────┼───────────────┘
            │                        │
┌───────────▼────────────┐  ┌────────▼──────────────┐
│ vite-plugin-pwa        │  │ Worker Interface      │
│ registerSW()           │  │ (WorkerService)       │
│ (Update Detection)     │  │ (Message Passing)     │
└────────────────────────┘  └───────────┬───────────┘
                                        │
                            ┌───────────▼───────────┐
                            │ Service Worker (sw.ts)│
                            │ - API Caching         │
                            │ - Score Sync          │
                            │ - Message Handlers    │
                            └───────────────────────┘
```

## Files Created/Modified

### New Files

1. **`src/ui/state/appSlice.ts`**
   - Redux slice for app-level PWA lifecycle state
   - Manages `updateAvailable` and `updateDismissed` flags
   - Provides selectors for components to determine when to show update prompt

2. **`src/ui/hooks/useServiceWorkerUpdates.ts`**
   - Hook that bridges vite-plugin-pwa with Redux
   - Calls `registerSW()` to detect updates
   - Dispatches Redux actions when updates are available
   - Exports `applyServiceWorkerUpdate()` function for triggering updates

3. **`src/ui/components/UpdatePrompt.tsx`**
   - Banner component that appears when updates are available
   - Provides "Update now" and "Later" buttons
   - Uses existing CSS from legacy implementation

### Modified Files

1. **`src/ui/state/rootReducer.ts`**
   - Added `app` slice to root reducer
   - Exported app-level selectors (`selectShouldShowUpdatePrompt`, `selectUpdateAvailable`)
   - Exported `AppState` type

2. **`src/ui/state/index.ts`**
   - Exported app slice actions (`setUpdateAvailable`, `dismissUpdate`)
   - Exported app slice selectors
   - Exported `AppState` type

3. **`src/ui/hooks/index.ts`**
   - Exported `useServiceWorkerUpdates` and `applyServiceWorkerUpdate`

4. **`src/ui/components/index.ts`**
   - Exported `UpdatePrompt` component

5. **`src/ui/App.tsx`**
   - Added `useServiceWorkerUpdates()` hook call in App component
   - Added `<UpdatePrompt />` to all return paths (before ErrorDialog and other content)

### Unchanged Files

- **`src/worker/sw.ts`**: No changes needed - already has `self.clients.claim()` in activate event
- **`vite.config.ts`**: Already configured with `registerType: 'prompt'`
- **`src/styles.css`**: Already has `.update-banner` styles from legacy implementation

## How It Works

### Update Detection Flow

1. **Browser detects new SW**: When manifest changes, browser downloads new `sw.js`
2. **vite-plugin-pwa detects waiting SW**: `registerSW()` callback fires
3. **Redux state updated**: `useServiceWorkerUpdates` dispatches `setUpdateAvailable(true)`
4. **UI renders prompt**: `UpdatePrompt` component appears at top of screen

### Update Application Flow

1. **User clicks "Update now"**: `UpdatePrompt` calls `applyServiceWorkerUpdate()`
2. **Trigger skipWaiting**: `updateSW()` tells new SW to skip waiting
3. **New SW activates**: Activate event fires, calls `self.clients.claim()`
4. **Page reloads**: `window.location.reload()` loads new code in new SW context

### Dismiss Flow

1. **User clicks "Later"**: `UpdatePrompt` dispatches `dismissUpdate()`
2. **Prompt hides**: `selectShouldShowUpdatePrompt` returns false
3. **Flag resets on new update**: When `setUpdateAvailable(true)` is dispatched again, `updateDismissed` resets to false

## Redux State

```typescript
interface AppState {
  updateAvailable: boolean;      // True when new SW version detected
  updateDismissed: boolean;       // True when user clicked "Later"
}
```

## Key Design Decisions

### Why Separation of Concerns?

**Option A (chosen)**: Keep PWA lifecycle separate from Worker interface
- ✅ Clean separation (business logic vs framework lifecycle)
- ✅ PWA features remain framework concerns
- ✅ Worker interface stays focused on business domain
- ✅ Easier to test (update logic separate from business logic)

**Option B (not chosen)**: Extend Worker interface with update notifications
- Would mix PWA lifecycle with business logic in the interface
- Worker interface becomes less focused
- Updates would be optional feature of Worker (breaks single responsibility)

### Why Redux?

- Centralized state management (consistent with app architecture)
- Components can reactively subscribe to update state
- Easy to test and mock
- Allows multiple components to react to update availability

### Why Global Window Variable?

The `useServiceWorkerUpdates` hook stores `updateSW` on `window.__updateServiceWorker` to avoid prop drilling. This is an acceptable tradeoff because:
- Only one instance of the hook exists (called once in App.tsx)
- The function is framework-level, not business logic
- Alternative would be Redux thunk, which is overkill for a one-time function

## Service Worker Lifecycle

### Client Claiming

The service worker uses `self.clients.claim()` in the activate event (sw.ts:19-21):

```typescript
self.addEventListener('activate', () => {
    self.clients.claim();
});
```

This ensures:
- **First install**: SW immediately takes control (no reload needed)
- **Updates**: New SW claims all tabs when activated
- **Multi-tab**: All tabs get new controller simultaneously

### Update Flow

1. **New SW detected**: Installing → Waiting (waits for old SW to release)
2. **User clicks "Update now"**: `skipWaiting()` called on new SW
3. **Activating**: New SW moves from waiting → activating state
4. **Activated**: `clients.claim()` takes control of all clients
5. **Reload**: Page reloads to run new code in new SW context

## Testing

### Manual Testing Steps

1. Build app: `npm run build`
2. Serve app: `npm run preview`
3. Make a code change (e.g., update button label)
4. Build again: `npm run build`
5. Wait 30 seconds (SW polls for updates)
6. Verify update prompt appears
7. Click "Later", verify prompt disappears
8. Click "Update now", verify reload and new code appears

### Automated Testing

Unit tests would require `@testing-library/react`, which is not currently installed. The test file was removed due to Jest configuration issues with .ts import extensions.

When `@testing-library/react` is available, tests should cover:
- UpdatePrompt renders when `updateAvailable && !updateDismissed`
- UpdatePrompt hidden when `!updateAvailable || updateDismissed`
- Clicking "Later" dispatches `dismissUpdate()`
- Clicking "Update now" calls `applyServiceWorkerUpdate()`

## Future Enhancements

1. **Auto-update in background**: Update SW silently, apply on next visit (less disruptive)
2. **Update notifications**: Show toast/badge count for available updates
3. **Release notes**: Fetch and display changelog on update
4. **Multi-tab coordination**: Synchronize update prompt across all open tabs
5. **Analytics**: Track update acceptance rate, time to update

## Troubleshooting

### Update prompt doesn't appear

- Check browser DevTools → Application → Service Workers
- Verify new SW is in "waiting" state
- Check console for `registerSW()` errors
- Verify `registerType: 'prompt'` in vite.config.ts

### Page reloads but new code doesn't appear

- Check if new SW actually activated (DevTools → Application → Service Workers)
- Verify `clients.claim()` is called in activate event
- Try hard refresh (Ctrl+Shift+R) to bypass cache

### Update prompt appears repeatedly

- Check if Redux state is persisting incorrectly
- Verify `setUpdateAvailable(true)` resets `updateDismissed` to false
- Check for multiple instances of `useServiceWorkerUpdates` hook

## References

- Plan document: Implementation plan provided before coding
- vite-plugin-pwa docs: https://vite-pwa-org.netlify.app/
- Service Worker lifecycle: https://web.dev/service-worker-lifecycle/
- RFC 8628 (Device Flow): Used elsewhere in this project for OAuth
