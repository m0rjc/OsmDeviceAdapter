import type {Section} from "../../api";

/** Message sent to the client to ask the user to reauthenticate. */
export type AuthenticationRequiredMessage = {
    type: 'authentication-required';
    loginUrl: string;
}

/** Message sent to the client to ask the user to reauthenticate. */
export function newAuthenticationRequiredMessage(loginUrl: string): AuthenticationRequiredMessage {
    return { type: 'authentication-required', loginUrl };
}

/** Message sent to the client to indicate that the user is authenticated. */
export type AuthenticatedMessage = {
    type: 'authenticated';
    userId: number;
    userName: string;
}

export function newAuthenticatedMessage(userId: number, userName: string): AuthenticatedMessage {
    return {
        type: 'authenticated',
        userId,
        userName
    };
}

export type SectionListChangeMessage = {
    type: 'section-list-change';
    sections: Section[];
}

export function newSectionListChangeMessage(sections: Section[]): SectionListChangeMessage {
    return { type: 'section-list-change', sections };
}

export type PatrolScore = {
    // Patrol ID - unique within a section (OSM API uses strings - can be empty or negative for special patrols)
    id: string;
    // Patrol name as displayed in the UI
    name: string;
    // Score held by the server
    committedScore: number;
    // Score held in the local database yet to be synced to the server.
    pendingScore: number;
}

export type PatrolsChangeMessage = {
    type: 'patrols-change';
    userId: number;
    sectionId: number;
    scores: PatrolScore[]
}

/** Union of all messages sent to the client. */
export type WorkerMessage = AuthenticationRequiredMessage | AuthenticatedMessage | PatrolsChangeMessage | SectionListChangeMessage ;
