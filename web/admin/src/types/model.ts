// OSM API uses string IDs (can be empty or negative for special patrols)
export type ScoreDelta = { patrolId: string, score: number };

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

export type Section = {
    id: number;
    name: string;
    groupName: string;
}