# Service Worker Update Handling - Implementation Summary

## What Was Implemented

A complete PWA update notification system that alerts users when a new version of the application is available and allows them to choose when to update.

## Key Features

✅ **User-visible update notifications**: Banner appears at top of screen when new version available
✅ **User control**: "Update now" or "Later" buttons
✅ **Redux state management**: Centralized update state
✅ **Separation of concerns**: PWA lifecycle separate from business logic worker
✅ **Clean architecture**: Reuses existing CSS, follows established patterns
✅ **No service worker changes needed**: Existing `clients.claim()` sufficient

## Implementation Details

### Architecture Choice

**Followed Option A: Separation of Concerns**

Two parallel systems using the same underlying service worker:
- **PWA lifecycle**: `useServiceWorkerUpdates` → `registerSW()` → Redux → `UpdatePrompt`
- **Business logic**: `useWorkerBootstrap` → Worker interface → score syncing

This keeps framework concerns (PWA updates) separate from business logic (score management).

### Files Modified/Created

**New files:**
- `src/ui/state/appSlice.ts` - Redux slice for PWA lifecycle state
- `src/ui/hooks/useServiceWorkerUpdates.ts` - Hook bridging vite-plugin-pwa to Redux
- `src/ui/components/UpdatePrompt.tsx` - Update notification banner component
- `web/admin/IMPLEMENTATION_NOTES.md` - Detailed implementation documentation

**Modified files:**
- `src/ui/state/rootReducer.ts` - Added app slice
- `src/ui/state/index.ts` - Exported app actions/selectors
- `src/ui/hooks/index.ts` - Exported new hook
- `src/ui/components/index.ts` - Exported UpdatePrompt
- `src/ui/App.tsx` - Called hook and rendered component

**Unchanged (as expected):**
- `src/worker/sw.ts` - Already has `clients.claim()` in activate event
- `vite.config.ts` - Already configured with `registerType: 'prompt'`
- `src/styles.css` - Already has `.update-banner` styles

## How It Works

### Update Detection
1. Browser detects new service worker version
2. `registerSW()` callback fires with "need refresh" event
3. `useServiceWorkerUpdates` dispatches `setUpdateAvailable(true)`
4. `UpdatePrompt` component appears at top of screen

### Update Application
1. User clicks "Update now"
2. `applyServiceWorkerUpdate()` called
3. New service worker calls `skipWaiting()`
4. New SW activates and claims all clients
5. Page reloads with new code

### Dismiss
1. User clicks "Later"
2. Redux state: `updateDismissed` set to true
3. Prompt disappears
4. Flag resets when next update becomes available

## Testing

### Build Verification
```bash
npm run build  # ✅ Successful
npm test       # ✅ All 82 tests passing
```

### Manual Testing Steps
1. Build: `npm run build`
2. Serve: `npm run preview`
3. Make code change and rebuild
4. Wait ~30 seconds for SW to detect update
5. Verify banner appears
6. Test "Later" button (dismisses)
7. Test "Update now" button (reloads)

## Design Decisions Explained

### Q: Why not extend the Worker interface with update methods?
**A:** Separation of concerns. PWA lifecycle is a framework concern, not business logic. The Worker interface should stay focused on domain operations (scores, profiles, sync).

### Q: Why use Redux for update state?
**A:** Consistency with app architecture, reactive components, easy testing, centralized state management.

### Q: Why store updateSW on window object?
**A:** Avoid prop drilling for a one-time framework function. Alternative (Redux thunk) would be overkill. Only one instance exists (App.tsx).

### Q: Does the service worker need changes?
**A:** No. Existing `clients.claim()` in activate event (sw.ts:19-21) already provides the necessary client takeover behavior for updates.

## Answered Questions from Plan

### Q1: Do clients auto-claim on reload?
**A:** The worker actively claims clients using `self.clients.claim()` (already present at sw.ts:19-21). This ensures new SW immediately takes control when activated.

### Q2: Is registerSW() gracefully degrading?
**A:** Yes for browser-level SW support (returns noop if no SW API). Architecture-level graceful degradation (non-SW Worker) would require manual handling, but 95%+ browsers support SW now.

### Q3: How can the client be informed of updates through the Worker class?
**A:** It shouldn't. PWA lifecycle is kept separate from Worker interface (framework vs business logic). Updates go through `registerSW()` → Redux → UI.

### Q4: How can the worker manage clients during updates?
**A:** The `client.ts` module can optionally broadcast reload requests for multi-tab coordination. Currently, each tab handles updates independently (simpler approach).

## What's Next

Optional future enhancements:
1. **Multi-tab coordination**: Synchronize updates across all open tabs
2. **Background updates**: Update silently, apply on next visit
3. **Release notes**: Display changelog when update available
4. **Update analytics**: Track acceptance rate and timing

## Verification

✅ Build successful (no TypeScript errors)
✅ All existing tests pass (82/82)
✅ No regressions in business logic
✅ Service worker lifecycle unchanged
✅ Redux state properly typed
✅ Component follows established patterns
✅ CSS reused from legacy implementation

## Documentation

See `IMPLEMENTATION_NOTES.md` for:
- Detailed architecture diagrams
- Complete file-by-file changes
- Service worker lifecycle explanation
- Troubleshooting guide
- Testing strategy
