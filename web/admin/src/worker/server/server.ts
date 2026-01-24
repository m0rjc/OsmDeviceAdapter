import * as api from './types';
import type {SessionResponse} from "./types";

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

export type SectionsResponse = api.SectionsResponse;
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

export type ScoreUpdate = {
  patrolId: string;  // OSM API uses string IDs (can be empty or negative for special patrols)
  points: number;
}

/**
 * OsmAdapterApiService represents a connection to the OSMAdapter server.
 * This service is designed to be used within a service worker context.
 * Service workers are stateless, so the pattern is:
 *
 * 1. Construct this service.
 * 2. Fetch the current session with fetchSession().
 * 3. Check that the session is valid with isAuthenticated. If not then ask the user to login
 * 4. Use the other methods to interact with the server.
 */
export class OsmAdapterApiService {
  private currentSession: SessionResponse | null = null;

  private get csrfToken(): string | null {
    return this.currentSession?.csrfToken ?? null;
  }

  /** Is the user authenticated? Only valid after a successful fetchSession() call. */
  public get isAuthenticated(): boolean {
    return this.currentSession?.authenticated ?? false;
  }

  /** The current user if authenticated, otherwise null. */
  public get user(): OsmUser | null {
    return this.currentSession?.user ?? null;
  }

  /** The current OSM user ID if authenticated, otherwise null. */
  public get userId() : number | null {
    return this.user?.osmUserId ?? null;
  }

  /**
   * Handle API responses, parsing JSON and throwing errors as needed
   * @throws {ApiError} for other API errors
   */
  private async handleResponse<T>(response: Response): Promise<T> {
    if (!response.ok) {
      // Handle authentication errors
      if (response.status === 401 || response.status === 403) {
        this.currentSession = null;
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
   */
  async fetchSession(): Promise<SessionStatus> {
    const response = await fetch('/api/admin/session', {
      credentials: 'same-origin',
    });
    this.currentSession = await this.handleResponse<api.SessionResponse>(response);

    if(this.currentSession.authenticated){
      return {
        isAuthenticated: true,
        userId: this.currentSession.user?.osmUserId!,
        userName: this.currentSession.user?.name!
      }
    }
    return {
      isAuthenticated: false
    }
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
      this.currentSession = null;
      return true;
    }

    throw new Error(`Logout failed with status ${response.status}`);
  }

  /**
   * Fetch the list of sections the user has access to
   */
  async fetchSections(): Promise<SectionsResponse> {
    const response = await fetch('/api/admin/sections', {
      credentials: 'same-origin',
    });
    return this.handleResponse<api.SectionsResponse>(response);
  }

  /**
   * Fetch patrol scores for a specific section
   */
  async fetchScores(sectionId: number): Promise<PatrolScore[]> {
    const response = await fetch(`/api/admin/sections/${sectionId}/scores`, {
      credentials: 'same-origin',
    });
    const raw: api.ScoresResponse = await this.handleResponse<api.ScoresResponse>(response);
    return raw.patrols;
  }

  /**
   * Update patrol scores for a specific section
   * @throws {UnauthenticatedError} if not authenticated or CSRF token is not available
   * @throws {ApiError} if the update fails
   */
  async updateScores(sectionId: number, updates: ScoreUpdate[]): Promise<PatrolScoreUpdateResult[]> {
    // Updates already have string patrol IDs matching the API format
    const apiUpdates: api.ScoreUpdate[] = updates;

    const response = await fetch(`/api/admin/sections/${sectionId}/scores`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': this.csrfToken || '',
      },
      body: JSON.stringify(apiUpdates),
    });

    const rawResponse = await this.handleResponse<api.UpdateResponse>(response);

    // Convert the response to our internal types with proper date parsing
    return rawResponse.patrols.map(patrol => {
      if (patrol.success) {
        return {
          success: true,
          id: patrol.id,
          name: patrol.name,
          previousScore: patrol.previousScore,
          newScore: patrol.newScore,
        } as PatrolScoreUpdateResultSuccess;
      } else if (patrol.isTemporaryError && patrol.retryAfter) {
        return {
          success: false,
          isTemporaryError: true,
          id: patrol.id,
          name: patrol.name,
          newScore: patrol.newScore,
          retryAfter: patrol.retryAfter, // Keep as string, will be parsed by caller
          errorMessage: patrol.errorMessage || 'Temporary error',
        } as PatrolScoreUpdateResultTemporaryError;
      } else {
        return {
          success: false,
          isTemporaryError: false,
          id: patrol.id,
          name: patrol.name,
          newScore: patrol.newScore,
          errorMessage: patrol.errorMessage || 'Update failed',
        } as PatrolScoreUpdateResultError;
      }
    });
  }
}

