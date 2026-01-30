import {useEffect, useRef} from 'react';
import {
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
import {setUnauthenticated} from "../state/userSlice.ts";

/**
 * Bootstrap hook to initialize worker communication and load initial user state.
 *
 * This hook:
 * 1. Waits for service worker to be ready
 * 2. Gets the worker instance (async)
 * 3. Sets up message handling from the worker
 * 4. Sends get-profile request to load initial state
 * 5. Handles worker responses to update Redux state
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

        // Async initialization function
        async function initializeWorker() {
            try {
                // Wait for worker to be ready
                console.log('[useWorkerBootstrap] Waiting for service worker...');
                const worker = await GetWorker();
                workerRef.current = worker;
                console.log('[useWorkerBootstrap] Service worker ready');

                // Set up message handler
                worker.onMessage = (message: WorkerMessage) => {
                    console.log('[useWorkerBootstrap] Received message:', message.type);

                    switch (message.type) {
                        case 'authentication-required':
                            // Clear user state - let LoginPage component handle the redirect
                            console.log('[useWorkerBootstrap] Authentication required, showing login page');
                            dispatch(setUnauthenticated());
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
            } catch (error) {
                console.error('[useWorkerBootstrap] Failed to initialize worker:', error);
                dispatch(setGlobalError(reduceError(error, "Unable to initialize worker.")));
            }
        }

        // Start initialization
        initializeWorker();
    }, [dispatch]);

    return workerRef.current;
}
