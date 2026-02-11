/**
 * ScoreDelta represents a score change to be applied to a patrol.
 * OSM API uses string IDs (can be empty or negative for special patrols).
 */
export type ScoreDelta = { patrolId: string, score: number };

/**
 * PatrolScore represents the score state for a patrol, combining server state
 * (committedScore) with local pending changes (pendingScore) and sync error state.
 */
export type PatrolScore = {
    // Patrol ID - unique within a section (OSM API uses strings - can be empty or negative for special patrols)
    id: string;
    // Patrol name as displayed in the UI
    name: string;
    // Score held by the server (last successful sync)
    committedScore: number;
    // Score changes held locally yet to be synced to the server
    pendingScore: number;
    // Retry timestamp: -1 = permanent error (user must acknowledge), 0 = can retry now, >0 = retry after this timestamp (ms)
    retryAfter?: number;
    // Error message from failed sync attempt (present when retryAfter is set)
    errorMessage?: string;
}

/**
 * Section represents a section of patrols.
 * Note: Section-level error state and versioning (uiRevision) are passed
 * at the message level (in PatrolsChangeMessage), not on Section objects.
 */
export type Section = {
    id: number;
    name: string;
    groupName: string;
}