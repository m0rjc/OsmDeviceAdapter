import { useEffect, useRef } from 'react';
import { useAppDispatch } from '../state/hooks';
import { setUser, clearUser } from '../state/userSlice';
import { setCanonicalPatrols, clearAllData, setGlobalError, setPatrolsError } from '../state/appSlice';
import { showErrorDialog } from '../state/dialogSlice';
import { removePendingRequest, addPendingRequest, clearAllPendingRequests } from '../state/pendingRequestsSlice';
import { updateSectionsList } from '../state/workerThunks';
import { store } from '../state/store';
import { GetWorker, type Worker } from '../worker';
import type { WorkerMessage } from '../../types/messages';

/**
 * Bootstrap hook to initialize worker communication and load initial user state.
 *
 * This hook:
 * 1. Gets the worker instance
 * 2. Sets up message handling from the worker
 * 3. Sends get-profile request to load initial state
 * 4. Handles worker responses to update Redux state
 *
 * Returns the worker instance for use by other components.
 */
export function useWorkerBootstrap(): Worker | null {
  const dispatch = useAppDispatch();
  const workerRef = useRef<Worker | null>(null);
  const hasBootstrappedRef = useRef(false);

  useEffect(() => {
    // Only bootstrap once
    if (hasBootstrappedRef.current) {
      return;
    }
    hasBootstrappedRef.current = true;

    // Get worker instance
    let worker: Worker;
    try {
      worker = GetWorker();
      workerRef.current = worker;
    } catch (error) {
      console.error('[useWorkerBootstrap] Failed to get worker:', error);
      // FIXME: Show error message
      return;
    }

    // Set up message handler
    worker.onMessage = (message: WorkerMessage) => {
      console.log('[useWorkerBootstrap] Received message:', message.type);

      switch (message.type) {
        case 'authentication-required':
          // Clear user state and redirect to login
          console.log('[useWorkerBootstrap] Authentication required, redirecting to:', message.loginUrl);
          dispatch(clearUser());
          window.location.href = message.loginUrl;
          break;

        case 'user-profile':
          // Set user profile in Redux
          console.log('[useWorkerBootstrap] User profile received:', message.userId, message.userName);

          // Remove pending request if this was a response to our request
          if (message.requestId) {
            dispatch(removePendingRequest(message.requestId));
          }

          dispatch(setUser({
            userId: message.userId.toString(),
            userName: message.userName,
          }));
          // Update sections list (will auto-fetch patrols if section is selected)
          dispatch(updateSectionsList(message.sections));
          break;

        case 'section-list-change':
          // Update sections list (will auto-fetch patrols if section is selected)
          console.log('[useWorkerBootstrap] Section list changed, sections count:', message.sections.length);
          dispatch(updateSectionsList(message.sections));
          break;

        case 'patrols-change':
          // Update patrols for a specific section
          console.log('[useWorkerBootstrap] Patrols changed for section:', message.sectionId);

          // If this is a response to a tracked request, remove it from pending
          if (message.requestId) {
            const pendingRequest = store.getState().pendingRequests.requests[message.requestId];
            if (pendingRequest) {
              console.log('[useWorkerBootstrap] Completing pending request:', message.requestId);
              dispatch(removePendingRequest(message.requestId));
            }
          }

          // Update patrol data (sets loading state to 'ready')
          dispatch(setCanonicalPatrols({
            sectionId: message.sectionId,
            patrols: message.scores,
          }));
          break;

        case 'wrong-user':
          // User mismatch - clear state and show error message
          console.warn('[useWorkerBootstrap] Wrong user detected:', message.requestedUserId, 'vs', message.currentUserId);
          dispatch(clearUser());
          dispatch(clearAllData());
          dispatch(setGlobalError('You have logged out or changed accounts in another tab. Please reload this page.'));
          break;

        case 'service-error':
          // Service error - use context (sectionId) for routing, requestId for cleanup
          console.error('[useWorkerBootstrap] Service error:', message.function, message.error, {
            userId: message.userId,
            sectionId: message.sectionId,
            patrolId: message.patrolId,
            requestId: message.requestId,
          });

          // If this is a response to a tracked request, remove it from pending
          // Note: Worker may send BOTH PatrolsChangeMessage (cached) AND ServiceErrorMessage (refresh failed)
          // in that order. If PatrolsChangeMessage arrived first, pending request is already removed,
          // so we won't find it here. This is correct - data is loaded (from cache), just stale.
          if (message.requestId) {
            const pendingRequest = store.getState().pendingRequests.requests[message.requestId];
            if (pendingRequest) {
              console.log('[useWorkerBootstrap] Removing pending request:', message.requestId, pendingRequest.type);
              dispatch(removePendingRequest(message.requestId));
            }
          }

          // Route error to specific section if context provided
          // This works for both solicited (with requestId) and unsolicited (background) errors
          if (message.sectionId) {
            dispatch(setPatrolsError({
              sectionId: message.sectionId,
              error: message.error,
            }));
          }

          // TODO: If patrolId is provided, set error state on specific patrol (future enhancement)

          // Always show error dialog for user feedback (warning if data was cached)
          dispatch(showErrorDialog({
            title: message.function,
            message: message.error,
          }));
          break;

        default:
          console.warn('[useWorkerBootstrap] Unhandled message type:', (message as any).type);
      }
    };

    // Request initial profile
    console.log('[useWorkerBootstrap] Requesting profile...');
    const requestId = worker.sendGetProfileRequest();

    // Track the pending request
    dispatch(addPendingRequest({
      requestId,
      type: 'get-profile',
      timestamp: Date.now(),
    }));
  }, [dispatch]);

  return workerRef.current;
}
