import * as messages from '../types/messages'

export * from '../types/messages/workerToClient'

/**
 * Worker interface for the UI client.
 * This interface represents the local session state and access to the backend server.
 * The system is message-based to support Service Workers. The client sends a message using
 * methods on this interface. The worker responds asynchronously by posting a message to the
 * client. The worker may send unsolicited messages, for example, in response to a background
 * sync or actions of another client if multiple clients are open.
 */
export interface Worker {
    onMessage: (message: messages.WorkerMessage) => void;

    /**
     * Load the profile for the current user.
     * If the user is logged in, then a UserProfileMessage is given.
     * If the user is logged out, then AuthenticationRequiredMessage is given.
     *
     * All clients will receive a SectionListChangeMessage if the section list has
     * changed. This is published after the UserProfileMessage sent to the calling
     * client. The first client will expect both messages because its call will
     * initialize the section list for the first time. The section list is
     * stored in browser storage until explicitly cleared.
     *
     * @returns requestId for correlating the response
     */
    sendGetProfileRequest(): string;

    /**
     * Request to refresh the patrol scores from the server.
     * This can also be used to perform the initial load of scores when the UI switches section.
     *
     * The worker will respond asynchronously with a PatrolsChangeMessage.
     * This may be cached if the server is unreachable. A ServiceErrorMessage will be sent in this case.
     *
     * If the user is not logged in, then AuthenticationRequiredMessage is given.
     * If the wrong user is logged in, then WrongUserMessage is given.
     *
     * @returns requestId for correlating the response
     */
    sendRefreshRequest(userId: number, sectionId: number): string;

    /**
     * Submit score changes to the worker for syncing to the server.
     * This is a fire-and-forget operation with optimistic updates.
     *
     * The worker will:
     * 1. Immediately add pending points to IndexedDB
     * 2. Broadcast an optimistic update to all clients (PatrolsChangeMessage)
     * 3. Attempt to sync to server if online
     * 4. Handle retries/errors automatically via background sync
     *
     * @param userId The user ID
     * @param sectionId The section ID
     * @param deltas Array of score changes to submit
     * @returns requestId for correlating any error responses
     */
    sendSubmitScoresRequest(userId: number, sectionId: number, deltas: Array<{
        patrolId: string,
        score: number
    }>): string;
}

/**
 * Worker factory function type.
 * Can be overridden for testing via setWorkerFactory().
 */
type WorkerFactory = () => Promise<Worker>;

/**
 * Default worker factory - returns real ServiceWorker-based implementation.
 * Waits for service worker to be ready before returning.
 */
const defaultWorkerFactory: WorkerFactory = async () => {
    // Check if Service Worker API is supported
    if (!navigator.serviceWorker) {
        throw new Error("Service Worker API not supported");
    }

    // Wait for service worker to be ready (registered and activated)
    await navigator.serviceWorker.ready;

    // Controller should now be available
    if (!navigator.serviceWorker.controller) {
        // This can happen if the page was loaded before SW was registered
        // Reload the page to let the SW take control
        console.warn('[GetWorker] Service worker ready but no controller, reloading page...');
        window.location.reload();
        // Return a dummy worker that will never be used (page is reloading)
        throw new Error("Reloading page to activate service worker");
    }

    return new WorkerService(navigator.serviceWorker);
};

/**
 * Current worker factory (can be overridden for testing).
 */
let currentWorkerFactory: WorkerFactory = defaultWorkerFactory;

/**
 * Get a worker instance using the current factory.
 * In production, waits for ServiceWorker to be ready and returns it.
 * In tests, can return a mock via setWorkerFactory().
 *
 * @returns Promise that resolves to a Worker instance
 */
export function GetWorker(): Promise<Worker> {
    return currentWorkerFactory();
}

/**
 * Override the worker factory for testing.
 * Call with no arguments to reset to default factory.
 *
 * @example
 * ```typescript
 * // In test setup
 * const mockWorker = { onMessage: jest.fn(), sendGetProfileRequest: jest.fn() };
 * setWorkerFactory(async () => mockWorker);
 *
 * // In test teardown
 * setWorkerFactory(); // Reset to default
 * ```
 */
export function setWorkerFactory(factory?: WorkerFactory): void {
    currentWorkerFactory = factory || defaultWorkerFactory;
}

/**
 * WorkerService represents the Service Worker API from the point of view of the UI client.
 */
class WorkerService implements Worker {
    private sw: ServiceWorkerContainer;

    /**
     * Event handler for messages from the worker.
     */
    public onMessage: (message: messages.WorkerMessage) => void = () => {
    };

    constructor(sw: ServiceWorkerContainer) {
        this.sw = sw;
        sw.addEventListener('message', (event) => {
            console.debug(`[WorkerMessage] Message Received`, {
                messageType: event.data.type,
                requestId: event.data.requestId,
            });
            this.onMessage(event.data);
        });
    }

    private sendMessage(message: messages.ClientMessage) {
        if (this.sw.controller) {
            console.debug(`[SendMessage] Sending message to controller:`, {
                messageType: message.type,
                requestId: message.requestId
            });
            this.sw.controller.postMessage(message);
        } else {
            console.error(`[SendMessage]  No controller, unable to send message`, {
                messageType: message.type,
                requestId: message.requestId
            });
        }
    }

    public sendGetProfileRequest(): string {
        const requestId = crypto.randomUUID();
        this.sendMessage({type: "get-profile", requestId});
        return requestId;
    }

    public sendRefreshRequest(userId: number, sectionId: number): string {
        const requestId = crypto.randomUUID();
        this.sendMessage({type: "refresh", requestId, userId, sectionId});
        return requestId;
    }

    public sendSubmitScoresRequest(userId: number, sectionId: number, deltas: Array<{
        patrolId: string,
        score: number
    }>): string {
        const requestId = crypto.randomUUID();
        this.sendMessage({type: "submit-scores", requestId, userId, sectionId, deltas});
        return requestId;
    }
}