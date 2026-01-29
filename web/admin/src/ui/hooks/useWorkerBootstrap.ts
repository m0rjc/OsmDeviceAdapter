import {useEffect, useRef} from 'react';
import {
    clearUser,
    handlePatrolsChange,
    handleSectionListChange,
    handleUserProfileMessage,
    handleWrongUser,
    setGlobalError,
    useAppDispatch
} from '../state';
import {GetWorker, type Worker} from '../worker';
import type {WorkerMessage} from '../../types/messages';
import {reduceError} from "../../types/reduceError.ts";

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
            dispatch(setGlobalError(reduceError(error, "Unable to initialize worker.")));
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
                    dispatch(handleUserProfileMessage(message));
                    break;

                case 'section-list-change':
                    // Update sections list (will auto-fetch patrols if section is selected)
                    console.log('[useWorkerBootstrap] Section list changed, sections count:', message.sections.length);
                    dispatch(handleSectionListChange(message));
                    break;

                case 'patrols-change':
                    // Update patrols for a specific section
                    console.log('[useWorkerBootstrap] Patrols changed for section:', message.sectionId);
                    dispatch(handlePatrolsChange(message));
                    break;

                case 'wrong-user':
                    // User mismatch - clear state and show error message
                    console.warn('[useWorkerBootstrap] Wrong user detected:', message.requestedUserId, 'vs', message.currentUserId);
                    dispatch(handleWrongUser());
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
