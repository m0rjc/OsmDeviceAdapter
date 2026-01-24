import * as api from './types';
import * as model from '../types/model';
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

  public isOffline(): boolean {
    return !navigator.onLine;
  }

  /**
   * Perform a fetch request and handle the response
   * @throws {NetworkError} for network/offline errors (retryable)
   * @throws {ApiError} for API errors
   */
  private async fetchAndHandle<T>(url: string, init?: RequestInit): Promise<T> {
    let response : Response;
    try {
      response = await fetch(url, init);
    } catch (error) {
      throw new NetworkError('Network request failed. Check your internet connection.', error);
    }

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
   * @throws {NetworkError} for network/offline errors
   * @throws {ApiError} for API errors
   */
  async fetchSession(): Promise<SessionStatus> {
    this.currentSession = await this.fetchAndHandle<api.SessionResponse>('/api/admin/session', {
      credentials: 'same-origin',
    });

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
      body: JSON.stringify(apiUpdates),
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

