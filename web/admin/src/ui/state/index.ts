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
} from './appSlice';
export type { UISection, UIPatrol, AppState } from './appSlice';
