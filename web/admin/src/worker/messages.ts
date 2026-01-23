/** Message sent to the client to ask the user to reauthenticate. */
export type AuthenticationRequiredMessage = {
    type: 'authentication-required';
}

/** Message sent to the client to ask the user to reauthenticate. */
export function newAuthenticationRequiredMessage(): AuthenticationRequiredMessage {
    return { type: 'authentication-required' };
}

/** Message sent to the client to indicate that the user is authenticated. */
export type AuthenticatedMessage = {
    type: 'authenticated';
}

export function newAuthenticatedMessage(): AuthenticatedMessage {
    return { type: 'authenticated' };
}

/** Union of all messages sent to the client. */
export type WorkerMessage = AuthenticationRequiredMessage | AuthenticatedMessage;
