import type {
  SessionResponse,
  SectionsResponse,
  ScoresResponse,
  UpdateResponse,
  ErrorResponse,
} from './types';
import {
  snapshotToOutbox,
  handleServerResponse,
  markOutboxEntriesFailed,
  deleteOutboxEntries,
  isOnline,
  requestBackgroundSync,
} from './offlineQueue';

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
    super('You are offline. Changes have been saved and will sync when back online.');
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

/**
 * Update scores using the outbox pattern
 *
 * Flow:
 * 1. Snapshot working scores to client outbox (generates idempotency key)
 * 2. Send to server with X-Idempotency-Key header
 * 3. On 200 OK or 202 Accepted: delete from client outbox
 * 4. On network error: keep in client outbox for retry
 * 5. On 4xx error: delete from client outbox (rejected, don't retry)
 *
 * Note: patrolName is not sent - server fetches authoritative name from OSM
 */
export async function updateScores(
  sectionId: number,
  patrolScores: Array<{ patrolId: string; points: number }>,
  csrfToken: string
): Promise<UpdateResponse> {
  // If offline, snapshot to outbox and throw OfflineQueuedError
  if (!isOnline()) {
    await snapshotToOutbox(sectionId, patrolScores);
    await requestBackgroundSync();
    throw new OfflineQueuedError();
  }

  // Snapshot working scores to client outbox (generates idempotency key)
  const { baseKey, entries } = await snapshotToOutbox(sectionId, patrolScores);

  try {
    // Build updates array from outbox entries
    const updates = entries.map(e => ({
      patrolId: e.patrolId,
      points: e.points,
    }));

    const response = await fetch(`/api/admin/sections/${sectionId}/scores`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrfToken,
        'X-Idempotency-Key': baseKey,
        'X-Sync-Mode': 'interactive', // Interactive mode for user-initiated submits
      },
      body: JSON.stringify({ updates }),
    });

    if (response.ok) {
      const data = await response.json();

      // Handle both 200 OK and 202 Accepted
      if (response.status === 200) {
        // Server completed synchronously - delete from client outbox
        await handleServerResponse(baseKey, 'completed');
        return data;
      } else if (response.status === 202) {
        // Server accepted (202) - delete from client outbox, server owns it now
        await handleServerResponse(baseKey, 'accepted');
        return data;
      }

      return data;
    } else if (response.status === 401 || response.status === 403) {
      // Auth error - keep in outbox, will retry after re-login
      await markOutboxEntriesFailed(baseKey, 'Authentication required. Please log in again.');
      throw new ApiError(response.status, 'auth_required', 'Please log in again to sync your changes.');
    } else if (response.status >= 400 && response.status < 500) {
      // Other 4xx error - delete from outbox, don't retry
      await deleteOutboxEntries(baseKey);
      const errorData: ErrorResponse = await response.json();
      throw new ApiError(response.status, errorData.error, errorData.message);
    } else {
      // 5xx error - mark as failed, keep in outbox for retry
      await markOutboxEntriesFailed(baseKey, `Server error: ${response.status}`);
      throw new ApiError(response.status, 'server_error', 'Server error occurred. Will retry.');
    }
  } catch (err) {
    // Network error - keep in outbox for later retry
    if (err instanceof TypeError && err.message.includes('fetch')) {
      await markOutboxEntriesFailed(baseKey, 'Network error. Will retry when online.');
      await requestBackgroundSync();
      throw new OfflineQueuedError();
    }
    throw err;
  }
}

export { ApiError };
