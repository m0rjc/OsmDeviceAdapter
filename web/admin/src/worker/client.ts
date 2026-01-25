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

/** Publish the updated scores to all clients. */
export async function publishScores(userId: number, sectionId: number, scores: store.Patrol[]) {
    const message : messages.PatrolsChangeMessage = {
        type: 'patrols-change',
        userId,
        sectionId,
        scores: scores.map( (s: store.Patrol):model.PatrolScore => ({
            id: s.patrolId,
            name: s.patrolName,
            committedScore: s.committedScore,
            pendingScore: s.pendingScoreDelta
        }))
    }
    return sendMessage(message);
}
