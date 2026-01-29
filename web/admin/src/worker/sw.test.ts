/// <reference lib="webworker" />

// Mock the workbox modules
jest.mock('workbox-precaching', () => ({
  precacheAndRoute: jest.fn(),
  cleanupOutdatedCaches: jest.fn(),
}));

jest.mock('workbox-routing', () => ({
  registerRoute: jest.fn(),
}));

jest.mock('workbox-strategies', () => ({
  NetworkFirst: jest.fn(),
}));

// Mock the client module
jest.mock('./client');

// Mock the server module
jest.mock('./server/server');

// Mock the store module
jest.mock('./store/store');

import { OsmAdapterApiService, NetworkError, ApiError } from './server/server';
import { OpenPatrolPointsStore, PatrolPointsStore, Patrol } from './store/store';
import * as messages from '../types/messages';
import * as clients from './client';
import { getProfile, refreshScores, submitScores } from './sw';

// Get the mocked constructors
const MockOsmAdapterApiService = OsmAdapterApiService as jest.MockedClass<typeof OsmAdapterApiService>;
const MockOpenPatrolPointsStore = OpenPatrolPointsStore as jest.MockedFunction<typeof OpenPatrolPointsStore>;

// Test correlation ID
const TEST_REQUEST_ID = 'test-request-id-123';

// Helper to create a mock client
function createMockClient(): Client {
  return {
    postMessage: jest.fn(),
    id: 'test-client-id',
  } as any;
}

describe('Service Worker Message Handlers', () => {
  let mockApiService: jest.Mocked<OsmAdapterApiService>;
  let mockStore: jest.Mocked<PatrolPointsStore>;
  let mockUow: any;

  beforeEach(() => {
    jest.clearAllMocks();

    // Create mock unit of work
    mockUow = {
      setCommittedScore: jest.fn().mockReturnThis(),
      setRetryAfter: jest.fn().mockReturnThis(),
      setError: jest.fn().mockReturnThis(),
      commit: jest.fn().mockResolvedValue(undefined),
    };

    // Mock OsmAdapterApiService instance
    mockApiService = {
      fetchSession: jest.fn(),
      getLoginUrl: jest.fn().mockReturnValue('/admin/login'),
      fetchSections: jest.fn(),
      fetchScores: jest.fn(),
      updateScores: jest.fn(),
      isOffline: jest.fn().mockReturnValue(false),
    } as any;

    // Mock PatrolPointsStore instance
    mockStore = {
      setCanonicalSectionList: jest.fn(),
      setCanonicalPatrolList: jest.fn(),
      getScoresForSection: jest.fn(),
      setSectionError: jest.fn(),
      setProfileError: jest.fn(),
      getSections: jest.fn(),
      addPendingPoints: jest.fn(),
      acquirePendingForSync: jest.fn(),
      releaseSyncLock: jest.fn(),
      newUnitOfWork: jest.fn().mockReturnValue(mockUow),
      markAllPendingAsFailed: jest.fn(),
      close: jest.fn(),
    } as any;

    // Setup the mocked constructors
    MockOsmAdapterApiService.mockImplementation(() => mockApiService);
    MockOpenPatrolPointsStore.mockResolvedValue(mockStore);
  });

  describe('get-profile message', () => {
    it('should fetch profile and send to client when authenticated', async () => {
      const client = createMockClient();
      const userId = 123;
      const userName = 'Test User';
      const sections = [
        { id: 1, name: 'Beavers', groupName: 'Test Group' },
        { id: 2, name: 'Cubs', groupName: 'Test Group' },
      ];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName,
      });

      mockApiService.fetchSections.mockResolvedValue(sections);
      mockStore.setCanonicalSectionList.mockResolvedValue({
        changed: false,
        sectionsListRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });

      await getProfile(client, TEST_REQUEST_ID);

      expect(mockApiService.fetchSession).toHaveBeenCalled();
      expect(mockApiService.fetchSections).toHaveBeenCalled();
      expect(mockStore.setCanonicalSectionList).toHaveBeenCalledWith(sections);
      expect(client.postMessage).toHaveBeenCalledWith(
        messages.newUserProfileMessage(userId, userName, sections, 1, TEST_REQUEST_ID, undefined, undefined)
      );
      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should broadcast section list change when sections changed', async () => {
      const client = createMockClient();
      const userId = 123;
      const userName = 'Test User';
      const sections = [{ id: 1, name: 'Beavers', groupName: 'Test Group' }];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName,
      });

      mockApiService.fetchSections.mockResolvedValue(sections);
      mockStore.setCanonicalSectionList.mockResolvedValue({
        changed: true,
        sectionsListRevision: 2,
        lastError: undefined,
        lastErrorTime: undefined
      });


      await getProfile(client, TEST_REQUEST_ID);

      expect(clients.sendMessage).toHaveBeenCalledWith(
        messages.newSectionListChangeMessage(userId, sections, 2)
      );
      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should send authentication-required when not logged in', async () => {
      const client = createMockClient();

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: false,
      });

      await getProfile(client, TEST_REQUEST_ID);

      expect(client.postMessage).toHaveBeenCalledWith(
        messages.newAuthenticationRequiredMessage('/admin/login', TEST_REQUEST_ID)
      );
      expect(mockApiService.fetchSections).not.toHaveBeenCalled();
      // Store is never opened on auth failure, so close() is not called
    });

    // Note: wrong-user test removed - getProfile no longer takes userId parameter
    // It now fetches whoever is currently logged in

    it('should send authentication required when fetchSession fails', async () => {
      const client = createMockClient();

      mockApiService.fetchSession.mockRejectedValue(new Error('Network error'));

      await getProfile(client, TEST_REQUEST_ID);

      // Should send authentication required message when session check fails
      expect(client.postMessage).toHaveBeenCalledWith(
        messages.newAuthenticationRequiredMessage('/admin/login', TEST_REQUEST_ID)
      );
      // Store is never opened when auth check fails
    });
  });

  describe('refresh message', () => {
    it('should refresh scores and send to specific client with requestId', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const scores = [
        { id: '1', name: 'Red', score: 10 },
        { id: '2', name: 'Blue', score: 20 },
      ];
      const storedPatrols = [
        new Patrol(userId, sectionId, '1', 'Red', 10),
        new Patrol(userId, sectionId, '2', 'Blue', 20),
      ];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockApiService.fetchScores.mockResolvedValue(scores);
      mockStore.setCanonicalPatrolList.mockResolvedValue({
        patrols: storedPatrols,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });


      await refreshScores(client, TEST_REQUEST_ID, userId, sectionId);

      expect(mockApiService.fetchScores).toHaveBeenCalledWith(sectionId);
      expect(mockStore.setCanonicalPatrolList).toHaveBeenCalledWith(sectionId, scores);
      expect(clients.sendScoresToClient).toHaveBeenCalledWith(client, userId, sectionId, storedPatrols, 1, undefined, undefined, TEST_REQUEST_ID);
      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should not refresh when not authenticated', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: false,
      });


      await refreshScores(client, TEST_REQUEST_ID, userId, sectionId);

      expect(mockApiService.fetchScores).not.toHaveBeenCalled();
      // Store is never opened on auth failure
    });

    it('should send cached scores and error message with requestId when refresh fails', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const cachedScores = [
        new Patrol(userId, sectionId, '1', 'Red', 10),
      ];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      const fetchError = new NetworkError('Fetch failed');
      mockApiService.fetchScores.mockRejectedValue(fetchError);
      mockStore.setSectionError.mockResolvedValue(2);
      mockStore.getScoresForSection.mockResolvedValue({
        patrols: cachedScores,
        uiRevision: 2,
        lastError: 'Unable to refresh patrol scores from server.',
        lastErrorTime: Date.now()
      });

      await refreshScores(client, TEST_REQUEST_ID, userId, sectionId);

      // Should send cached scores with requestId and error state
      expect(clients.sendScoresToClient).toHaveBeenCalledWith(
        client,
        userId,
        sectionId,
        cachedScores,
        2,
        'Unable to refresh patrol scores from server.',
        expect.any(Number),
        TEST_REQUEST_ID
      );

      // The important thing is that close() is ALWAYS called
      expect(mockStore.close).toHaveBeenCalled();
    });
  });

  describe('submit-scores message', () => {

    it('should add pending points and publish optimistic update', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [
        { patrolId: '1', score: 5 },
        { patrolId: '2', score: 3 },
      ];
      const updatedScores = [
        new Patrol(userId, sectionId, '1', 'Red', 10),
        new Patrol(userId, sectionId, '2', 'Blue', 20),
      ];
      updatedScores[0].pendingScoreDelta = 5;
      updatedScores[1].pendingScoreDelta = 3;

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });
      mockStore.acquirePendingForSync.mockResolvedValue({
        lockId: 'lock-123',
        pending: updatedScores,
      });

      mockApiService.updateScores.mockResolvedValue([
        {
          success: true,
          id: '1',
          name: 'Red',
          previousScore: 10,
          newScore: 15,
        },
        {
          success: true,
          id: '2',
          name: 'Blue',
          previousScore: 20,
          newScore: 23,
        },
      ]);



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should add pending points
      expect(mockStore.addPendingPoints).toHaveBeenCalledWith(sectionId, '1', 5);
      expect(mockStore.addPendingPoints).toHaveBeenCalledWith(sectionId, '2', 3);

      // Should publish optimistic update (note: no requestId - this is an unsolicited broadcast to all clients)
      expect(clients.publishScores).toHaveBeenCalledWith(userId, sectionId, updatedScores, 1, undefined, undefined);

      // Should sync to server
      expect(mockApiService.updateScores).toHaveBeenCalled();

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should still save pending changes when not authenticated', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];
      const updatedScores = [new Patrol(userId, sectionId, '1', 'Red', 10)];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: false,
      });

      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should still add pending points
      expect(mockStore.addPendingPoints).toHaveBeenCalledWith(sectionId, '1', 5);

      // Should publish optimistic update
      expect(clients.publishScores).toHaveBeenCalledWith(userId, sectionId, updatedScores, 1, undefined, undefined);

      // Should NOT sync to server
      expect(mockApiService.updateScores).not.toHaveBeenCalled();

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should not sync when offline', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];
      const updatedScores = [new Patrol(userId, sectionId, '1', 'Red', 10)];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockApiService.isOffline.mockReturnValue(true);
      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should add pending points
      expect(mockStore.addPendingPoints).toHaveBeenCalled();

      // Should NOT sync to server when offline
      expect(mockApiService.updateScores).not.toHaveBeenCalled();

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should handle network errors during sync gracefully', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];
      const updatedScores = [new Patrol(userId, sectionId, '1', 'Red', 10)];
      updatedScores[0].pendingScoreDelta = 5;

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });
      mockStore.acquirePendingForSync.mockResolvedValue({
        lockId: 'lock-123',
        pending: updatedScores,
      });

      // Network error during sync
      mockApiService.updateScores.mockRejectedValue(new NetworkError('Network failed'));



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should release lock even on error
      expect(mockStore.releaseSyncLock).toHaveBeenCalledWith(sectionId, 'lock-123');

      // Should NOT mark as failed for network errors
      expect(mockStore.markAllPendingAsFailed).not.toHaveBeenCalled();

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should mark all pending as failed on non-network errors', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];
      const updatedScores = [new Patrol(userId, sectionId, '1', 'Red', 10)];
      updatedScores[0].pendingScoreDelta = 5;

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });
      mockStore.acquirePendingForSync.mockResolvedValue({
        lockId: 'lock-123',
        pending: updatedScores,
      });

      // API error during sync
      mockApiService.updateScores.mockRejectedValue(
        new ApiError(500, 'server_error', 'Internal server error')
      );



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should mark all as failed for API errors - uses e.message || fallback
      expect(mockStore.markAllPendingAsFailed).toHaveBeenCalledWith(
        sectionId,
        expect.any(String) // Accept any string since error.message varies
      );

      // Should release lock
      expect(mockStore.releaseSyncLock).toHaveBeenCalledWith(sectionId, 'lock-123');

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should handle mixed success and error results from server', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [
        { patrolId: '1', score: 5 },
        { patrolId: '2', score: 3 },
        { patrolId: '3', score: 2 },
      ];
      const updatedScores = [
        new Patrol(userId, sectionId, '1', 'Red', 10),
        new Patrol(userId, sectionId, '2', 'Blue', 20),
        new Patrol(userId, sectionId, '3', 'Green', 30),
      ];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockStore.getScoresForSection
        .mockResolvedValueOnce({
          patrols: updatedScores,
          uiRevision: 1,
          lastError: undefined,
          lastErrorTime: undefined
        })
        .mockResolvedValueOnce({
          patrols: updatedScores,
          uiRevision: 2,
          lastError: undefined,
          lastErrorTime: undefined
        });
      mockStore.acquirePendingForSync.mockResolvedValue({
        lockId: 'lock-123',
        pending: updatedScores,
      });

      mockApiService.updateScores.mockResolvedValue([
        {
          success: true,
          id: '1',
          name: 'Red',
          previousScore: 10,
          newScore: 15,
        },
        {
          success: false,
          isTemporaryError: true,
          id: '2',
          name: 'Blue',
          newScore: 23,
          retryAfter: '2024-01-01T00:05:00Z',
          errorMessage: 'Rate limited',
        },
        {
          success: false,
          isTemporaryError: false,
          id: '3',
          name: 'Green',
          newScore: 32,
          errorMessage: 'Patrol deleted',
        },
      ]);



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      const uow = mockStore.newUnitOfWork();

      // Should set committed score for success
      expect(uow.setCommittedScore).toHaveBeenCalledWith(sectionId, '1', 15, 'Red');

      // Should set retry for temporary error
      expect(uow.setRetryAfter).toHaveBeenCalledWith(
        sectionId,
        '2',
        new Date('2024-01-01T00:05:00Z'),
        'Rate limited'
      );

      // Should set error for permanent failure
      expect(uow.setError).toHaveBeenCalledWith(sectionId, '3', 'Patrol deleted');

      expect(uow.commit).toHaveBeenCalled();
      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should not sync when no pending scores', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];
      const updatedScores = [new Patrol(userId, sectionId, '1', 'Red', 10)];

      mockApiService.fetchSession.mockResolvedValue({
        isAuthenticated: true,
        userId,
        userName: 'Test User',
      });

      mockStore.getScoresForSection.mockResolvedValue({
        patrols: updatedScores,
        uiRevision: 1,
        lastError: undefined,
        lastErrorTime: undefined
      });
      mockStore.acquirePendingForSync.mockResolvedValue({
        lockId: 'lock-123',
        pending: [], // No pending!
      });



      await submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas);

      // Should NOT call updateScores when no pending
      expect(mockApiService.updateScores).not.toHaveBeenCalled();

      // Should still release lock
      expect(mockStore.releaseSyncLock).toHaveBeenCalledWith(sectionId, 'lock-123');

      expect(mockStore.close).toHaveBeenCalled();
    });

    it('should close store even if error occurs', async () => {
      const client = createMockClient();
      const userId = 123;
      const sectionId = 1;
      const deltas = [{ patrolId: '1', score: 5 }];

      // Force an error during addPendingPoints
      mockStore.addPendingPoints.mockRejectedValue(new Error('Database error'));



      await expect(submitScores(client, TEST_REQUEST_ID, userId, sectionId, deltas)).rejects.toThrow(
        'Database error'
      );
      expect(mockStore.close).toHaveBeenCalled();
    });
  });
});
