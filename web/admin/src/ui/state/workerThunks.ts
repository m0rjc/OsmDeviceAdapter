import {createAsyncThunk} from '@reduxjs/toolkit';
import {
    GetWorker,
    type PatrolsChangeMessage,
    type SectionListChangeMessage,
    type UserProfileMessage,
    type Worker
} from '../worker';
import {clearUser, setUser} from './userSlice';
import * as uiSlice from './uiSlice';
import type {RootState} from './rootReducer';
import {selectChangesForCurrentSection, selectSelectedSection} from "./rootReducer";
import {
    setCanonicalPatrols,
    setCanonicalSectionList,
    setSectionError,
    setSectionState,
    type UISection
} from "./patrolsSlice.ts";
import {setGlobalError, showErrorDialog} from "./dialogSlice.ts";

export const handleUserProfileMessage = createAsyncThunk<
    void,
    UserProfileMessage,
    { state: RootState }>(
    'worker/handleUserProfileMessage',
    async (message: UserProfileMessage, {dispatch, getState}) => {
        dispatch(setUser(message));

        const currentVersion = getState().patrols.sectionIdListVersion;
        if (currentVersion >= message.sectionsListRevision) {
            console.warn('[handleUserProfileMessage] Ignoring outdated profile message:', message.sectionsListRevision, currentVersion);
            return;
        }
        dispatch(setCanonicalSectionList({version: message.sectionsListRevision, sections: message.sections}));

        // Load patrols for the selected section if we have one
        const state: RootState = getState() as RootState;
        const selectedSection: UISection | null = selectSelectedSection(state);
        if (selectedSection?.state === 'uninitialized') {
            dispatch(fetchPatrolScoresForSection(selectedSection.id));
        }

        // Show an error dialog if we have an error
        if (message.lastError) {
            dispatch(showErrorDialog({title: 'Error fetching section list', message: message.lastError}))
        }
    }
);


/**
 * Respond to a section list change message from the worker.
 * We can only respond if we have a current user. We must ignore an unsolicited message
 * that arrives before we have loaded the current user profile.
 */
export const handleSectionListChange = createAsyncThunk<
    void,
    SectionListChangeMessage,
    { state: RootState }
>(
    'worker/handleSectionListChange',
    async (message, {dispatch, getState}) => {
        var state = getState();
        const userId = state.user.userId;
        if (userId == null) {
            return;
        }
        if (message.userId !== userId) {
            console.warn('Message received for unexpected userID');
            return;
        }
        if (message.sectionsListRevision <= state.patrols.sectionIdListVersion) {
            // We can see double messages if the worker broadcasts a list change message in response
            // to first load. So we ignore this.
            return;
        }
        dispatch(setCanonicalSectionList({version: message.sectionsListRevision, sections: message.sections}));

        // Check if we need to fetch patrols for the selected section.
        // We don't expect this in an unsolicited message.
        // TODO: Put up a dialog explaining why the patrol list has changed under their feet (patrol disappeared).
        state = getState();
        const selectedSection: UISection | null = selectSelectedSection(state);
        if (selectedSection?.state === 'uninitialized') {
            dispatch(fetchPatrolScoresForSection(selectedSection.id));
        }
    }
);


/**
 * Thunk to select a section and auto-fetch patrols if needed.
 *
 * This thunk:
 * 1. Updates the selected section ID
 * 2. Checks if patrols are loaded for this section
 * 3. If not loaded, triggers patrol fetch
 *
 * Use this instead of directly dispatching selectSection when the user
 * manually selects a section from the UI.
 *
 * @param sectionId - The section ID to select
 */
export const setSelectedSection = createAsyncThunk<
    void,
    number,
    { state: RootState }
>(
    'worker/selectSection',
    async (sectionId, {dispatch, getState}) => {
        // Update the selected section
        dispatch(uiSlice.setSelectedSectionId(sectionId));

        // Check if we need to fetch patrols for this section
        const state = getState();
        const selectedSection = selectSelectedSection(state);
        if (selectedSection?.state === 'uninitialized') {
            console.log('[selectSection] Auto-fetching patrols for section:', sectionId);
            dispatch(fetchPatrolScoresForSection(sectionId));
        }
    }
);

/**
 * Thunk to fetch patrol scores from the worker.
 *
 * This thunk:
 * 1. Generates a requestId and tracks the pending request
 * 2. Sets the section's loading state to 'loading'
 * 3. Sends refresh request to worker
 * 4. Returns immediately (response handled in useWorkerBootstrap)
 *
 * The worker will respond asynchronously with either:
 * - PatrolsChangeMessage (success) - updates patrols and sets state to 'ready'
 * - ServiceErrorMessage (failure) - sets state to 'error'
 *
 * @param sectionId - The section ID to fetch scores for
 */
const fetchPatrolScoresForSection = createAsyncThunk<
    void,
    number,
    { state: RootState }
>(
    'worker/fetchPatrolScores',
    async (sectionId, {getState, dispatch}) => {
        const state = getState();
        const userId = state.user.userId;

        if (!userId) {
            // TODO: Error dialog
            throw new Error('User not authenticated');
        }

        dispatch(setSectionState({sectionId, stateName: 'loading'}));

        const worker: Worker = GetWorker();
        worker.sendRefreshRequest(userId, sectionId);


        // Note: This thunk completes immediately after sending the message.
        // The actual response (PatrolsChangeMessage or ServiceErrorMessage)
        // will arrive asynchronously through the worker's onMessage handler.
    }
);

export const handlePatrolsChange = createAsyncThunk<
    void,
    PatrolsChangeMessage,
    { state: RootState }
>(
    'worker/handlePatrolsChange',
    async (message: PatrolsChangeMessage, {dispatch, getState}) => {
        const state: RootState = getState();
        const userId: number | null = state.user.userId;

        if (userId !== message.userId) {
            console.warn('Message received for unexpected userID');
            return;
        }

        dispatch(setCanonicalPatrols({
            version: message.uiRevision,
            patrols: message.scores,
            sectionId: message.sectionId
        }));

        if (message.lastError) {
            dispatch(showErrorDialog({title: 'Error fetching patrol scores', message: message.lastError}))
            dispatch(setSectionError({
                sectionId: message.sectionId,
                error: message.lastError,
                version: message.uiRevision
            }));
        }
    }
)

/**
 * Thunk to handle wrong user scenarios.
 *
 * This thunk handles the case where worker messages arrive for a different user
 * (e.g., user logged out and back in as different user in another tab).
 *
 * It:
 * 1. Clears the user state
 * 2. Clears all application data
 * 3. Clears all pending requests
 * 4. Shows a global error message prompting the user to reload
 *
 * This is a centralized action to maintain DRY when handling user mismatches
 * in different message handlers.
 */
export const handleWrongUser = createAsyncThunk<void, void, { state: RootState }>(
    'worker/handleWrongUser',
    async (_, {dispatch}) => {
        dispatch(clearUser());
        // dispatch(clearAllData());
        // dispatch(clearAllPendingRequests());
        dispatch(setGlobalError('You have logged out or changed accounts in another tab. Please reload this page.'));
    }
);

/**
 * Thunk to submit score changes to the worker.
 *
 * This thunk:
 * 1. Collects user changes from state
 * 2. Sends submit-scores message to worker
 * 3. Clears user entries from UI state (optimistic update)
 *
 * The worker will:
 * - Add pending points to IndexedDB
 * - Broadcast optimistic update to all clients (PatrolsChangeMessage)
 * - Sync to server if online
 * - Handle retries/errors automatically
 *
 * This is a fire-and-forget operation. The UI receives updates via
 * PatrolsChangeMessage broadcasts from the worker.
 */
export const submitScoreChanges = createAsyncThunk<
    void,
    void,
    { state: RootState }
>(
    'worker/submitScores',
    async (_, {getState}) => {
        const state = getState();
        const userId = state.user.userId;
        const selectedSection = selectSelectedSection(state);
        const changes = selectChangesForCurrentSection(state);

        if (!userId) {
            throw new Error('User not authenticated');
        }

        if (!selectedSection) {
            throw new Error('No section selected');
        }

        if (changes.length === 0) {
            return; // Nothing to submit
        }

        const worker: Worker = GetWorker();
        worker.sendSubmitScoresRequest(
            userId,
            selectedSection.id,
            changes.map(c => ({
                patrolId: c.patrolId,
                score: c.score
            }))
        );

        // Note: User entry clearing will be handled by the component after successful submit
        // since we want to show a success message before clearing
    }
);
