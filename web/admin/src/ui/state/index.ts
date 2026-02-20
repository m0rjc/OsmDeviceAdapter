// Store
export {store} from './store';
export type {RootState, AppDispatch} from './store';

// Hooks
export {useAppDispatch, useAppSelector} from './hooks';

// User slice - actions
export {setUser} from './userSlice';

// User slice - types and selectors (from rootReducer)
export type {UserState} from './rootReducer';
export {
    selectUserId,
    selectUserName,
    selectIsAuthenticated,
    selectIsLoading,
} from './rootReducer';

// Patrols slice - types
export {
    type SectionMetadata,
    type UIPatrol,
} from './patrolsSlice';

// Patrols slice - selectors (from rootReducer)
export {
    selectSelectedSection,
    selectSelectedPatrolKeys,
    selectChangesForCurrentSection,
    selectSections,
    makeSelectPatrolById,
} from './rootReducer';

// UI slice - actions
export {
    setPatrolScore,
    clearUserEntriesForSection,
} from './uiSlice';

// UI slice - selectors (from rootReducer)
export {
    selectSelectedSectionId,
    selectUserScoreForPatrolKey,
} from './rootReducer';

// Dialog slice - actions
export {
    showErrorDialog,
    closeErrorDialog,
    setGlobalError,
} from './dialogSlice';

// Dialog slice - types and selectors (from rootReducer)
export type {DialogState} from './rootReducer';
export {
    selectDialogState,
    selectIsErrorDialogOpen,
    selectErrorTitle,
    selectErrorMessage,
    selectGlobalError,
} from './rootReducer';

// Worker thunks
export {
    setSelectedSection,
    handlePatrolsChange,
    handleSectionListChange,
    handleUserProfileMessage,
    handleWrongUser,
    submitScoreChanges,
    refreshCurrentSection,
    syncNow,
    forceSync,
    fetchSectionSettings,
    saveSectionSettings,
} from './workerThunks';

// App slice - types (from rootReducer)
export type {AppState} from './rootReducer';

// Settings slice - actions
export {
    setCanonicalSettings,
    setSettingsState,
    setSettingsError,
    updatePatrolColor,
    setSaving,
    setSaveError,
    clearSaveError,
} from './settingsSlice';

// Settings slice - types and selectors (from rootReducer)
export type {SettingsState, SectionSettingsState, PatrolInfo} from './rootReducer';
export {
    selectSettingsForSection,
    selectPatrolColorsForSection,
    selectPatrolsForSettings,
    selectSettingsLoadState,
    selectIsSavingSettings,
    selectSettingsSaveError,
} from './rootReducer';

// Teams slice - actions and types
export type {AdhocTeam, TeamsState} from './rootReducer';
export {
    selectAllTeams,
    selectTeamsLoadState,
    selectTeamsError,
    selectTeamsSaving,
} from './rootReducer';
export {
    fetchTeams,
    createTeam,
    updateTeam,
    deleteTeam,
    resetTeamScores,
} from './workerThunks';

// Scoreboards slice - actions and types
export type {Scoreboard, ScoreboardsState} from './rootReducer';
export {
    selectAllScoreboards,
    selectScoreboardsLoadState,
    selectScoreboardsError,
    selectScoreboardsSaving,
} from './rootReducer';
export {
    fetchScoreboards,
    changeScoreboardSection,
    sendTimerCommand,
} from './workerThunks';
