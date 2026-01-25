# Testing the Worker-Based UI

## Overview

The UI uses a message-passing architecture with the Service Worker. To test Redux state logic without needing a real Service Worker, we provide a mocking system via `setWorkerFactory()`.

## Mock Worker Pattern

### Basic Setup

```typescript
import { setWorkerFactory, type Worker } from '../worker';
import type { WorkerMessage } from '../../types/messages';

// Create a mock worker
const mockWorker: Worker = {
  onMessage: jest.fn(),
  sendGetProfileRequest: jest.fn(),
};

// Override the factory
setWorkerFactory(() => mockWorker);

// Your tests here...

// Reset to default after tests
setWorkerFactory();
```

### Testing Redux State Updates

To test how the UI responds to worker messages:

1. **Set up Redux store and mock worker**
2. **Render the component with useWorkerBootstrap()**
3. **Simulate worker messages by calling the onMessage handler**
4. **Assert Redux state changes**

Example (requires `@testing-library/react`):

```typescript
import { renderHook, waitFor } from '@testing-library/react';
import { Provider } from 'react-redux';
import { configureStore } from '@reduxjs/toolkit';
import { useWorkerBootstrap } from './hooks/useWorkerBootstrap';
import userReducer from './state/userSlice';
import appReducer from './state/appSlice';

test('user profile message updates Redux state', async () => {
  // Set up store
  const store = configureStore({
    reducer: { user: userReducer, app: appReducer }
  });

  // Set up mock worker
  const mockWorker: Worker = {
    onMessage: jest.fn(),
    sendGetProfileRequest: jest.fn(),
  };
  setWorkerFactory(() => mockWorker);

  // Render hook with Redux provider
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <Provider store={store}>{children}</Provider>
  );
  renderHook(() => useWorkerBootstrap(), { wrapper });

  // Simulate worker message
  const message: WorkerMessage = {
    type: 'user-profile',
    userId: 123,
    userName: 'Test User',
    sections: [
      { id: 1, name: 'Beavers', groupName: 'Test Group' },
    ],
  };

  mockWorker.onMessage(message);

  // Assert state updates
  await waitFor(() => {
    const state = store.getState();
    expect(state.user.userId).toBe('123');
    expect(state.user.userName).toBe('Test User');
    expect(state.app.sections).toHaveLength(1);
    expect(state.app.selectedSectionId).toBe(1); // Auto-selected!
  });

  // Cleanup
  setWorkerFactory();
});
```

## Message Types

### Authentication Flow

```typescript
// User needs to log in
const authRequired: WorkerMessage = {
  type: 'authentication-required',
  loginUrl: '/admin/login',
};
mockWorker.onMessage(authRequired);
// Result: window.location.href set to login URL
```

### User Profile

```typescript
// User logged in successfully
const profile: WorkerMessage = {
  type: 'user-profile',
  userId: 123,
  userName: 'John Doe',
  sections: [
    { id: 1, name: 'Beavers', groupName: 'Test Group' },
    { id: 2, name: 'Cubs', groupName: 'Test Group' },
  ],
};
mockWorker.onMessage(profile);
// Result: Redux user state updated, sections loaded, first section auto-selected
```

### Section List Change

```typescript
// Sections updated (e.g., user gained access to new section)
const sectionsUpdate: WorkerMessage = {
  type: 'section-list-change',
  sections: [
    { id: 1, name: 'Beavers', groupName: 'Test Group' },
    { id: 2, name: 'Cubs', groupName: 'Test Group' },
    { id: 3, name: 'Scouts', groupName: 'Test Group' },
  ],
};
mockWorker.onMessage(sectionsUpdate);
// Result: Redux app.sections updated, preserving patrol data for existing sections
```

### Patrol Scores

```typescript
// Patrol scores loaded/updated
const patrols: WorkerMessage = {
  type: 'patrols-change',
  userId: 123,
  sectionId: 1,
  scores: [
    { id: '1', name: 'Red Patrol', committedScore: 10, pendingScore: 0 },
    { id: '2', name: 'Blue Patrol', committedScore: 20, pendingScore: 5 },
  ],
};
mockWorker.onMessage(patrols);
// Result: Redux app.sections[0].patrols updated, user entries preserved
```

### Session Mismatch

```typescript
// User changed in another tab
const wrongUser: WorkerMessage = {
  type: 'wrong-user',
  requestedUserId: 123,
  currentUserId: 456,
};
mockWorker.onMessage(wrongUser);
// Result: User cleared, data cleared, globalError set, alert shown
```

## Testing Reducer Logic Directly

For unit testing Redux reducers without the worker:

```typescript
import { configureStore } from '@reduxjs/toolkit';
import appReducer, { setCanonicalSections } from './state/appSlice';

test('auto-selects first section when sections loaded', () => {
  const store = configureStore({
    reducer: { app: appReducer }
  });

  store.dispatch(setCanonicalSections([
    { id: 1, name: 'Beavers', groupName: 'Test' },
    { id: 2, name: 'Cubs', groupName: 'Test' },
  ]));

  const state = store.getState();
  expect(state.app.sections).toHaveLength(2);
  expect(state.app.selectedSectionId).toBe(1); // Auto-selected!
});
```

## Installing Test Dependencies

To run the full integration tests shown above:

```bash
npm install -D @testing-library/react
```

## See Also

- `src/ui/hooks/useWorkerBootstrap.test.tsx` - Example tests
- `src/ui/worker.ts` - Worker interface and factory function
- `src/types/messages/` - Message type definitions
