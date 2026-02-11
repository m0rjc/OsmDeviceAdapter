import * as messages from "../types/messages"
import * as model from "../types/model"
import * as store from "./store/store"

declare let self: ServiceWorkerGlobalScope;

/** Send a message or messages to all clients */
export async function sendMessage(...messages : messages.WorkerMessage[]){
    const clients = await self.clients.matchAll();
    clients.forEach( (client : Client) : void =>
        messages.forEach((message : messages.WorkerMessage) : void =>
            client.postMessage(message)
        )
    );
}

async function createPatrolMessageFromStoreData(
    userId: number,
    sectionId: number,
    scores: store.Patrol[],
    uiRevision: number,
    lastError?: string,
    lastErrorTime?: number,
    requestId?: string
): Promise<messages.PatrolsChangeMessage> {
    // Calculate sync timing metadata
    const now = Date.now();
    let pendingCount = 0;
    let readyCount = 0;
    let nextRetryTime: number | undefined = undefined;
    let minRetryTime = Infinity;

    for (const patrol of scores) {
        if (patrol.pendingScoreDelta !== 0) {
            pendingCount++;

            // Count ready entries: not locked, retry time passed, not permanent error
            if (patrol.lockTimeout <= now && patrol.retryAfter <= now && patrol.retryAfter >= 0) {
                readyCount++;
            }

            // Track soonest retry time for future retries
            if (patrol.retryAfter > now && patrol.retryAfter >= 0) {
                minRetryTime = Math.min(minRetryTime, patrol.retryAfter);
            }
        }
    }

    if (minRetryTime !== Infinity) {
        nextRetryTime = minRetryTime;
    }

    // Check if section sync lock is held
    const patrolStore = await store.OpenPatrolPointsStore(userId);
    let syncInProgress = false;
    try {
        // Get section metadata to check lock status
        const sections = await patrolStore.getSections();
        const section = sections.find(s => s.id === sectionId);
        syncInProgress = section ? section.syncLockTimeout > now : false;
    } finally {
        patrolStore.close();
    }

    const message: messages.PatrolsChangeMessage = {
        type: 'patrols-change',
        requestId,
        userId,
        sectionId,
        scores: scores.map((s: store.Patrol): model.PatrolScore => ({
            id: s.patrolId,
            name: s.patrolName,
            committedScore: s.committedScore,
            pendingScore: s.pendingScoreDelta,
            retryAfter: s.retryAfter,
            errorMessage: s.errorMessage
        })),
        uiRevision,
        lastError,
        lastErrorTime,
        nextRetryTime,
        pendingCount,
        readyCount,
        syncInProgress
    }
    return message;
}

/** Publish the updated scores to all clients (unsolicited update, no requestId). */
export async function publishScores(
    userId: number,
    sectionId: number,
    scores: store.Patrol[],
    uiRevision: number,
    lastError?: string,
    lastErrorTime?: number
) {
    const message = await createPatrolMessageFromStoreData(userId, sectionId, scores, uiRevision, lastError, lastErrorTime);
    return sendMessage(message);
}

/** Send scores to a specific client in response to a request. */
export async function sendScoresToClient(
    client: Client,
    userId: number,
    sectionId: number,
    scores: store.Patrol[],
    uiRevision: number,
    lastError?: string,
    lastErrorTime?: number,
    requestId?: string
):Promise<void> {
    const message = await createPatrolMessageFromStoreData(userId, sectionId, scores, uiRevision, lastError, lastErrorTime, requestId);
    client.postMessage(message);
}

/** Send a message to a specific client. This is a typesafe wrapper around client.postMessage */
export function send(client: Client, message: messages.WorkerMessage):void {
    client.postMessage(message);
}
