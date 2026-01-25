import { useEffect, useRef } from 'react';
import { useAppDispatch } from '../state/hooks';
import { setUser, clearUser } from '../state/userSlice';
import { setCanonicalSections, setCanonicalPatrols, clearAllData, setGlobalError } from '../state/appSlice';
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
          dispatch(setUser({
            userId: message.userId.toString(),
            userName: message.userName,
          }));
          // Also set sections from the profile message
          dispatch(setCanonicalSections(message.sections));
          break;

        case 'section-list-change':
          // Update sections list
          console.log('[useWorkerBootstrap] Section list changed, sections count:', message.sections.length);
          dispatch(setCanonicalSections(message.sections));
          break;

        case 'patrols-change':
          // Update patrols for a specific section
          console.log('[useWorkerBootstrap] Patrols changed for section:', message.sectionId);
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

        default:
          console.warn('[useWorkerBootstrap] Unhandled message type:', (message as any).type);
      }
    };

    // Request initial profile
    console.log('[useWorkerBootstrap] Requesting profile...');
    worker.sendGetProfileRequest();
  }, [dispatch]);

  return workerRef.current;
}
