// Store
export {store} from './store';
export type {RootState, AppDispatch} from './store';

// Hooks
export {useAppDispatch, useAppSelector} from './hooks';

// User slice
export {
    setUser,
    clearUser,
    selectUserId,
    selectUserName,
    selectIsAuthenticated,
} from './userSlice';
export type {UserState} from './userSlice';

export {
    selectSections,
    makeSelectPatrolById
} from './patrolsSlice'

export {
    selectSelectedSection,
    selectSelectedPatrolKeys,
    selectChangesForCurrentSection,
} from './rootReducer'

export {
    setPatrolScore,
    clearUserEntriesForSection,
    selectSelectedSectionId,
    selectUserScoreForPatrolKey
} from './uiSlice'

// Dialog slice
export {
    showErrorDialog,
    closeErrorDialog,
    setGlobalError,
    selectDialogState,
    selectIsErrorDialogOpen,
    selectErrorTitle,
    selectErrorMessage,
    selectGlobalError,
} from './dialogSlice';
export type {DialogState} from './dialogSlice';

// Worker thunks
export {
    setSelectedSection,
    handlePatrolsChange,
    handleSectionListChange,
    handleUserProfileMessage,
    handleWrongUser,
    submitScoreChanges,
} from './workerThunks';
