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
}

export function newUserProfileMessage(userId: number, userName: string, sections: Section[], requestId: string) {
    return { type: 'user-profile', requestId, userId, userName, sections };
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
    /**
     * Correlation ID matching the request that triggered this response.
     * Optional - may be undefined for unsolicited updates (background sync, other clients).
     */
    requestId?: string;
    userId: number;
    sectionId: number;
    scores: PatrolScore[]
}

export type ServiceErrorMessage = {
    type: 'service-error';
    /** Correlation ID matching the request that triggered this error (optional for general errors) */
    requestId?: string;
    /** User ID - allows routing error to correct user context */
    userId: number;
    /** Section ID - allows routing error to specific section (optional) */
    sectionId?: number;
    /** Patrol ID - allows routing error to specific patrol (optional) */
    patrolId?: string;
    /** The function is what we were trying to do, for example load scores. */
    function: string;
    /** The error message returned by the service. */
    error: string;
}

export function newServiceErrorMessage(
    functionName: string,
    error: string,
    userId: number,
    options?: {
        requestId?: string;
        sectionId?: number;
        patrolId?: string;
    }
): ServiceErrorMessage {
    return {
        type: 'service-error',
        requestId: options?.requestId,
        userId,
        sectionId: options?.sectionId,
        patrolId: options?.patrolId,
        function: functionName,
        error
    };
}

/** Union of all messages sent to the client. */
export type WorkerMessage = AuthenticationRequiredMessage | PatrolsChangeMessage | SectionListChangeMessage | UserProfileMessage | ClientIsWrongUserMessage | ServiceErrorMessage ;
