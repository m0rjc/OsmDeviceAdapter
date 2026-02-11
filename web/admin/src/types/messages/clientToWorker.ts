import type {ScoreDelta} from "../model.ts";

/**
 * Load the profile for the current user.
 * If the user is logged in, then UserProfileMessage is given.
 * If the user is logged out, then AuthenticationRequiredMessage is given.
 *
 * All clients will receive a SectionListChangeMessage if the section list has
 * changed. This is published after the UserProfileMessage sent to the calling
 * client. The first client will expect both messages because its call will
 * initialize the section list for the first time. The section list is
 * stored in browser storage until explicitly cleared.
 */
export type GetProfileMessage = {
  type: "get-profile";
  /** Correlation ID to match request with response */
  requestId: string;
};

/** Request to refresh the patrol scores from the server. */
export type RefreshRequestMessage = {
  type: "refresh";
  /** Correlation ID to match request with response */
  requestId: string;
  userId: number;
  sectionId: number;
};

export type SubmitScoresMessage = {
    type: "submit-scores";
    /** Correlation ID to match request with response */
    requestId: string;
    userId: number;
    sectionId: number;
    deltas:ScoreDelta[];
}

/** Request to sync pending scores now (respects retry timers and permanent errors) */
export type SyncNowMessage = {
    type: "sync-now";
    /** Correlation ID to match request with response */
    requestId: string;
    userId: number;
    sectionId: number;
};

/** Request to forcefully sync pending scores (clears permanent errors, preserves rate limits) */
export type ForceSyncMessage = {
    type: "force-sync";
    /** Correlation ID to match request with response */
    requestId: string;
    userId: number;
    sectionId: number;
};

export type ClientMessage = RefreshRequestMessage | SubmitScoresMessage | GetProfileMessage | SyncNowMessage | ForceSyncMessage;
