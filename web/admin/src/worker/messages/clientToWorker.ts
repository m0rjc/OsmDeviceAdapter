import type {ScoreDelta} from "../types/model";

/**
 * Load the profile for the given user. This must be the currently logged-in user.
 * If the user is logged in then profile including section list is loaded.
 * If the user is logged out then must-authenticate is given
 * If a differnt user is logged in then wrong-user is given to this client only.
 */
export type GetProfileMessage = {
  type: "get-profile";
  userId: number;
};

/** Request to refresh the patrol scores from the server. */
export type RefreshRequestMessage = {
  type: "refresh";
  userId: number;
  sectionId: number;
};

export type SubmitScoresMessage = {
    type: "submit-scores";
    userId: number;
    sectionId: number;
    deltas:ScoreDelta[];
}

export type ClientMessage = RefreshRequestMessage | SubmitScoresMessage | GetProfileMessage;
