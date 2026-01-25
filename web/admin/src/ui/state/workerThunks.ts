import { createAsyncThunk } from '@reduxjs/toolkit';
import { GetWorker } from '../worker';
import { setCanonicalSections, selectSection as selectSectionAction, setPatrolsLoading } from './appSlice';
import { addPendingRequest } from './pendingRequestsSlice';
import type { RootState } from './store';
import type { Section } from '../../types/model';

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
export const fetchPatrolScores = createAsyncThunk<
  void,
  number,
  { state: RootState }
>(
  'worker/fetchPatrolScores',
  async (sectionId, { getState, dispatch }) => {
    const state = getState();
    const userId = state.user.userId;

    if (!userId) {
      throw new Error('User not authenticated');
    }

    const worker = GetWorker();

    // Send request and get correlation ID
    const requestId = worker.sendRefreshRequest(Number(userId), sectionId);

    // Track the pending request
    dispatch(addPendingRequest({
      requestId,
      type: 'refresh',
      sectionId,
      userId: Number(userId),
      timestamp: Date.now(),
    }));

    // Set UI loading state
    dispatch(setPatrolsLoading({ sectionId }));

    // Note: This thunk completes immediately after sending the message.
    // The actual response (PatrolsChangeMessage or ServiceErrorMessage)
    // will arrive asynchronously through the worker's onMessage handler.
  }
);

/**
 * Thunk to update the section list and auto-fetch patrols if needed.
 *
 * This thunk:
 * 1. Updates the canonical section list (which may auto-select first section)
 * 2. Checks if a section is now selected
 * 3. If selected and patrols aren't loaded yet, triggers patrol fetch
 *
 * This centralizes the "section change -> load patrols" logic in one place.
 *
 * @param sections - The new section list from the worker
 */
export const updateSectionsList = createAsyncThunk<
  void,
  Section[],
  { state: RootState }
>(
  'worker/updateSectionsList',
  async (sections, { dispatch, getState }) => {
    // Update the canonical section list (may auto-select first section)
    dispatch(setCanonicalSections(sections));

    // Check if we need to fetch patrols for the selected section
    const state = getState();
    const selectedSectionId = state.app.selectedSectionId;

    if (selectedSectionId !== null) {
      const selectedSection = state.app.sections.find(s => s.id === selectedSectionId);

      // If section is selected but patrols aren't loaded yet, fetch them
      if (selectedSection && selectedSection.patrols === undefined) {
        console.log('[updateSectionsList] Auto-fetching patrols for section:', selectedSectionId);
        dispatch(fetchPatrolScores(selectedSectionId));
      }
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
export const selectSection = createAsyncThunk<
  void,
  number,
  { state: RootState }
>(
  'worker/selectSection',
  async (sectionId, { dispatch, getState }) => {
    // Update the selected section
    dispatch(selectSectionAction(sectionId));

    // Check if we need to fetch patrols for this section
    const state = getState();
    const selectedSection = state.app.sections.find(s => s.id === sectionId);

    if (selectedSection && selectedSection.patrols === undefined) {
      console.log('[selectSection] Auto-fetching patrols for section:', sectionId);
      dispatch(fetchPatrolScores(sectionId));
    }
  }
);
