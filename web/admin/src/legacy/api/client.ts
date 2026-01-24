import type {
  SessionResponse,
  SectionsResponse,
  ScoresResponse,
  UpdateRequest,
  UpdateResponse,
  ErrorResponse,
} from './types.ts';
import { queueUpdate, isOnline, requestBackgroundSync } from './offlineQueue.ts';

class ApiError extends Error {
  statusCode: number;
  errorCode: string;

  constructor(
    statusCode: number,
    errorCode: string,
    message: string
  ) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.errorCode = errorCode;
  }
}

/**
 * Error thrown when a request is queued for offline sync
 */
export class OfflineQueuedError extends Error {
  constructor() {
    super('You are offline. Changes have been queued and will sync when back online.');
    this.name = 'OfflineQueuedError';
  }
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let errorData: ErrorResponse;
    try {
      errorData = await response.json();
    } catch {
      throw new ApiError(response.status, 'unknown_error', 'An unexpected error occurred');
    }
    throw new ApiError(response.status, errorData.error, errorData.message);
  }
  return response.json();
}

export async function fetchSession(): Promise<SessionResponse> {
  const response = await fetch('/api/admin/session', {
    credentials: 'same-origin',
  });
  return handleResponse<SessionResponse>(response);
}

export async function fetchSections(): Promise<SectionsResponse> {
  const response = await fetch('/api/admin/sections', {
    credentials: 'same-origin',
  });
  return handleResponse<SectionsResponse>(response);
}

export async function fetchScores(sectionId: number): Promise<ScoresResponse> {
  const response = await fetch(`/api/admin/sections/${sectionId}/scores`, {
    credentials: 'same-origin',
  });
  return handleResponse<ScoresResponse>(response);
}

export async function updateScores(
  sectionId: number,
  updates: UpdateRequest,
  csrfToken: string
): Promise<UpdateResponse> {
  // If offline, queue the update for later sync
  if (!isOnline()) {
    await queueUpdate(sectionId, updates.updates);
    await requestBackgroundSync();
    throw new OfflineQueuedError();
  }

  try {
    const response = await fetch(`/api/admin/sections/${sectionId}/scores`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrfToken,
      },
      body: JSON.stringify(updates),
    });
    return handleResponse<UpdateResponse>(response);
  } catch (err) {
    // Network error - queue for later
    if (err instanceof TypeError && err.message.includes('fetch')) {
      await queueUpdate(sectionId, updates.updates);
      await requestBackgroundSync();
      throw new OfflineQueuedError();
    }
    throw err;
  }
}

export { ApiError };
