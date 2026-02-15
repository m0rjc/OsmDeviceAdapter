import {createAsyncThunk, type GetThunkAPI} from '@reduxjs/toolkit';
import {
    GetWorker,
    type PatrolsChangeMessage,
    type SectionListChangeMessage,
    type UserProfileMessage,
    type Worker
} from '../worker';
import {setUnauthenticated, setUser} from './userSlice';
import * as uiSlice from './uiSlice';
import {selectSelectedSectionId} from './uiSlice';
import type {RootState} from './rootReducer';
import {selectChangesForCurrentSection, selectSelectedSection, selectSettingsLoadState} from "./rootReducer";
import {
    type SectionMetadata,
    setCanonicalPatrols,
    setCanonicalSectionList,
    setSectionError,
    setSectionListLoadError,
    setSectionState
} from "./patrolsSlice.ts";
import {setGlobalError, showErrorDialog} from "./dialogSlice.ts";
import type {AppDispatch} from "./store.ts";
import {
    setCanonicalSettings,
    setSettingsError,
    setSettingsState,
    setSaving,
    setSaveError,
} from "./settingsSlice.ts";
import {
    setTeams,
    setTeamsLoading,
    setTeamsError,
    addTeam,
    updateTeam as updateTeamAction,
    removeTeam,
    resetAllTeamScores,
    setTeamsSaving,
} from "./teamsSlice.ts";
import {
    setScoreboards,
    setScoreboardsLoading,
    setScoreboardsError,
    updateScoreboardSection as updateScoreboardSectionAction,
    setScoreboardsSaving,
} from "./scoreboardsSlice.ts";
import {OsmAdapterApiService} from "../../worker/server/server";

type AppThunkConfig = {
    state: RootState;
    dispatch: AppDispatch;
};

type ThunkApiBasics = Pick<GetThunkAPI<AppThunkConfig>, 'getState' | 'dispatch'>;

/**
 * Change the selected section if needed.
 * This does not trigger a fetch of patrol scores.
 * @param getState
 * @param dispatch
 */
function setDefaultSectionIfNeeded({getState, dispatch}: ThunkApiBasics): boolean {
    const state: RootState = getState();
    const selectedSectionId: number | null = selectSelectedSectionId(state.ui);
    const availableSectionIds: number[] = state.patrols.sectionIds; //TODO: Encapsulate in slice

    const hasSections = availableSectionIds.length > 0;
    const sectionDisappeared = selectedSectionId !== null && !availableSectionIds.includes(selectedSectionId);
    const sectionWasNull = selectedSectionId === null && availableSectionIds.length > 0;

    let changed = false;
    if (hasSections && (sectionDisappeared || sectionWasNull)) {
        dispatch(dispatch(uiSlice.setSelectedSectionId(availableSectionIds[0])));
        changed = true;
    }

    if (sectionDisappeared && !hasSections) {
        dispatch(dispatch(uiSlice.setSelectedSectionId(null)));
        changed = true;
    }

    return changed;
}

/**
 * Fetch patrol scores if needed for the currently selected section.
 * @param getState
 * @param dispatch
 */
function loadPatrolsIfNeeded({getState, dispatch}: ThunkApiBasics) {
    const selectedSection: SectionMetadata | null = selectSelectedSection(getState());
    if (selectedSection?.state === 'uninitialized') {
        dispatch(fetchPatrolScoresForSection(selectedSection.id));
    }
}

/**
 * Fetch settings if needed for the currently selected section.
 * Follows the same pattern as loadPatrolsIfNeeded.
 */
function loadSettingsIfNeeded({getState, dispatch}: ThunkApiBasics) {
    const state = getState();
    const selectedSection = selectSelectedSection(state);
    if (selectedSection) {
        const settingsLoadState = selectSettingsLoadState(state, selectedSection.id);
        if (settingsLoadState === 'uninitialized') {
            dispatch(fetchSectionSettings(selectedSection.id));
        }
    }
}

// Retry timer for auto-syncing pending scores
let retryTimerId: ReturnType<typeof setTimeout> | null = null;

function scheduleRetry(dispatch: AppDispatch, nextRetryTime: number | undefined) {
    if (retryTimerId !== null) {
        clearTimeout(retryTimerId);
        retryTimerId = null;
    }
    if (!nextRetryTime) return;

    const delay = nextRetryTime - Date.now();
    if (delay <= 0) {
        dispatch(syncNow());
    } else {
        retryTimerId = setTimeout(() => {
            retryTimerId = null;
            dispatch(syncNow());
        }, delay);
    }
}


export const handleUserProfileMessage = createAsyncThunk<
    void,
    UserProfileMessage,
    AppThunkConfig>(
    'worker/handleUserProfileMessage',
    async (message: UserProfileMessage, {dispatch, getState}) => {
        dispatch(setUser({userId: message.userId, userName: message.userName, csrfToken: message.csrfToken}));

        const currentVersion = getState().patrols.sectionIdListVersion;
        if (currentVersion >= message.sectionsListRevision) {
            console.warn('[handleUserProfileMessage] Ignoring outdated profile message:', message.sectionsListRevision, currentVersion);
            return;
        }
        dispatch(setCanonicalSectionList({version: message.sectionsListRevision, sections: message.sections}));

        setDefaultSectionIfNeeded({getState, dispatch});
        loadPatrolsIfNeeded({getState, dispatch});
        loadSettingsIfNeeded({getState, dispatch});

        // Show an error dialog if we have an error
        if (message.lastError) {
            dispatch(showErrorDialog({title: 'Error fetching section list', message: message.lastError}))
            dispatch(setSectionListLoadError({version: message.sectionsListRevision, error: message.lastError}))
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
    AppThunkConfig
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
        setDefaultSectionIfNeeded({getState, dispatch});
        loadPatrolsIfNeeded({getState, dispatch});
        loadSettingsIfNeeded({getState, dispatch});
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
    AppThunkConfig
>(
    'worker/selectSection',
    async (sectionId, {dispatch, getState}) => {
        // Update the selected section
        dispatch(uiSlice.setSelectedSectionId(sectionId));

        // Check if we need to fetch patrols and settings for this section
        loadPatrolsIfNeeded({getState, dispatch});
        loadSettingsIfNeeded({getState, dispatch});
    }
);

/**
 * Thunk to fetch patrol scores from the worker for a specific section.
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
export const fetchPatrolScoresForSection = createAsyncThunk<
    void,
    number,
    AppThunkConfig
>(
    'worker/fetchPatrolScores',
    async (sectionId, {getState, dispatch}) => {
        const state = getState();
        const userId = state.user.userId;

        if (!userId) {
            throw new Error('User not authenticated');
        }

        dispatch(setSectionState({sectionId, stateName: 'loading'}));

        const worker: Worker = await GetWorker();
        worker.sendRefreshRequest(userId, sectionId);

        // Note: This thunk completes immediately after sending the message.
        // The actual response (PatrolsChangeMessage or ServiceErrorMessage)
        // will arrive asynchronously through the worker's onMessage handler.
    }
);

/**
 * Thunk to refresh patrol scores for the currently selected section.
 *
 * Convenience wrapper around fetchPatrolScoresForSection that uses
 * the currently selected section from state.
 */
export const refreshCurrentSection = createAsyncThunk<
    void,
    void,
    AppThunkConfig
>(
    'worker/refreshCurrentSection',
    async (_, {getState, dispatch}) => {
        const state = getState();
        const selectedSection = selectSelectedSection(state);

        if (!selectedSection) {
            throw new Error('No section selected');
        }

        dispatch(fetchPatrolScoresForSection(selectedSection.id));
    }
);

/**
 * Thunk to handle a patrols change message from the worker.
 */
export const handlePatrolsChange = createAsyncThunk<
    void,
    PatrolsChangeMessage,
    AppThunkConfig
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
            sectionId: message.sectionId,
            nextRetryTime: message.nextRetryTime,
            pendingCount: message.pendingCount,
            readyCount: message.readyCount,
            syncInProgress: message.syncInProgress
        }));

        if (message.lastError) {
            dispatch(showErrorDialog({title: 'Error fetching patrol scores', message: message.lastError}))
            dispatch(setSectionError({
                sectionId: message.sectionId,
                error: message.lastError,
                version: message.uiRevision
            }));
        }

        // Schedule auto-retry if this message is for the selected section
        const selectedSection = selectSelectedSection(getState());
        if (selectedSection && message.sectionId === selectedSection.id) {
            scheduleRetry(dispatch, message.nextRetryTime);
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
export const handleWrongUser = createAsyncThunk<void, void, AppThunkConfig>(
    'worker/handleWrongUser',
    async (_, {dispatch}) => {
        dispatch(setUnauthenticated());
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
    AppThunkConfig
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

        const worker: Worker = await GetWorker();
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
        // TODO: Consider risk of double-submit. Review this.
    }
);

/**
 * Thunk to sync pending scores now.
 * Respects retry timers and permanent errors.
 * Triggered by user clicking "Sync Now" button or automatic retry timer.
 */
export const syncNow = createAsyncThunk<
    void,
    void,
    AppThunkConfig
>(
    'worker/syncNow',
    async (_, {getState}) => {
        const state = getState();
        const userId = state.user.userId;
        const selectedSection = selectSelectedSection(state);

        if (!userId) {
            throw new Error('User not authenticated');
        }

        if (!selectedSection) {
            throw new Error('No section selected');
        }

        const worker: Worker = await GetWorker();
        worker.sendSyncNowRequest(userId, selectedSection.id);
    }
);

/**
 * Thunk to force sync pending scores.
 * Clears permanent errors but preserves rate limit backoffs.
 * Triggered by user clicking "Force Sync" button after confirmation.
 */
export const forceSync = createAsyncThunk<
    void,
    void,
    AppThunkConfig
>(
    'worker/forceSync',
    async (_, {getState}) => {
        const state = getState();
        const userId = state.user.userId;
        const selectedSection = selectSelectedSection(state);

        if (!userId) {
            throw new Error('User not authenticated');
        }

        if (!selectedSection) {
            throw new Error('No section selected');
        }

        const worker: Worker = await GetWorker();
        worker.sendForceSyncRequest(userId, selectedSection.id);
    }
);

// ============================================================================
// Settings Thunks
// ============================================================================

// Version counter for settings requests
let settingsVersion = 0;

/**
 * Thunk to fetch settings for a section.
 * Settings are fetched directly from the API (not via worker) since they
 * don't require offline support.
 */
export const fetchSectionSettings = createAsyncThunk<
    void,
    number,
    AppThunkConfig
>(
    'settings/fetchSectionSettings',
    async (sectionId, {getState, dispatch}) => {
        const state = getState();
        const userId = state.user.userId;
        const csrfToken = state.user.csrfToken;

        if (!userId) {
            throw new Error('User not authenticated');
        }

        const version = ++settingsVersion;
        dispatch(setSettingsState({sectionId, stateName: 'loading'}));

        try {
            const api = new OsmAdapterApiService(userId, state.user.userName ?? undefined, csrfToken ?? undefined);
            const settings = await api.fetchSettings(sectionId);

            dispatch(setCanonicalSettings({
                sectionId,
                version,
                patrolColors: settings.patrolColors,
                patrols: settings.patrols,
            }));
        } catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to fetch settings';
            dispatch(setSettingsError({sectionId, version, error: message}));
            throw error;
        }
    }
);

/**
 * Thunk to save section settings.
 */
export const saveSectionSettings = createAsyncThunk<
    void,
    {sectionId: number, patrolColors: Record<string, string>},
    AppThunkConfig
>(
    'settings/saveSectionSettings',
    async ({sectionId, patrolColors}, {getState, dispatch}) => {
        const state = getState();
        const userId = state.user.userId;
        const csrfToken = state.user.csrfToken;

        if (!userId) {
            throw new Error('User not authenticated');
        }

        dispatch(setSaving({sectionId, saving: true}));

        try {
            const api = new OsmAdapterApiService(userId, state.user.userName ?? undefined, csrfToken ?? undefined);
            const updatedSettings = await api.updateSettings(sectionId, patrolColors);

            // Update state with the server response
            const version = ++settingsVersion;
            dispatch(setCanonicalSettings({
                sectionId,
                version,
                patrolColors: updatedSettings.patrolColors,
                patrols: state.settings.sections[sectionId]?.patrols ?? [],
            }));
        } catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to save settings';
            dispatch(setSaveError({sectionId, error: message}));
            throw error;
        }
    }
);

// ============================================================================
// Ad-hoc Teams Thunks
// ============================================================================

function createApi(state: RootState): OsmAdapterApiService {
    return new OsmAdapterApiService(
        state.user.userId ?? undefined,
        state.user.userName ?? undefined,
        state.user.csrfToken ?? undefined,
    );
}

export const fetchTeams = createAsyncThunk<void, void, AppThunkConfig>(
    'teams/fetchTeams',
    async (_, { getState, dispatch }) => {
        dispatch(setTeamsLoading());
        try {
            const api = createApi(getState());
            const patrols = await api.fetchAdhocPatrols();
            dispatch(setTeams(patrols));
        } catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to fetch teams';
            dispatch(setTeamsError(message));
            throw error;
        }
    }
);

export const createTeam = createAsyncThunk<void, { name: string; color: string }, AppThunkConfig>(
    'teams/createTeam',
    async ({ name, color }, { getState, dispatch }) => {
        dispatch(setTeamsSaving(true));
        try {
            const api = createApi(getState());
            const patrol = await api.createAdhocPatrol(name, color);
            dispatch(addTeam(patrol));
        } finally {
            dispatch(setTeamsSaving(false));
        }
    }
);

export const updateTeam = createAsyncThunk<void, { id: string; name: string; color: string }, AppThunkConfig>(
    'teams/updateTeam',
    async ({ id, name, color }, { getState, dispatch }) => {
        dispatch(setTeamsSaving(true));
        try {
            const api = createApi(getState());
            const patrol = await api.updateAdhocPatrol(id, name, color);
            dispatch(updateTeamAction(patrol));
        } finally {
            dispatch(setTeamsSaving(false));
        }
    }
);

export const deleteTeam = createAsyncThunk<void, string, AppThunkConfig>(
    'teams/deleteTeam',
    async (id, { getState, dispatch }) => {
        dispatch(setTeamsSaving(true));
        try {
            const api = createApi(getState());
            await api.deleteAdhocPatrol(id);
            dispatch(removeTeam(id));
        } finally {
            dispatch(setTeamsSaving(false));
        }
    }
);

export const resetTeamScores = createAsyncThunk<void, void, AppThunkConfig>(
    'teams/resetScores',
    async (_, { getState, dispatch }) => {
        dispatch(setTeamsSaving(true));
        try {
            const api = createApi(getState());
            await api.resetAdhocScores();
            dispatch(resetAllTeamScores());
        } finally {
            dispatch(setTeamsSaving(false));
        }
    }
);

// ============================================================================
// Scoreboard Thunks
// ============================================================================

export const fetchScoreboards = createAsyncThunk<void, void, AppThunkConfig>(
    'scoreboards/fetchScoreboards',
    async (_, { getState, dispatch }) => {
        dispatch(setScoreboardsLoading());
        try {
            const api = createApi(getState());
            const boards = await api.fetchScoreboards();
            dispatch(setScoreboards(boards));
        } catch (error) {
            const message = error instanceof Error ? error.message : 'Failed to fetch scoreboards';
            dispatch(setScoreboardsError(message));
            throw error;
        }
    }
);

export const changeScoreboardSection = createAsyncThunk<
    void,
    { deviceCodePrefix: string; sectionId: number; sectionName: string },
    AppThunkConfig
>(
    'scoreboards/changeSection',
    async ({ deviceCodePrefix, sectionId, sectionName }, { getState, dispatch }) => {
        dispatch(setScoreboardsSaving(true));
        try {
            const api = createApi(getState());
            await api.updateScoreboardSection(deviceCodePrefix, sectionId);
            dispatch(updateScoreboardSectionAction({ deviceCodePrefix, sectionId, sectionName }));
        } finally {
            dispatch(setScoreboardsSaving(false));
        }
    }
);
