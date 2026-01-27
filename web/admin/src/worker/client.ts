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

function createPatrolMessageFromStoreData(
    userId: number,
    sectionId: number,
    scores: store.Patrol[],
    uiRevision: number,
    lastError?: string,
    lastErrorTime?: number,
    requestId?: string
) {
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
        lastErrorTime
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
    const message = createPatrolMessageFromStoreData(userId, sectionId, scores, uiRevision, lastError, lastErrorTime);
    return sendMessage(message);
}

/** Send scores to a specific client in response to a request. */
export function sendScoresToClient(
    client: Client,
    userId: number,
    sectionId: number,
    scores: store.Patrol[],
    uiRevision: number,
    lastError?: string,
    lastErrorTime?: number,
    requestId?: string
):void {
    client.postMessage(createPatrolMessageFromStoreData(userId, sectionId, scores, uiRevision, lastError, lastErrorTime, requestId));
}

/** Send a message to a specific client. This is a typesafe wrapper around client.postMessage */
export function send(client: Client, message: messages.WorkerMessage):void {
    client.postMessage(message);
}
