import { OsmAdapterApiService, ApiError, NetworkError } from './server';
import * as api from './types';

describe('OsmAdapterApiService', () => {
  let service: OsmAdapterApiService;
  let mockFetch: jest.Mock;

  beforeEach(() => {
    service = new OsmAdapterApiService();
    mockFetch = jest.fn();
    global.fetch = mockFetch;
  });

  afterEach(() => {
    jest.resetAllMocks();
  });

  describe('Initial State', () => {
    it('should start unauthenticated', () => {
      expect(service.isAuthenticated).toBe(false);
      expect(service.user).toBeNull();
      expect(service.userId).toBeNull();
    });
  });

  describe('fetchSession', () => {
    it('should fetch authenticated session and update state', async () => {
      const sessionResponse: api.SessionResponse = {
        authenticated: true,
        user: {
          osmUserId: 123,
          name: 'Test User',
        },
        selectedSectionId: 1,
        csrfToken: 'test-csrf-token',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      const status = await service.fetchSession();

      expect(mockFetch).toHaveBeenCalledWith('/api/admin/session', {
        credentials: 'same-origin',
      });
      expect(status).toEqual({
        isAuthenticated: true,
        userId: 123,
        userName: 'Test User',
      });
      expect(service.isAuthenticated).toBe(true);
      expect(service.user).toEqual({
        osmUserId: 123,
        name: 'Test User',
      });
      expect(service.userId).toBe(123);
    });

    it('should fetch unauthenticated session', async () => {
      const sessionResponse: api.SessionResponse = {
        authenticated: false,
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      const status = await service.fetchSession();

      expect(status).toEqual({
        isAuthenticated: false,
      });
      expect(service.isAuthenticated).toBe(false);
      expect(service.user).toBeNull();
      expect(service.userId).toBeNull();
    });

    it('should handle API error responses', async () => {
      const errorResponse: api.ErrorResponse = {
        error: 'invalid_session',
        message: 'Session expired',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: async () => errorResponse,
      });

      const promise = service.fetchSession();
      await expect(promise).rejects.toThrow(ApiError);
      await expect(promise).rejects.toThrow('Session expired');
    });

    it('should handle malformed API error responses', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => { throw new Error('Parse error'); },
      });

      const promise = service.fetchSession();
      await expect(promise).rejects.toThrow(ApiError);
      await expect(promise).rejects.toThrow('An unexpected error occurred');
    });

    it('should throw NetworkError on network failure', async () => {
      mockFetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));

      const promise = service.fetchSession();
      await expect(promise).rejects.toThrow(NetworkError);
      await expect(promise).rejects.toThrow('Network request failed');
    });
  });

  describe('Authentication State', () => {
    it('should clear session on 401 error', async () => {
      // First authenticate
      const sessionResponse: api.SessionResponse = {
        authenticated: true,
        user: { osmUserId: 123, name: 'Test User' },
        csrfToken: 'test-csrf-token',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      await service.fetchSession();
      expect(service.isAuthenticated).toBe(true);

      // Then receive 401 error
      const errorResponse: api.ErrorResponse = {
        error: 'unauthorized',
        message: 'Not authorized',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: async () => errorResponse,
      });

      await expect(service.fetchSections()).rejects.toThrow(ApiError);
      expect(service.isAuthenticated).toBe(false);
      expect(service.user).toBeNull();
    });

    it('should clear session on 403 error', async () => {
      // First authenticate
      const sessionResponse: api.SessionResponse = {
        authenticated: true,
        user: { osmUserId: 123, name: 'Test User' },
        csrfToken: 'test-csrf-token',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      await service.fetchSession();
      expect(service.isAuthenticated).toBe(true);

      // Then receive 403 error
      const errorResponse: api.ErrorResponse = {
        error: 'forbidden',
        message: 'Forbidden',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 403,
        json: async () => errorResponse,
      });

      await expect(service.fetchSections()).rejects.toThrow(ApiError);
      expect(service.isAuthenticated).toBe(false);
      expect(service.user).toBeNull();
    });
  });

  describe('getLoginUrl', () => {
    it('should return the login URL', () => {
      expect(service.getLoginUrl()).toBe('/admin/login');
    });
  });

  describe('deauthenticate', () => {
    it('should log out authenticated user', async () => {
      // First authenticate
      const sessionResponse: api.SessionResponse = {
        authenticated: true,
        user: { osmUserId: 123, name: 'Test User' },
        csrfToken: 'test-csrf-token',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      await service.fetchSession();
      expect(service.isAuthenticated).toBe(true);

      // Then logout
      mockFetch.mockResolvedValueOnce({
        ok: true,
      });

      const result = await service.deauthenticate();

      expect(mockFetch).toHaveBeenCalledWith('/admin/logout', {
        method: 'POST',
        credentials: 'same-origin',
      });
      expect(result).toBe(true);
      expect(service.isAuthenticated).toBe(false);
    });

    it('should handle logout when already unauthenticated', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
      });

      const result = await service.deauthenticate();

      expect(result).toBe(true);
      expect(service.isAuthenticated).toBe(false);
    });

    it('should throw error on logout failure', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
      });

      await expect(service.deauthenticate()).rejects.toThrow('Logout failed with status 500');
    });
  });

  describe('fetchSections', () => {
    it('should fetch sections successfully', async () => {
      const sectionsResponse: api.SectionsResponse = {
        sections: [
          { id: 1, name: 'Beavers', groupName: 'Test Group' },
          { id: 2, name: 'Cubs', groupName: 'Test Group' },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sectionsResponse,
      });

      const sections = await service.fetchSections();

      expect(mockFetch).toHaveBeenCalledWith('/api/admin/sections', {
        credentials: 'same-origin',
      });
      expect(sections).toEqual([
        { id: 1, name: 'Beavers', groupName: 'Test Group' },
        { id: 2, name: 'Cubs', groupName: 'Test Group' },
      ]);
    });

    it('should handle empty sections list', async () => {
      const sectionsResponse: api.SectionsResponse = {
        sections: [],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sectionsResponse,
      });

      const sections = await service.fetchSections();

      expect(sections).toEqual([]);
    });

    it('should handle API errors', async () => {
      const errorResponse: api.ErrorResponse = {
        error: 'server_error',
        message: 'Failed to fetch sections',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => errorResponse,
      });

      const promise = service.fetchSections();
      await expect(promise).rejects.toThrow(ApiError);
      await expect(promise).rejects.toThrow('Failed to fetch sections');
    });

    it('should throw NetworkError on network failure', async () => {
      mockFetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));

      await expect(service.fetchSections()).rejects.toThrow(NetworkError);
    });
  });

  describe('fetchScores', () => {
    it('should fetch patrol scores successfully', async () => {
      const scoresResponse: api.ScoresResponse = {
        section: { id: 1, name: 'Beavers' },
        termId: 1,
        patrols: [
          { id: '1', name: 'Red', score: 10 },
          { id: '2', name: 'Blue', score: 20 },
        ],
        fetchedAt: '2024-01-01T00:00:00Z',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => scoresResponse,
      });

      const scores = await service.fetchScores(1);

      expect(mockFetch).toHaveBeenCalledWith('/api/admin/sections/1/scores', {
        credentials: 'same-origin',
      });
      expect(scores).toEqual([
        { id: '1', name: 'Red', score: 10 },
        { id: '2', name: 'Blue', score: 20 },
      ]);
    });

    it('should handle empty patrols list', async () => {
      const scoresResponse: api.ScoresResponse = {
        section: { id: 1, name: 'Beavers' },
        termId: 1,
        patrols: [],
        fetchedAt: '2024-01-01T00:00:00Z',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => scoresResponse,
      });

      const scores = await service.fetchScores(1);

      expect(scores).toEqual([]);
    });

    it('should handle API errors', async () => {
      const errorResponse: api.ErrorResponse = {
        error: 'not_found',
        message: 'Section not found',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: async () => errorResponse,
      });

      const promise = service.fetchScores(999);
      await expect(promise).rejects.toThrow(ApiError);
      await expect(promise).rejects.toThrow('Section not found');
    });

    it('should throw NetworkError on network failure', async () => {
      mockFetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));

      await expect(service.fetchScores(1)).rejects.toThrow(NetworkError);
    });
  });

  describe('updateScores', () => {
    beforeEach(async () => {
      // Authenticate first to get CSRF token
      const sessionResponse: api.SessionResponse = {
        authenticated: true,
        user: { osmUserId: 123, name: 'Test User' },
        csrfToken: 'test-csrf-token',
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => sessionResponse,
      });

      await service.fetchSession();
      mockFetch.mockClear();
    });

    it('should update scores successfully', async () => {
      const updateResponse: api.UpdateResponse = {
        patrols: [
          {
            id: '1',
            name: 'Red',
            success: true,
            previousScore: 10,
            newScore: 15,
          },
          {
            id: '2',
            name: 'Blue',
            success: true,
            previousScore: 20,
            newScore: 23,
          },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [
        { patrolId: '1', score: 5 },
        { patrolId: '2', score: 3 },
      ];

      const results = await service.updateScores(1, updates);

      expect(mockFetch).toHaveBeenCalledWith('/api/admin/sections/1/scores', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': 'test-csrf-token',
        },
        body: JSON.stringify([
          { patrolId: '1', points: 5 },
          { patrolId: '2', points: 3 },
        ]),
      });

      expect(results).toEqual([
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
    });

    it('should handle temporary errors with retry after', async () => {
      const updateResponse: api.UpdateResponse = {
        patrols: [
          {
            id: '1',
            name: 'Red',
            success: false,
            previousScore: 10,
            newScore: 15,
            isTemporaryError: true,
            retryAfter: '2024-01-01T00:05:00Z',
            errorMessage: 'Rate limit exceeded',
          },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [{ patrolId: '1', score: 5 }];
      const results = await service.updateScores(1, updates);

      expect(results).toEqual([
        {
          success: false,
          isTemporaryError: true,
          id: '1',
          name: 'Red',
          newScore: 15,
          retryAfter: '2024-01-01T00:05:00Z',
          errorMessage: 'Rate limit exceeded',
        },
      ]);
    });

    it('should handle permanent errors', async () => {
      const updateResponse: api.UpdateResponse = {
        patrols: [
          {
            id: '1',
            name: 'Red',
            success: false,
            previousScore: 10,
            newScore: 15,
            isTemporaryError: false,
            errorMessage: 'Patrol not found',
          },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [{ patrolId: '1', score: 5 }];
      const results = await service.updateScores(1, updates);

      expect(results).toEqual([
        {
          success: false,
          isTemporaryError: false,
          id: '1',
          name: 'Red',
          newScore: 15,
          errorMessage: 'Patrol not found',
        },
      ]);
    });

    it('should handle mixed success and error results', async () => {
      const updateResponse: api.UpdateResponse = {
        patrols: [
          {
            id: '1',
            name: 'Red',
            success: true,
            previousScore: 10,
            newScore: 15,
          },
          {
            id: '2',
            name: 'Blue',
            success: false,
            previousScore: 20,
            newScore: 23,
            isTemporaryError: true,
            retryAfter: '2024-01-01T00:05:00Z',
            errorMessage: 'Rate limit exceeded',
          },
          {
            id: '3',
            name: 'Green',
            success: false,
            previousScore: 30,
            newScore: 33,
            isTemporaryError: false,
            errorMessage: 'Patrol deleted',
          },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [
        { patrolId: '1', score: 5 },
        { patrolId: '2', score: 3 },
        { patrolId: '3', score: 3 },
      ];

      const results = await service.updateScores(1, updates);

      expect(results).toHaveLength(3);
      expect(results[0].success).toBe(true);

      expect(results[1].success).toBe(false);
      const result1 = results[1] as any;
      expect(result1.isTemporaryError).toBe(true);

      expect(results[2].success).toBe(false);
      const result2 = results[2] as any;
      expect(result2.isTemporaryError).toBe(false);
    });

    it('should handle API errors', async () => {
      const errorResponse: api.ErrorResponse = {
        error: 'validation_error',
        message: 'Invalid patrol ID',
      };

      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => errorResponse,
      });

      const updates = [{ patrolId: 'invalid', score: 5 }];
      const promise = service.updateScores(1, updates);

      await expect(promise).rejects.toThrow(ApiError);
      await expect(promise).rejects.toThrow('Invalid patrol ID');
    });

    it('should use empty string for CSRF token when not authenticated', async () => {
      // Create new service without authentication
      const unauthService = new OsmAdapterApiService();
      global.fetch = mockFetch;

      const updateResponse: api.UpdateResponse = {
        patrols: [],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [{ patrolId: '1', score: 5 }];
      await unauthService.updateScores(1, updates);

      expect(mockFetch).toHaveBeenCalledWith('/api/admin/sections/1/scores', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'X-CSRF-Token': '',
        },
        body: JSON.stringify([{ patrolId: '1', points: 5 }]),
      });
    });

    it('should handle default error messages', async () => {
      const updateResponse: api.UpdateResponse = {
        patrols: [
          {
            id: '1',
            name: 'Red',
            success: false,
            previousScore: 10,
            newScore: 15,
            isTemporaryError: true,
            retryAfter: '2024-01-01T00:05:00Z',
            // No errorMessage provided
          },
          {
            id: '2',
            name: 'Blue',
            success: false,
            previousScore: 20,
            newScore: 23,
            isTemporaryError: false,
            // No errorMessage provided
          },
        ],
      };

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => updateResponse,
      });

      const updates = [
        { patrolId: '1', score: 5 },
        { patrolId: '2', score: 3 },
      ];

      const results = await service.updateScores(1, updates);

      expect(results[0].success).toBe(false);
      const result0 = results[0] as any;
      expect(result0.isTemporaryError).toBe(true);
      expect(result0.errorMessage).toBe('Temporary error');

      expect(results[1].success).toBe(false);
      const result1 = results[1] as any;
      expect(result1.isTemporaryError).toBe(false);
      expect(result1.errorMessage).toBe('Update failed');
    });

    it('should throw NetworkError on network failure', async () => {
      mockFetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));

      const updates = [{ patrolId: '1', score: 5 }];
      const promise = service.updateScores(1, updates);

      await expect(promise).rejects.toThrow(NetworkError);
      await expect(promise).rejects.toThrow('Network request failed');
    });
  });

  describe('Error Types', () => {
    it('should create ApiError with correct properties', () => {
      const error = new ApiError(404, 'not_found', 'Resource not found');

      expect(error).toBeInstanceOf(Error);
      expect(error.name).toBe('ApiError');
      expect(error.statusCode).toBe(404);
      expect(error.errorCode).toBe('not_found');
      expect(error.message).toBe('Resource not found');
    });

    it('should create NetworkError with correct properties', () => {
      const cause = new TypeError('Failed to fetch');
      const error = new NetworkError('Network request failed', cause);

      expect(error).toBeInstanceOf(Error);
      expect(error.name).toBe('NetworkError');
      expect(error.message).toBe('Network request failed');
      expect(error.cause).toBe(cause);
    });
  });
});
