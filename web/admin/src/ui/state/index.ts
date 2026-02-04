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
} from './workerThunks';

// App slice - actions
export {
    setUpdateAvailable,
    dismissUpdate,
} from './appSlice';

// App slice - types and selectors (from rootReducer)
export type {AppState} from './rootReducer';
export {
    selectShouldShowUpdatePrompt,
    selectUpdateAvailable,
} from './rootReducer';
