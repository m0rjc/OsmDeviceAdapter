import * as api from './types';
import * as model from '../../types/model';

/**
 * Error thrown when an API request fails
 */
export class ApiError extends Error {
  statusCode: number;
  errorCode: string;

  constructor(statusCode: number, errorCode: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.errorCode = errorCode;
  }
}

/**
 * Error thrown when a network request fails (offline, connection issues, etc.)
 * This indicates a temporary/retryable error condition
 */
export class NetworkError extends Error {
  public readonly cause?: any;

  constructor(message: string, cause?: any) {
    super(message);
    this.cause = cause;
    this.name = 'NetworkError';
  }
}

export type OsmUser = api.UserInfo

export type SessionStatusAuthenticated = {
  isAuthenticated: true;
  userId: number;
  userName: string;
}

export type SessionStatusUnauthenticated = {
  isAuthenticated: false;
}

export type SessionStatus = SessionStatusAuthenticated | SessionStatusUnauthenticated;

export type PatrolScore = api.Patrol;

export type PatrolScoreUpdateResultSuccess = {
  success: true;
  id: string; // Server returns string IDs (from OSM API)
  name: string;
  previousScore: number;
  newScore: number;
}

export type PatrolScoreUpdateResultTemporaryError = {
  success: false;
  isTemporaryError: true;
  id: string; // Server returns string IDs (from OSM API)
  name: string;
  newScore: number;
  retryAfter: string; // Server returns ISO date string
  errorMessage: string;
}

export type PatrolScoreUpdateResultError = {
  success: false;
  isTemporaryError: false;
  id: string; // Server returns string IDs (from OSM API)
  name: string;
  newScore: number;
  errorMessage: string;
}

export type PatrolScoreUpdateResult = PatrolScoreUpdateResultSuccess | PatrolScoreUpdateResultTemporaryError | PatrolScoreUpdateResultError;

// Settings response types
export type SectionSettings = {
  sectionId: number;
  patrolColors: Record<string, string>;
  patrols: PatrolInfoResponse[];
}

export type PatrolInfoResponse = {
  id: string;
  name: string;
}

/**
 * OsmAdapterApiService represents a connection to the OSMAdapter server.
 * This service is designed to be used within a service worker context.
 *
 * Usage pattern:
 * 1. For authenticated operations: construct with userId, userName, and csrfToken from IndexedDB
 * 2. For bootstrap/re-authentication: construct without args, call fetchSession(), then persist to IndexedDB
 * 3. Use the API methods to interact with the server
 */
export class OsmAdapterApiService {
  private _userId: number | null;
  private _userName: string | null;
  private _csrfToken: string | null;

  /**
   * @param userId - OSM user ID (optional, required for authenticated operations)
   * @param userName - User's display name (optional, for display purposes)
   * @param csrfToken - CSRF token for state-changing requests (optional, fetched if not provided)
   */
  constructor(userId?: number, userName?: string, csrfToken?: string) {
    this._userId = userId ?? null;
    this._userName = userName ?? null;
    this._csrfToken = csrfToken ?? null;
  }

  private get csrfToken(): string | null {
    return this._csrfToken;
  }

  /** Get the CSRF token for persistence (e.g., to IndexedDB) */
  public getCsrfToken(): string | null {
    return this._csrfToken;
  }

  /** Is the user authenticated? */
  public get isAuthenticated(): boolean {
    return this._userId !== null;
  }

  /** The current user if authenticated, otherwise null. */
  public get user(): OsmUser | null {
    if (!this.isAuthenticated || !this._userId) {
      return null;
    }
    return {
      osmUserId: this._userId,
      name: this._userName ?? ''
    };
  }

  /** The current OSM user ID if authenticated, otherwise null. */
  public get userId(): number | null {
    return this._userId;
  }

  public isOffline(): boolean {
    return !navigator.onLine;
  }

  /**
   * Perform a fetch request and handle the response
   * @throws {NetworkError} for network/offline errors (retryable)
   * @throws {ApiError} for API errors
   */
  private async fetchAndHandle<T>(url: string, init?: RequestInit): Promise<T> {
    let response: Response;
    try {
      response = await fetch(url, init);
    } catch (error) {
      throw new NetworkError('Network request failed. Check your internet connection.', error);
    }

    if (!response.ok) {
      // Handle authentication errors - clear session state
      if (response.status === 401 || response.status === 403) {
        this._userId = null;
        this._userName = null;
        this._csrfToken = null;
      }

      let errorData: api.ErrorResponse;
      try {
        errorData = await response.json();
      } catch {
        throw new ApiError(response.status, 'unknown_error', 'An unexpected error occurred');
      }
      throw new ApiError(response.status, errorData.error, errorData.message);
    }
    return response.json();
  }

  /**
   * Fetch the current session information and update this class' authentication state.
   * Call this during bootstrap or re-authentication, then persist the returned values to IndexedDB.
   * @returns SessionStatus with userId, userName, and csrfToken (if authenticated)
   * @throws {NetworkError} for network/offline errors
   * @throws {ApiError} for API errors
   */
  async fetchSession(): Promise<SessionStatus> {
    const sessionResponse = await this.fetchAndHandle<api.SessionResponse>('/api/admin/session', {
      credentials: 'same-origin',
    });

    if (sessionResponse.authenticated && sessionResponse.user) {
      // Update internal state
      this._userId = sessionResponse.user.osmUserId;
      this._userName = sessionResponse.user.name;
      this._csrfToken = sessionResponse.csrfToken ?? null;

      return {
        isAuthenticated: true,
        userId: sessionResponse.user.osmUserId,
        userName: sessionResponse.user.name
      };
    }

    // Clear state on unauthenticated response
    this._userId = null;
    this._userName = null;
    this._csrfToken = null;

    return {
      isAuthenticated: false
    };
  }

  /**
   * Get the login URL for OAuth authentication.
   * The caller should redirect the user to this URL.
   *
   * @returns The login URL path
   */
  getLoginUrl(): string {
    return '/admin/login';
  }

  /**
   * Log out the current user.
   *
   * @returns True if the user was logged out (state changed from authenticated to unauthenticated)
   * @throws {Error} if the logout request fails
   */
  async deauthenticate(): Promise<boolean> {
    const response = await fetch('/admin/logout', {
      method: 'POST',
      credentials: 'same-origin',
    });

    if (response.ok || response.status === 401) {
      // Clear authentication state
      this._userId = null;
      this._userName = null;
      this._csrfToken = null;
      return true;
    }

    throw new Error(`Logout failed with status ${response.status}`);
  }

  /**
   * Fetch the list of sections the user has access to
   * @throws {NetworkError} for network/offline errors
   * @throws {ApiError} for API errors
   */
  async fetchSections(): Promise<model.Section[]> {
    const parsedResponse = await this.fetchAndHandle<api.SectionsResponse>('/api/admin/sections', {
      credentials: 'same-origin',
    });
    return parsedResponse.sections;
  }

  /**
   * Fetch patrol scores for a specific section
   * @throws {NetworkError} for network/offline errors
   * @throws {ApiError} for API errors
   */
  async fetchScores(sectionId: number): Promise<PatrolScore[]> {
    const raw = await this.fetchAndHandle<api.ScoresResponse>(`/api/admin/sections/${sectionId}/scores`, {
      credentials: 'same-origin',
    });
    return raw.patrols;
  }

  /**
   * Fetch settings for a specific section
   * @throws {NetworkError} for network/offline errors
   * @throws {ApiError} for API errors
   */
  async fetchSettings(sectionId: number): Promise<SectionSettings> {
    const raw = await this.fetchAndHandle<api.SettingsResponse>(`/api/admin/sections/${sectionId}/settings`, {
      credentials: 'same-origin',
    });
    return {
      sectionId: raw.sectionId,
      patrolColors: raw.patrolColors || {},
      patrols: raw.patrols || [],
    };
  }

  /**
   * Update settings for a specific section
   * @throws {NetworkError} for network/offline errors (retryable)
   * @throws {ApiError} for API errors
   */
  async updateSettings(sectionId: number, patrolColors: Record<string, string>): Promise<SectionSettings> {
    const raw = await this.fetchAndHandle<api.SettingsResponse>(`/api/admin/sections/${sectionId}/settings`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify({ patrolColors }),
    });
    return {
      sectionId: raw.sectionId,
      patrolColors: raw.patrolColors || {},
      patrols: raw.patrols || [],
    };
  }

  // ============================================================================
  // Ad-hoc Patrol Methods
  // ============================================================================

  /**
   * Fetch the user's ad-hoc patrols
   */
  async fetchAdhocPatrols(): Promise<api.AdhocPatrol[]> {
    return this.fetchAndHandle<api.AdhocPatrol[]>('/api/admin/adhoc/patrols', {
      credentials: 'same-origin',
    });
  }

  /**
   * Create a new ad-hoc patrol
   */
  async createAdhocPatrol(name: string, color: string): Promise<api.AdhocPatrol> {
    return this.fetchAndHandle<api.AdhocPatrol>('/api/admin/adhoc/patrols', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify({ name, color }),
    });
  }

  /**
   * Update an ad-hoc patrol's name and color
   */
  async updateAdhocPatrol(id: string, name: string, color: string): Promise<api.AdhocPatrol> {
    return this.fetchAndHandle<api.AdhocPatrol>(`/api/admin/adhoc/patrols/${id}`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify({ name, color }),
    });
  }

  /**
   * Delete an ad-hoc patrol
   */
  async deleteAdhocPatrol(id: string): Promise<void> {
    const response = await fetch(`/api/admin/adhoc/patrols/${id}`, {
      method: 'DELETE',
      credentials: 'same-origin',
      headers: {
        'X-CSRF-Token': this.csrfToken || '',
      },
    });
    if (!response.ok && response.status !== 204) {
      let errorData: api.ErrorResponse;
      try {
        errorData = await response.json();
      } catch {
        throw new ApiError(response.status, 'unknown_error', 'Failed to delete patrol');
      }
      throw new ApiError(response.status, errorData.error, errorData.message);
    }
  }

  /**
   * Reset all ad-hoc patrol scores to 0
   */
  async resetAdhocScores(): Promise<void> {
    await this.fetchAndHandle<{ success: boolean }>('/api/admin/adhoc/patrols/reset', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'X-CSRF-Token': this.csrfToken || '',
      },
    });
  }

  // ============================================================================
  // Scoreboard Methods
  // ============================================================================

  /**
   * Fetch the user's authorized scoreboards (devices)
   */
  async fetchScoreboards(): Promise<api.Scoreboard[]> {
    return this.fetchAndHandle<api.Scoreboard[]>('/api/admin/scoreboards', {
      credentials: 'same-origin',
    });
  }

  /**
   * Send a timer command to a specific scoreboard device
   */
  async sendTimerCommand(deviceCodePrefix: string, command: 'start' | 'pause' | 'resume' | 'reset', duration?: number): Promise<void> {
    const body: Record<string, unknown> = { command };
    if (duration !== undefined) {
      body.duration = duration;
    }
    await this.fetchAndHandle<{ success: boolean }>(`/api/admin/scoreboards/${deviceCodePrefix}/timer`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify(body),
    });
  }

  /**
   * Update a scoreboard's displayed section
   */
  async updateScoreboardSection(deviceCodePrefix: string, sectionId: number): Promise<void> {
    await this.fetchAndHandle<{ success: boolean }>(`/api/admin/scoreboards/${deviceCodePrefix}/section`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify({ sectionId }),
    });
  }

  /**
   * Update patrol scores for a specific section
   * @throws {NetworkError} for network/offline errors (retryable)
   * @throws {ApiError} for API errors
   */
  async updateScores(sectionId: number, updates: model.ScoreDelta[]): Promise<PatrolScoreUpdateResult[]> {
    // Updates already have string patrol IDs matching the API format
    const apiUpdates: api.ScoreUpdate[] = updates.map( (request : model.ScoreDelta) : api.ScoreUpdate => ({
      patrolId: request.patrolId,
      points: request.score
    }));

    const rawResponse = await this.fetchAndHandle<api.UpdateResponse>(`/api/admin/sections/${sectionId}/scores`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify({ updates: apiUpdates }),
    });

    // Convert the response to our internal types with proper date parsing
    return rawResponse.patrols.map( (patrol : api.PatrolResult):PatrolScoreUpdateResult => {
      if (patrol.success) {
        return {
          success: true,
          id: patrol.id,
          name: patrol.name,
          previousScore: patrol.previousScore,
          newScore: patrol.newScore,
        };
      } else if (patrol.isTemporaryError && patrol.retryAfter) {
        return {
          success: false,
          isTemporaryError: true,
          id: patrol.id,
          name: patrol.name,
          newScore: patrol.newScore,
          retryAfter: patrol.retryAfter, // Keep as string, will be parsed by caller
          errorMessage: patrol.errorMessage || 'Temporary error',
        };
      } else {
        return {
          success: false,
          isTemporaryError: false,
          id: patrol.id,
          name: patrol.name,
          newScore: patrol.newScore,
          errorMessage: patrol.errorMessage || 'Update failed',
        };
      }
    });
  }
}

