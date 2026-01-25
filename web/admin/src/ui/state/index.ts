// Store
export { store } from './store';
export type { RootState, AppDispatch } from './store';

// Hooks
export { useAppDispatch, useAppSelector } from './hooks';

// User slice
export {
  setUser,
  clearUser,
  selectUserId,
  selectUserName,
  selectIsAuthenticated,
} from './userSlice';
export type { UserState } from './userSlice';

// App slice (sections and patrols)
export {
  setCanonicalSections,
  setCanonicalPatrols,
  setPatrolsLoading,
  setPatrolsError,
  setUserEntry,
  clearUserEntry,
  clearAllUserEntries,
  selectSection,
  clearSelectedSection,
  clearAllData,
  selectSections,
  selectSelectedSectionId,
  selectSelectedSection,
  selectPatrolsForSelectedSection,
  selectHasSelectedSection,
  selectArePatrolsLoadedForSelectedSection,
  selectPatrolsWithUserEntry,
  selectHasUnsavedEdits,
  selectTotalUserEntryPoints,
  selectPatrolById,
  selectPatrolsLoadingStateForSelectedSection,
  selectIsPatrolsLoading,
  selectPatrolsError,
  selectCanRetryPatrolsLoad,
} from './appSlice';
export type { UISection, UIPatrol, AppState, PatrolsLoadingState } from './appSlice';

// Dialog slice
export {
  showErrorDialog,
  closeErrorDialog,
  selectDialogState,
  selectIsErrorDialogOpen,
  selectErrorTitle,
  selectErrorMessage,
} from './dialogSlice';
export type { DialogState } from './dialogSlice';

// Pending requests slice
export {
  addPendingRequest,
  removePendingRequest,
  clearAllPendingRequests,
  selectPendingRequests,
  selectPendingRequest,
  selectPendingRefreshForSection,
  selectHasPendingRefreshForSection,
} from './pendingRequestsSlice';
export type { PendingRequest, PendingRequestType, PendingRequestsState } from './pendingRequestsSlice';

// Worker thunks
export {
  fetchPatrolScores,
  updateSectionsList,
  selectSection as selectSectionWithPatrolFetch,
} from './workerThunks';
