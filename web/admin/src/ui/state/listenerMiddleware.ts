import { createListenerMiddleware, isAnyOf } from '@reduxjs/toolkit';
import type { RootState, AppDispatch } from './store';
import { setCanonicalSections, selectSection } from './appSlice';
import { fetchPatrolScores } from './workerThunks';
import { selectHasPendingRefreshForSection } from './pendingRequestsSlice';

/**
 * Listener middleware for automatic side effects.
 *
 * This middleware implements the "reactive" part of our hybrid architecture:
 * - Thunks handle explicit user actions (selectSectionWithPatrolFetch)
 * - Listeners handle implicit state changes (setCanonicalSections auto-selecting)
 *
 * Pattern: "When selected section changes AND patrols not loaded/loading, fetch them"
 *
 * This ensures patrols are loaded regardless of how the section selection changed.
 */
export const listenerMiddleware = createListenerMiddleware<RootState, AppDispatch>();

/**
 * Auto-fetch patrols when section is selected (if needed).
 *
 * Triggered by:
 * - setCanonicalSections (auto-selects first section on initial load)
 * - selectSection (manual section selection, though should use thunk instead)
 *
 * Guards:
 * - Skip if no section selected
 * - Skip if patrols already loaded (state = 'ready')
 * - Skip if patrols already loading (state = 'loading')
 * - Skip if refresh request already pending
 */
listenerMiddleware.startListening({
  matcher: isAnyOf(setCanonicalSections, selectSection),
  effect: async (action, listenerApi) => {
    const state = listenerApi.getState();
    const selectedSectionId = state.app.selectedSectionId;

    if (selectedSectionId === null) {
      return; // No section selected
    }

    const selectedSection = state.app.sections.find(s => s.id === selectedSectionId);
    if (!selectedSection) {
      return; // Section not found (shouldn't happen)
    }

    // Check if patrols are already loaded or loading
    if (selectedSection.patrolsLoadingState === 'ready' || selectedSection.patrolsLoadingState === 'loading') {
      console.log('[listener] Patrols already loaded/loading for section:', selectedSectionId);
      return;
    }

    // Check if there's already a pending refresh request for this section
    const hasPendingRefresh = selectHasPendingRefreshForSection(selectedSectionId)(state);
    if (hasPendingRefresh) {
      console.log('[listener] Refresh already in progress for section:', selectedSectionId);
      return;
    }

    // All guards passed - trigger fetch
    console.log('[listener] Auto-fetching patrols for section:', selectedSectionId);
    listenerApi.dispatch(fetchPatrolScores(selectedSectionId));
  },
});
