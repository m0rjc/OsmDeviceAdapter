import type {PatrolScore, Section} from "../model.ts";

/** Message sent to the client to ask the user to reauthenticate. */
export type AuthenticationRequiredMessage = {
    type: 'authentication-required';
    /** Correlation ID matching the request that triggered this response */
    requestId?: string;
    loginUrl: string;
}

/** Message sent to the client to ask the user to reauthenticate. */
export function newAuthenticationRequiredMessage(loginUrl: string, requestId?: string): AuthenticationRequiredMessage {
    return { type: 'authentication-required', requestId, loginUrl };
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
    /** Correlation ID matching the request that triggered this response */
    requestId: string;
    userId: number;
    userName: string;
    sections: Section[];
    /** Global revision number for the section list - increments when sections added/removed/changed */
    sectionsListRevision: number;
    /** Last error from profile/section list fetch (undefined if no error) */
    lastError?: string;
    /** Timestamp of last error (milliseconds, undefined if no error) */
    lastErrorTime?: number;
}

export function newUserProfileMessage(
    userId: number,
    userName: string,
    sections: Section[],
    sectionsListRevision: number,
    requestId: string,
    lastError?: string,
    lastErrorTime?: number
) {
    return { type: 'user-profile', requestId, userId, userName, sections, sectionsListRevision, lastError, lastErrorTime };
}

export type SectionListChangeMessage = {
    type: 'section-list-change';
    sections: Section[];
    /** Global revision number for the section list - increments when sections added/removed/changed */
    sectionsListRevision: number;
}

export function newSectionListChangeMessage(sections: Section[], sectionsListRevision: number): SectionListChangeMessage {
    return { type: 'section-list-change', sections, sectionsListRevision };
}

export type PatrolsChangeMessage = {
    type: 'patrols-change';
    /**
     * Correlation ID matching the request that triggered this response.
     * Optional - may be undefined for unsolicited updates (background sync, other clients).
     */
    requestId?: string;
    userId: number;
    sectionId: number;
    scores: PatrolScore[];
    /** UI revision number for this section - increments on each patrol state change */
    uiRevision: number;
    /** Last error from section refresh (undefined if no error) */
    lastError?: string;
    /** Timestamp of last error (milliseconds, undefined if no error) */
    lastErrorTime?: number;
}

/** Union of all messages sent to the client. */
export type WorkerMessage = AuthenticationRequiredMessage | PatrolsChangeMessage | SectionListChangeMessage | UserProfileMessage | ClientIsWrongUserMessage;
