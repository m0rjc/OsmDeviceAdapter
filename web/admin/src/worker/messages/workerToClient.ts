import type {PatrolScore, Section} from "../types/model.ts";

/** Message sent to the client to ask the user to reauthenticate. */
export type AuthenticationRequiredMessage = {
    type: 'authentication-required';
    loginUrl: string;
}

/** Message sent to the client to ask the user to reauthenticate. */
export function newAuthenticationRequiredMessage(loginUrl: string): AuthenticationRequiredMessage {
    return { type: 'authentication-required', loginUrl };
}

/**
 * Message sent to the client to indicate that the user requested by the client is not the currently logged in user.
 * This could be a client open in a tab while another tab has logged out and logged in again as a different user.
 */
export type ClientIsWrongUserMessage = {
    type: 'wrong-user';
    requestedUserId: number;
    currentUserId: number;
}

export function newWrongUserMessage(requestedUserId: number, currentUserId: number): ClientIsWrongUserMessage {
    return { type: 'wrong-user', requestedUserId, currentUserId };
}

export type UserProfileMessage = {
    type: 'user-profile';
    userId: number;
    userName: string;
    sections: Section[];
}

export function newUserProfileMessage(userId: number, userName: string, sections: Section[]) {
    return { type: 'user-profile', userId, userName, sections };
}

export type SectionListChangeMessage = {
    type: 'section-list-change';
    sections: Section[];
}

export function newSectionListChangeMessage(sections: Section[]): SectionListChangeMessage {
    return { type: 'section-list-change', sections };
}

export type PatrolsChangeMessage = {
    type: 'patrols-change';
    userId: number;
    sectionId: number;
    scores: PatrolScore[]
}

/** Union of all messages sent to the client. */
export type WorkerMessage = AuthenticationRequiredMessage | PatrolsChangeMessage | SectionListChangeMessage | UserProfileMessage | ClientIsWrongUserMessage ;
