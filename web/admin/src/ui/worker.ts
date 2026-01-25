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
     */
    sendGetProfileRequest():void;
}

/**
 * Worker factory function type.
 * Can be overridden for testing via setWorkerFactory().
 */
type WorkerFactory = () => Worker;

/**
 * Default worker factory - returns real ServiceWorker-based implementation.
 */
const defaultWorkerFactory: WorkerFactory = () => {
    if (navigator.serviceWorker?.controller) {
        return new WorkerService(navigator.serviceWorker);
    }
    // We could fallback to an in-memory implementation if needed.
    // So far this has only been run on clients that support the Service Worker API.
    throw new Error("Service Worker API not supported");
};

/**
 * Current worker factory (can be overridden for testing).
 */
let currentWorkerFactory: WorkerFactory = defaultWorkerFactory;

/**
 * Get a worker instance using the current factory.
 * In production, returns a ServiceWorker-based implementation.
 * In tests, can return a mock via setWorkerFactory().
 */
export function GetWorker(): Worker {
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
 * setWorkerFactory(() => mockWorker);
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
    private sw : ServiceWorkerContainer;

    /**
     * Event handler for messages from the worker.
     */
    public onMessage: (message: messages.WorkerMessage) => void = () => {};

    constructor(sw : ServiceWorkerContainer) {
        this.sw = sw;
        sw.addEventListener('message', (event) => {
            this.onMessage(event.data);
        });
    }
    
    private sendMessage(message: messages.ClientMessage) {
        this.sw.controller?.postMessage(message);
    }

    public sendGetProfileRequest() {
        this.sendMessage({type: "get-profile"});
    }
}