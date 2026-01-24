export type RefreshRequestMessage = {
  type: "refresh";
  sectionId: number;
};

// OSM API uses string IDs (can be empty or negative for special patrols)
export type ScoreDelta = { patrolId: string, score: number };

export type SubmitScoresMessage = {
    type: "submit-scores";
    userId: number;
    sectionId: number;
    deltas:ScoreDelta[];
}

export type ClientMessage = RefreshRequestMessage | SubmitScoresMessage;
