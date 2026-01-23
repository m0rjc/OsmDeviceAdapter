import type {WorkerMessage} from "./messages.ts";

declare let self: ServiceWorkerGlobalScope;

/** Send a message or messages to all clients */
export async function sendMessage(...messages : WorkerMessage[]){
    const clients = await self.clients.matchAll();
    clients.forEach( (client : Client) : void =>
        messages.forEach((message : WorkerMessage) : void =>
            client.postMessage(message)
        )
    );
}