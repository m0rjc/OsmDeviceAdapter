# Service Worker Update Handling Plan

## Current State

### Existing Infrastructure (Legacy)
- **PWA Registration Hook**: `legacy/hooks/useServiceWorker.ts` with update detection
- **Update UI Component**: `legacy/components/UpdatePrompt.tsx` (banner with "Update now" / "Later" buttons)
- **vite-plugin-pwa**: Configured with `registerType: 'prompt'` in `vite.config.ts`
- **Status**: Infrastructure exists but **not integrated** into the new Redux-based app

### Current Architecture (New App)
- **Worker Interface**: `ui/worker.ts` - Abstraction for business logic (scores, profiles, sync)
- **Service Worker**: `worker/sw.ts` - Handles API caching, message passing for patrol scores
- **Client Module**: `worker/client.ts` - Utilities for SW to broadcast messages to all clients
- **Bootstrap Hook**: `ui/hooks/useWorkerBootstrap.ts` - Initializes worker and sets up message handlers
- **Redux State**: UI state managed through Redux with worker messages dispatching actions

### Gap
The new app does **not** detect or handle service worker updates. Users won't be notified when new versions are available.

---

## Service Worker Lifecycle Primer

### Update Flow
1. **New SW Detected**: Browser downloads new `sw.js` when manifest changes
2. **Installing**: New SW enters `installing` state
3. **Waiting**: New SW waits for old SW clients to close (unless `skipWaiting()` called)
4. **Activating**: New SW activates when old SW releases control
5. **Activated**: New SW takes control, `controllerchange` event fires on clients

### Update Strategies
- **Prompt User** (recommended): Show UI, let user decide when to reload
- **Auto-update**: Call `skipWaiting()` immediately and reload all clients
- **Passive**: Update only when all tabs close naturally
- **Background**: Update silently, apply on next visit

---

## Design Approach: Separation of Concerns

### Principle
**Keep PWA lifecycle management separate from business logic worker**

The Service Worker serves two distinct purposes:
1. **PWA Lifecycle**: Caching, offline support, app updates (framework concern)
2. **Business Logic**: Score syncing, API proxying, state management (app concern)

These should remain decoupled.

### Recommended Architecture

```
┌─────────────────────────────────────────────────────┐
│  React UI Layer                                     │
│  ┌──────────────────┐  ┌─────────────────────────┐ │
│  │ UpdatePrompt     │  │ App (Scores UI)         │ │
│  │ (PWA Updates)    │  │ (Business Logic)        │ │
│  └────────┬─────────┘  └───────────┬─────────────┘ │
│           │                        │               │
│           │                        │               │
│  ┌────────▼─────────┐  ┌───────────▼─────────────┐ │
│  │ useServiceWorker │  │ useWorkerBootstrap      │ │
│  │ (PWA Hook)       │  │ (Business Worker Hook)  │ │
│  └────────┬─────────┘  └───────────┬─────────────┘ │
└───────────┼────────────────────────┼───────────────┘
            │                        │
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

**Two parallel systems:**
- **Left side**: PWA update lifecycle (framework concern)
- **Right side**: Business logic worker (app concern)

---

## Addressing Key Concerns

### Q1: Do clients auto-claim on reload, or does the worker have to claim them?

**Answer**: Your service worker **actively claims** clients using `self.clients.claim()`.

**Current implementation** (`worker/sw.ts:19-21`):
```typescript
self.addEventListener('activate', () => {
    self.clients.claim();
});
```

**What this means:**
- **First install**: SW installs → activates → `clients.claim()` immediately takes control (no reload needed)
- **Update flow**: New SW installs → waits for old SW → user triggers update → `skipWaiting()` → activates → `clients.claim()` → takes control
- **Without clients.claim()**: New SW would only control pages loaded *after* activation (requiring manual reload)

**For update handling:**
- When user clicks "Update now", we call `skipWaiting()` on the new SW
- The new SW activates and immediately claims all clients via `clients.claim()`
- All tabs get a new controller, triggering `controllerchange` events
- We reload the page to run the new code

**Verdict**: Your current `clients.claim()` setup is correct for update handling. No changes needed.

---

### Q2: Worker abstraction for non-SW fallback & graceful degradation

**Valid concern**: You value the `Worker` interface abstraction because it allows swapping in a non-SW implementation (e.g., in-process worker for browsers without SW support).

**Current implementation** (`ui/worker.ts:76-96`):
```typescript
const defaultWorkerFactory: WorkerFactory = async () => {
    if (!navigator.serviceWorker) {
        throw new Error("Service Worker API not supported");
    }
    // ... waits for SW and returns WorkerService
};
```

Currently, if SW API is unavailable, the app **throws an error**. There's no fallback Worker implementation yet (though the architecture supports it via the factory pattern).

**The concern**: If we use `registerSW()` directly in the UI layer (outside the Worker abstraction), it creates **two separate mechanisms** for SW interaction:
1. `Worker` interface for business logic
2. `registerSW()` for PWA lifecycle

If you later implement a non-SW Worker (e.g., direct API calls with in-memory state), the `registerSW()` path would still try to register a SW, creating inconsistency.

---

### Q3: Does registerSW() gracefully degrade?

**Yes, with caveats:**

**vite-plugin-pwa behavior:**
- `registerSW()` checks for `'serviceWorker' in navigator` internally
- If SW API unavailable, returns a **noop function** (does nothing, doesn't error)
- Callbacks (`onNeedRefresh`, `onOfflineReady`) simply never fire

**Example graceful degradation:**
```typescript
const updateSW = registerSW({
    onNeedRefresh() { /* never called if no SW support */ }
});
// updateSW is a noop if no SW, safe to call
updateSW(); // Does nothing if SW unavailable
```

**However:**
- This only handles **browser-level SW support** detection
- It does **not** know if you've chosen to use a non-SW Worker implementation
- If you swap in a non-SW Worker via `setWorkerFactory()`, `registerSW()` might still try to register a SW (if SW API exists)

---

### Two Design Options: Choose Your Priority

#### Option A: Separation of Concerns (Recommended in original plan)

**Approach**: Keep PWA lifecycle separate from Worker interface

```
UI Layer:
  - useWorkerBootstrap() → Worker interface (business logic)
  - useServiceWorkerUpdates() → registerSW() (PWA lifecycle)

Two parallel systems that happen to use the same underlying SW
```

**Pros:**
- Clean separation (business logic vs framework lifecycle)
- PWA features (caching, updates) remain framework concerns
- Worker interface stays focused on business domain

**Cons:**
- Two separate mechanisms for SW interaction
- If you implement non-SW Worker, need to also avoid calling `useServiceWorkerUpdates()`
- Slight inconsistency in abstraction

**Graceful degradation:**
```typescript
// In useServiceWorkerUpdates hook
useEffect(() => {
    if (!('serviceWorker' in navigator)) {
        return; // Gracefully skip if no SW support
    }

    const updateSW = registerSW({ ... });
}, []);
```

---

#### Option B: Unified Worker Abstraction

**Approach**: Extend Worker interface to include update notifications

**File**: `ui/worker.ts`

```typescript
export interface Worker {
    onMessage: (message: messages.WorkerMessage) => void;

    // Existing business methods
    sendGetProfileRequest(): string;
    sendRefreshRequest(userId: number, sectionId: number): string;
    sendSubmitScoresRequest(...): string;

    // NEW: PWA lifecycle callbacks
    onUpdateAvailable?: () => void;
    applyUpdate?: () => Promise<void>;
}

class WorkerService implements Worker {
    public onUpdateAvailable?: () => void;
    private updateSW?: () => Promise<void>;

    constructor(sw: ServiceWorkerContainer) {
        // ... existing setup

        // Register for updates
        this.updateSW = registerSW({
            onNeedRefresh: () => {
                if (this.onUpdateAvailable) {
                    this.onUpdateAvailable();
                }
            }
        });
    }

    public async applyUpdate() {
        if (this.updateSW) {
            await this.updateSW();
            window.location.reload();
        }
    }
}

// Future: Non-SW implementation
class InMemoryWorker implements Worker {
    // No onUpdateAvailable or applyUpdate (they're optional)
    // Business methods communicate via direct API calls
}
```

**Usage:**
```typescript
const worker = await GetWorker();

worker.onUpdateAvailable = () => {
    dispatch(setUpdateAvailable(true));
};

// Later, when user clicks "Update now"
await worker.applyUpdate();
```

**Pros:**
- Single abstraction point (all SW interaction via Worker)
- Future non-SW Worker doesn't need parallel update system
- Consistent with your factory pattern philosophy
- Updates remain optional (non-SW implementation simply omits them)

**Cons:**
- Mixes PWA lifecycle with business logic in the interface
- Worker interface becomes less focused (now handles framework and domain)
- `onUpdateAvailable` and `applyUpdate` are optional/unused in non-SW implementation

**Graceful degradation:**
- Non-SW Worker implementation simply doesn't set `onUpdateAvailable`
- UI checks `if (worker.onUpdateAvailable)` before using
- Updates are an optional feature, not a requirement

---

### Recommendation: Choose Based on Priority

**If you prioritize:**
- **Clean separation of concerns** → Option A (separate PWA lifecycle)
- **Unified abstraction** → Option B (extend Worker interface)
- **Simple pragmatism** → Option A (less likely to need non-SW Worker in practice)

**My recommendation**: **Option A** for now, with Option B available if you actually implement a non-SW Worker later. Reasons:
1. PWA updates are a framework concern, not business logic
2. Browser SW support is now >95% globally (you may never need non-SW fallback)
3. Easier to test (update logic separate from business logic)
4. The Worker abstraction already requires SW (`defaultWorkerFactory` throws if unavailable)

If you later implement a non-SW Worker, you can:
- Add an `updateStrategy` parameter to the factory
- Factory decides whether to call `registerSW()` or skip it
- Keep interfaces separate

---

## Implementation Plan

### Phase 1: Redux State for Update Management

**File**: `ui/store/slices/appSlice.ts` (new file or add to existing app state slice)

Add Redux state to track update availability:

```typescript
interface AppState {
  updateAvailable: boolean;
  updateDismissed: boolean;
}

// Actions:
// - setUpdateAvailable(boolean)
// - dismissUpdate()
// - applyUpdate() - triggers reload
```

**Why Redux?**
- Centralized state management (consistent with app architecture)
- Components can reactively subscribe to update state
- Easy to test and mock

---

### Phase 2: Integrate PWA Registration Hook with Redux

**File**: `ui/hooks/useServiceWorkerUpdates.ts` (new hook)

Create a bridge between vite-plugin-pwa and Redux:

```typescript
export function useServiceWorkerUpdates() {
  const dispatch = useDispatch();

  useEffect(() => {
    const updateSW = registerSW({
      onNeedRefresh() {
        // New version available
        dispatch(setUpdateAvailable(true));
      },
      onOfflineReady() {
        // App is offline-ready (optional: could show toast)
        console.log('App is offline-ready');
      },
    });

    // Store the updateSW function for later use
    return updateSW;
  }, [dispatch]);
}
```

**File**: `ui/App.tsx`

Call the hook early in app initialization:

```typescript
function App() {
  useServiceWorkerUpdates();  // Register PWA lifecycle
  useWorkerBootstrap();       // Register business logic worker

  // ... rest of app
}
```

---

### Phase 3: Update Prompt UI Component

**Option A**: Migrate legacy component to new architecture

**File**: `ui/components/UpdatePrompt.tsx` (migrated from legacy)

Adapt the existing `legacy/components/UpdatePrompt.tsx`:
- Read `updateAvailable` from Redux instead of hook state
- Dispatch `applyUpdate()` action instead of calling hook function
- Keep the same banner UI design

**Option B**: Create new component from scratch

Design considerations:
- **Non-intrusive**: Banner at top or bottom (not modal)
- **User control**: "Update now" and "Dismiss" buttons
- **Clarity**: Explain why update is needed ("New version available")
- **Accessibility**: Keyboard navigation, screen reader support

**Placement**: Render in `ui/App.tsx` at root level (outside main content).

---

### Phase 4: Update Application Logic

**File**: `ui/store/slices/appSlice.ts`

Implement the `applyUpdate` thunk:

```typescript
export const applyUpdate = createAsyncThunk(
  'app/applyUpdate',
  async (_, { getState }) => {
    // Call the updateSW function stored during registration
    // This triggers skipWaiting() in the new service worker
    await updateSW();

    // Reload the page to activate the new service worker
    window.location.reload();
  }
);
```

**How it works:**
1. User clicks "Update now"
2. Component dispatches `applyUpdate()`
3. Thunk calls `updateSW()` (from vite-plugin-pwa)
4. New service worker calls `self.skipWaiting()`
5. New SW activates and takes control
6. Page reloads with new code

---

### Phase 5: Service Worker - No Changes Needed

**File**: `worker/sw.ts`

**No modifications required** to the business logic service worker.

**Current implementation is already correct:**
```typescript
self.addEventListener('activate', () => {
    self.clients.claim(); // ✅ Already present (line 19-21)
});
```

**Why this works for updates:**
1. User clicks "Update now" → `updateSW()` called → new SW calls `skipWaiting()`
2. New SW moves from waiting → activating state
3. `activate` event fires → `clients.claim()` executes
4. New SW takes control of all clients immediately (no reload needed for control)
5. Page reload loads new code in newly controlled context

**The vite-plugin-pwa handles the PWA lifecycle separately:**
- Continues to handle score syncing and API caching
- Does **not** need to participate in update detection
- The plugin injects necessary Workbox code during build

**Optional enhancement** (future):
If you want more explicit control, you can add a message handler:

```typescript
self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});
```

Then clients can send `{type: 'SKIP_WAITING'}` instead of relying on the plugin's `updateSW()` function. This gives you more control but requires custom message passing.

---

### Phase 6: Client Coordination (Multi-Tab Support)

**File**: `worker/client.ts` (optional enhancement)

The current `client.ts` already has infrastructure for broadcasting to all clients. For update coordination:

**Challenge**: If user has multiple tabs open, they should all update together.

**Solution Options:**

**Option 1: Reload All Tabs** (simple)
- When one tab accepts update, it sends message to SW
- SW broadcasts "reload now" to all clients
- All clients reload simultaneously

```typescript
// In worker/client.ts (optional new function)
export async function broadcastReloadRequest() {
  const message = { type: 'reload-requested' };
  await sendMessage(message);
}
```

**Option 2: Show Prompt in All Tabs** (more graceful)
- When new SW is waiting, all tabs show update prompt
- User can accept in any tab
- Accepting in one tab triggers reload in all tabs

This requires coordinating via the `updateAvailable` state, which could be synchronized via:
- BroadcastChannel API (modern browsers)
- Service Worker message passing
- SharedWorker (overkill)

**Recommendation**: Start with Option 1 (reload all tabs) for simplicity.

---

### Phase 7: Testing Strategy

**Unit Tests:**
1. Test Redux state transitions (`setUpdateAvailable`, `dismissUpdate`, `applyUpdate`)
2. Mock `registerSW` in `useServiceWorkerUpdates` hook
3. Test UpdatePrompt component rendering and button clicks

**Integration Tests:**
1. Test that `useServiceWorkerUpdates` dispatches Redux actions
2. Test that `applyUpdate` calls `window.location.reload()`

**Manual Testing:**
1. Build app: `npm run build`
2. Serve app: `npm run preview`
3. Make a code change (e.g., update a button label)
4. Build again: `npm run build`
5. Wait 30 seconds (SW polls for updates)
6. Verify update prompt appears
7. Click "Update now", verify reload and new code appears

**Service Worker DevTools:**
- Chrome: `chrome://inspect/#service-workers`
- Firefox: `about:debugging#/runtime/this-firefox`
- Use "Update on reload" checkbox during development

---

## Alternative Approach: Controller Change Detection

**Simpler approach** if you want to avoid Redux complexity:

1. Listen for `controllerchange` event in `useWorkerBootstrap`
2. When new SW takes control, automatically reload:

```typescript
useEffect(() => {
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    window.location.reload();
  });
}, []);
```

**Pros:**
- Very simple, no UI needed
- Automatic updates

**Cons:**
- No user control (disruptive)
- Doesn't follow `registerType: 'prompt'` philosophy
- Poor UX if user is mid-task

**Recommendation**: Only use this for development, not production.

---

## Recommended Approach Summary

### Keep PWA Lifecycle Separate from Business Worker

1. **Use vite-plugin-pwa's `registerSW()`** for update detection
2. **Bridge to Redux** via `useServiceWorkerUpdates` hook
3. **Show UpdatePrompt** component when `updateAvailable` is true
4. **Keep `worker/sw.ts` focused** on business logic (scores, sync)
5. **Keep `Worker` interface focused** on business logic (no update methods)

### Benefits
- ✅ Separation of concerns (PWA vs business logic)
- ✅ Testable (Redux state, mockable hooks)
- ✅ User control (prompt before reload)
- ✅ Reuses existing infrastructure (`useServiceWorker`, `UpdatePrompt`)
- ✅ Consistent with `registerType: 'prompt'` config
- ✅ No changes needed to core business logic worker

---

## Critical Files to Modify

1. **New**: `ui/hooks/useServiceWorkerUpdates.ts` - Bridge between PWA and Redux
2. **New**: `ui/store/slices/appSlice.ts` - Redux state for updates (or add to existing slice)
3. **Migrate**: `ui/components/UpdatePrompt.tsx` - Adapt from legacy version
4. **Edit**: `ui/App.tsx` - Call `useServiceWorkerUpdates()` and render `<UpdatePrompt />`
5. **Optional**: `worker/client.ts` - Add multi-tab reload coordination

---

## Answers to All Questions

### Q1: Does the worker have to claim clients, or do they auto-claim on reload?

**Answer**: Your worker **actively claims** clients using `self.clients.claim()` (already in `worker/sw.ts:19-21`).

**Behavior:**
- **Without `clients.claim()`**: New SW only controls pages loaded *after* activation (requires manual reload)
- **With `clients.claim()`** (your current setup): New SW immediately takes control when activated (no reload needed for control)
- **On update**: New SW → `skipWaiting()` → activates → `clients.claim()` → controls all tabs immediately

**Verdict**: Your current implementation is correct for updates. Keep the existing `clients.claim()`.

---

### Q2: Is registerSW() gracefully degrading if we need to support non-service-worker clients?

**Answer**: `registerSW()` gracefully handles **browser-level** SW support, but not **architecture-level** non-SW Workers.

**Browser-level graceful degradation (automatic):**
```typescript
const updateSW = registerSW({ ... });
// If 'serviceWorker' not in navigator, returns noop function
// Callbacks never fire, no errors thrown
```

**Architecture-level graceful degradation (requires manual handling):**
If you implement a non-SW Worker (e.g., `InMemoryWorker` using direct API calls):
- `registerSW()` doesn't know about your Worker abstraction
- It will still try to register a SW if the browser supports it
- You need to manually skip calling `registerSW()` when using non-SW Worker

**Solution**: Check which Worker implementation you're using before calling `registerSW()`:

**Option A** (Separate PWA lifecycle):
```typescript
// Only call registerSW if using SW-backed Worker
if ('serviceWorker' in navigator) {
    registerSW({ ... });
}
```

**Option B** (Unified in Worker interface):
- WorkerService calls `registerSW()` internally (has `onUpdateAvailable` callback)
- InMemoryWorker doesn't call `registerSW()` (no `onUpdateAvailable` callback)
- Updates become an optional Worker feature

See "Two Design Options" section above for detailed comparison.

---

### Q3: How can the client, through the Worker class, be informed that a new service worker is available?

**Answer**: Two options depending on your priority:

**Option A - Separation of Concerns** (recommended):
- Client should **not** be informed through the `Worker` class
- PWA lifecycle updates go through **separate channel**: `registerSW()` → Redux → UI
- Worker interface stays focused on business logic (scores, profiles, sync)
- Maintains clean separation between framework concerns and business logic

**Option B - Unified Abstraction** (if you prioritize abstraction):
- Extend `Worker` interface with optional `onUpdateAvailable` callback
- `WorkerService` calls `registerSW()` internally and invokes callback
- Non-SW Worker implementation simply doesn't have the callback
- Updates become an optional Worker feature

See "Two Design Options" section above for full details.

---

### Q4: How can the worker, through its clients module, manage those clients during updates?

**Answer**: The service worker's `client.ts` module can optionally help with **multi-tab coordination**:

1. **Current capability**: `sendMessage()` already broadcasts to all clients
2. **New capability** (optional): Add `broadcastReloadRequest()` to tell all tabs to reload
3. **When to use**: After one tab accepts the update, notify other tabs to reload too

However, this is **optional**. The simpler approach:
- Each client independently detects update via `registerSW()`
- Each client shows update prompt independently
- Each client reloads independently when user accepts

For most use cases, independent per-tab handling is sufficient. Multi-tab coordination is only needed if you want synchronized updates across tabs.

---

## Verification Plan

After implementation, verify:

1. **Update detection**: Change code, rebuild, wait 30s, see prompt
2. **Dismiss**: Click "Later", prompt disappears, state updates
3. **Apply**: Click "Update now", page reloads, new code appears
4. **Multi-tab**: Open two tabs, verify both show prompt (if implementing coordination)
5. **Redux state**: Check Redux DevTools, verify `updateAvailable` state
6. **No regressions**: Score syncing still works, offline support intact
7. **Service worker**: Check Application tab in DevTools, verify SW state transitions

---

## Future Enhancements

1. **Auto-update in background**: Update SW silently, apply on next visit (less disruptive)
2. **Update notifications**: Show toast/badge count for available updates
3. **Release notes**: Fetch and display changelog on update
4. **Progressive enhancement**: Detect SW support, gracefully degrade if unavailable
5. **Analytics**: Track update acceptance rate, time to update
