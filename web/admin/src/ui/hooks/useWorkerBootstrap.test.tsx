/**
 * Example test demonstrating how to mock the worker for testing.
 *
 * This file demonstrates the pattern for mocking the worker using setWorkerFactory().
 *
 * To run full integration tests with useWorkerBootstrap, install @testing-library/react:
 *   npm install -D @testing-library/react
 */

import { setWorkerFactory, type Worker } from '../worker';
import type { WorkerMessage } from '../../types/messages';

describe('Worker mocking pattern', () => {
  it('should allow mocking the worker factory', () => {
    // Create mock worker
    const mockWorker: Worker = {
      onMessage: jest.fn(),
      sendGetProfileRequest: jest.fn().mockReturnValue('request-id-123'),
      sendRefreshRequest: jest.fn().mockReturnValue('request-id-456'),
      sendSubmitScoresRequest: jest.fn().mockReturnValue('request-id-789'),
    };

    // Override the worker factory to return our mock (now async)
    setWorkerFactory(async () => mockWorker);

    // Reset to default factory
    setWorkerFactory();

    expect(true).toBe(true);
  });

  it('should demonstrate creating mock worker messages', () => {
    // Example of worker messages that can be sent to test Redux state updates
    const profileMessage: WorkerMessage = {
      type: 'user-profile',
      requestId: 'req-123',
      userId: 123,
      userName: 'Test User',
      sections: [
        { id: 1, name: 'Beavers', groupName: 'Test Group' },
      ],
      sectionsListRevision: 1,
    };

    const sectionsMessage: WorkerMessage = {
      type: 'section-list-change',
      userId: 123,
      sections: [
        { id: 1, name: 'Beavers', groupName: 'Test Group' },
        { id: 2, name: 'Cubs', groupName: 'Test Group' },
      ],
      sectionsListRevision: 2,
    };

    const patrolsMessage: WorkerMessage = {
      type: 'patrols-change',
      requestId: 'req-456', // Optional - may be absent for unsolicited updates
      userId: 123,
      sectionId: 1,
      uiRevision: 1,
      scores: [
        { id: '1', name: 'Red Patrol', committedScore: 10, pendingScore: 0 },
      ],
    };

    // These messages can be passed to mockWorker.onMessage() in tests
    expect(profileMessage.type).toBe('user-profile');
    expect(sectionsMessage.type).toBe('section-list-change');
    expect(patrolsMessage.type).toBe('patrols-change');
  });
});

/*
 * Full integration test examples (requires @testing-library/react):
 *
 * import { renderHook, waitFor } from '@testing-library/react';
 * import { Provider } from 'react-redux';
 * import { configureStore } from '@reduxjs/toolkit';
 * import { useWorkerBootstrap } from './useWorkerBootstrap';
 * import userReducer from '../state/userSlice';
 * import appReducer from '../state/appSlice';
 *
 * it('should handle user-profile message and update Redux state', async () => {
 *   const store = configureStore({
 *     reducer: { user: userReducer, app: appReducer }
 *   });
 *
 *   const mockWorker: Worker = {
 *     onMessage: jest.fn(),
 *     sendGetProfileRequest: jest.fn().mockReturnValue('req-123'),
 *     sendRefreshRequest: jest.fn().mockReturnValue('req-456'),
 *   };
 *   setWorkerFactory(async () => mockWorker);
 *
 *   const wrapper = ({ children }: { children: React.ReactNode }) => (
 *     <Provider store={store}>{children}</Provider>
 *   );
 *
 *   renderHook(() => useWorkerBootstrap(), { wrapper });
 *
 *   // Simulate worker message
 *   const message: WorkerMessage = {
 *     type: 'user-profile',
 *     requestId: 'req-123',
 *     userId: 123,
 *     userName: 'Test User',
 *     sections: [
 *       { id: 1, name: 'Beavers', groupName: 'Test Group' },
 *       { id: 2, name: 'Cubs', groupName: 'Test Group' },
 *     ],
 *   };
 *   mockWorker.onMessage(message);
 *
 *   await waitFor(() => {
 *     const state = store.getState();
 *     expect(state.user.userId).toBe('123');
 *     expect(state.user.userName).toBe('Test User');
 *     expect(state.app.sections).toHaveLength(2);
 *     expect(state.app.selectedSectionId).toBe(1); // Auto-selected
 *   });
 *
 *   setWorkerFactory(); // Reset
 * });
 */
