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
