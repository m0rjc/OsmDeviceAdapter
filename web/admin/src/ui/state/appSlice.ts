import { createSlice, type PayloadAction, createSelector } from '@reduxjs/toolkit';
import type { Section as ModelSection } from '../../types/model';
import type { RootState } from './store';

/**
 * Loading state for async patrol data.
 */
export type PatrolsLoadingState =
  | 'uninitialized'  // No data, no request sent
  | 'loading'        // Request in flight
  | 'ready'          // Data loaded successfully
  | 'error';         // Load failed

/**
 * UI representation of a patrol combining server state with local user input.
 *
 * TODO: We should bring the error information through to here from the SW so we can
 *       provide a means to show it. (Possibly an icon that can be clicked on or hovered over)
 */
export interface UIPatrol {
  /** Patrol ID (can be empty string or negative for OSM special patrols) */
  id: string;
  /** Patrol display name */
  name: string;
  /** Score confirmed on the server */
  committedScore: number;
  /** Score changes queued for sync (stored in IndexedDB, not in Redux) */
  pendingScore: number;
  /** Points the user is currently entering (not yet submitted or queued) */
  userEntry: number;
}

/**
 * UI representation of a section with optional patrol data.
 *
 * The patrols array is undefined until scores are loaded for this section.
 * This allows lazy loading - we can display the section list without
 * fetching scores for all sections upfront.
 *
 * Loading states prevent duplicate requests and provide better UX:
 * - 'uninitialized': No data, no request sent yet
 * - 'loading': Request in flight, avoid duplicate fetches
 * - 'ready': Data loaded successfully
 * - 'error': Load failed, can retry
 *
 * TODO: We'll store last refresh time in here, but we need that supported from the SW code. Future story.
 */
export interface UISection {
  id: number;
  name: string;
  groupName: string;
  /** Patrol scores (undefined until loaded from server) */
  patrols?: UIPatrol[];
  /** Loading state for patrol data */
  patrolsLoadingState: PatrolsLoadingState;
  /** Error message if loading failed */
  patrolsError?: string;
}

/**
 * Application state containing sections and selection.
 */
export interface AppState {
  /** All sections available to the user */
  sections: UISection[];
  /** ID of the currently selected section (null if none selected) */
  selectedSectionId: number | null;
  /** Global error message (e.g., session mismatch) */
  globalError: string | null;
}

const initialState: AppState = {
  sections: [],
  selectedSectionId: null,
  globalError: null,
};

/**
 * Application slice managing sections, patrols, and user score entries.
 *
 * Key Features:
 * - Canonical merge updates (similar to worker/store/store.ts)
 * - Preserves user edits during server updates
 * - Hierarchical data structure (sections contain patrols)
 * - Lazy loading support (patrols loaded on demand)
 */
const appSlice = createSlice({
  name: 'app',
  initialState,
  reducers: {
    /**
     * Set the canonical list of sections (merge pattern like store.ts:306).
     *
     * - Adds new sections
     * - Updates existing sections (name, groupName)
     * - Removes sections not in the new list
     * - Preserves patrols arrays for existing sections
     * - Clears selection if selected section was removed
     */
    setCanonicalSections: (state, action: PayloadAction<ModelSection[]>) => {
      const newSectionIds = new Set(action.payload.map(s => s.id));
      const existingSectionsMap = new Map(state.sections.map(s => [s.id, s]));

      // Build new sections array
      const newSections: UISection[] = [];

      for (const modelSection of action.payload) {
        const existing = existingSectionsMap.get(modelSection.id);
        if (existing) {
          // Update existing section but preserve patrols and loading state
          newSections.push({
            ...existing,
            name: modelSection.name,
            groupName: modelSection.groupName,
          });
        } else {
          // Create new section without patrols
          newSections.push({
            id: modelSection.id,
            name: modelSection.name,
            groupName: modelSection.groupName,
            patrolsLoadingState: 'uninitialized',
          });
        }
      }

      state.sections = newSections;

      // Clear selection if selected section was deleted
      if (state.selectedSectionId !== null && !newSectionIds.has(state.selectedSectionId)) {
        state.selectedSectionId = null;
      }

      // Auto-select first section if no section is selected and sections are available
      if (state.selectedSectionId === null && newSections.length > 0) {
        state.selectedSectionId = newSections[0].id;
      }
    },

    /**
     * Set the canonical list of patrols for a section (merge pattern like store.ts:355).
     *
     * - Adds new patrols
     * - Updates existing patrols (name, committedScore, pendingScore)
     * - Removes patrols not in the new list
     * - PRESERVES userEntry for existing patrols (won't wipe in-progress edits)
     * - Sets loading state to 'ready'
     *
     * This is critical for handling server updates while user is editing.
     * For example, if background sync completes while user is entering scores,
     * this will update committedScore without losing their current input.
     */
    setCanonicalPatrols: (
      state,
      action: PayloadAction<{
        sectionId: number;
        patrols: Array<{ id: string; name: string; committedScore: number; pendingScore: number }>;
      }>
    ) => {
      const { sectionId, patrols } = action.payload;
      const section = state.sections.find(s => s.id === sectionId);

      if (!section) {
        console.warn(`[appSlice] Cannot set patrols for unknown section ${sectionId}`);
        return;
      }

      const existingPatrolsMap = new Map(
        (section.patrols || []).map(p => [p.id, p])
      );

      // Build new patrols array
      const newPatrols: UIPatrol[] = patrols.map(modelPatrol => {
        const existing = existingPatrolsMap.get(modelPatrol.id);

        if (existing) {
          // Update committed/pending scores but preserve userEntry
          return {
            ...existing,
            name: modelPatrol.name,
            committedScore: modelPatrol.committedScore,
            pendingScore: modelPatrol.pendingScore,
          };
        } else {
          // Create new patrol with no userEntry
          return {
            id: modelPatrol.id,
            name: modelPatrol.name,
            committedScore: modelPatrol.committedScore,
            pendingScore: modelPatrol.pendingScore,
            userEntry: 0,
          };
        }
      });

      section.patrols = newPatrols;
      section.patrolsLoadingState = 'ready';
      section.patrolsError = undefined;
    },

    /**
     * Set patrol loading state to 'loading' for a section.
     * Called when we start fetching patrol data.
     */
    setPatrolsLoading: (state, action: PayloadAction<{ sectionId: number }>) => {
      const section = state.sections.find(s => s.id === action.payload.sectionId);
      if (section) {
        section.patrolsLoadingState = 'loading';
        section.patrolsError = undefined;
      }
    },

    /**
     * Set patrol loading state to 'error' for a section.
     * Called when patrol fetch fails.
     */
    setPatrolsError: (
      state,
      action: PayloadAction<{ sectionId: number; error: string }>
    ) => {
      const section = state.sections.find(s => s.id === action.payload.sectionId);
      if (section) {
        section.patrolsLoadingState = 'error';
        section.patrolsError = action.payload.error;
      }
    },

    /**
     * Update the user entry for a specific patrol.
     *
     * This is the "working copy" - points the user is currently entering
     * but hasn't submitted yet. Values are clamped to [-1000, 1000] by
     * the input component before calling this action.
     */
    setUserEntry: (
      state,
      action: PayloadAction<{ sectionId: number; patrolId: string; points: number }>
    ) => {
      const { sectionId, patrolId, points } = action.payload;
      const section = state.sections.find(s => s.id === sectionId);

      if (!section?.patrols) {
        console.warn(`[appSlice] Cannot set user entry for section ${sectionId} - patrols not loaded`);
        return;
      }

      const patrol = section.patrols.find(p => p.id === patrolId);
      if (patrol) {
        patrol.userEntry = points;
      }
    },

    /**
     * Clear user entry for a specific patrol.
     *
     * Called after successfully submitting that patrol's score,
     * or when user explicitly clears a single field.
     */
    clearUserEntry: (
      state,
      action: PayloadAction<{ sectionId: number; patrolId: string }>
    ) => {
      const { sectionId, patrolId } = action.payload;
      const section = state.sections.find(s => s.id === sectionId);

      if (!section?.patrols) return;

      const patrol = section.patrols.find(p => p.id === patrolId);
      if (patrol) {
        patrol.userEntry = 0;
      }
    },

    /**
     * Clear user entries for all patrols in a section.
     *
     * Called after submitting all changes for a section,
     * or when user clicks "Clear" button.
     */
    clearAllUserEntries: (state, action: PayloadAction<{ sectionId: number }>) => {
      const section = state.sections.find(s => s.id === action.payload.sectionId);

      if (!section?.patrols) return;

      section.patrols.forEach(patrol => {
        patrol.userEntry = 0;
      });
    },

    /**
     * Select a section for viewing/editing.
     *
     * Triggers patrol loading in ScoreEntryPage if patrols aren't loaded yet.
     */
    selectSection: (state, action: PayloadAction<number>) => {
      state.selectedSectionId = action.payload;
    },

    /**
     * Clear the selected section.
     *
     * Returns to "no section selected" state.
     */
    clearSelectedSection: (state) => {
      state.selectedSectionId = null;
    },

    /**
     * Clear all data (e.g., on logout).
     *
     * Resets to initial state - no sections, no selection.
     */
    clearAllData: (state) => {
      state.sections = [];
      state.selectedSectionId = null;
      state.globalError = null;
    },

    /**
     * Set a global error message.
     *
     * Used for critical errors like session mismatch that require user action.
     */
    setGlobalError: (state, action: PayloadAction<string>) => {
      state.globalError = action.payload;
    },

    /**
     * Clear the global error message.
     */
    clearGlobalError: (state) => {
      state.globalError = null;
    },
  },
});

export const {
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
  setGlobalError,
  clearGlobalError,
} = appSlice.actions;

// Basic selectors
export const selectSections = (state: RootState) => state.app.sections;
export const selectSelectedSectionId = (state: RootState) => state.app.selectedSectionId;
export const selectGlobalError = (state: RootState) => state.app.globalError;

// Composed selectors
export const selectSelectedSection = createSelector(
  [selectSections, selectSelectedSectionId],
  (sections, selectedId) => {
    if (selectedId === null) return null;
    return sections.find(s => s.id === selectedId) || null;
  }
);

export const selectPatrolsForSelectedSection = createSelector(
  [selectSelectedSection],
  (section) => section?.patrols || null
);

export const selectHasSelectedSection = createSelector(
  [selectSelectedSectionId],
  (selectedId) => selectedId !== null
);

export const selectArePatrolsLoadedForSelectedSection = createSelector(
  [selectSelectedSection],
  (section) => section !== null && section.patrols !== undefined
);

export const selectPatrolsWithUserEntry = createSelector(
  [selectSelectedSection],
  (section) => {
    if (!section?.patrols) return [];
    return section.patrols.filter(p => p.userEntry !== 0);
  }
);

export const selectHasUnsavedEdits = createSelector(
  [selectPatrolsWithUserEntry],
  (patrols) => patrols.length > 0
);

export const selectTotalUserEntryPoints = createSelector(
  [selectSelectedSection],
  (section) => {
    if (!section?.patrols) return 0;
    return section.patrols.reduce((sum, p) => sum + p.userEntry, 0);
  }
);

export const selectPatrolById = (sectionId: number, patrolId: string) =>
  createSelector(
    [selectSections],
    (sections) => {
      const section = sections.find(s => s.id === sectionId);
      if (!section?.patrols) return null;
      return section.patrols.find(p => p.id === patrolId) || null;
    }
  );

// Loading state selectors
export const selectPatrolsLoadingStateForSelectedSection = createSelector(
  [selectSelectedSection],
  (section) => section?.patrolsLoadingState || 'uninitialized'
);

export const selectIsPatrolsLoading = createSelector(
  [selectPatrolsLoadingStateForSelectedSection],
  (state) => state === 'loading'
);

export const selectPatrolsError = createSelector(
  [selectSelectedSection],
  (section) => section?.patrolsError || null
);

export const selectCanRetryPatrolsLoad = createSelector(
  [selectPatrolsLoadingStateForSelectedSection],
  (state) => state === 'error'
);

export default appSlice.reducer;
