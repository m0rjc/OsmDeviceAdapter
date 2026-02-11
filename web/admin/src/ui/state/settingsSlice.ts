import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import type { LoadingState } from './loadState';

/**
 * Patrol info for settings UI (canonical list from OSM)
 */
export type PatrolInfo = {
  id: string;
  name: string;
};

/**
 * Section settings state
 */
export type SectionSettingsState = {
  loadState: LoadingState;
  version: number;
  patrolColors: Record<string, string>;
  patrols: PatrolInfo[];
  error?: string;
  saving: boolean;
  saveError?: string;
};

/**
 * Settings state indexed by section ID
 */
export type SettingsState = {
  sections: Record<number, SectionSettingsState>;
};

const initialState: SettingsState = {
  sections: {},
};

export type SetCanonicalSettingsPayload = {
  sectionId: number;
  version: number;
  patrolColors: Record<string, string>;
  patrols: PatrolInfo[];
};

export type SetSettingsErrorPayload = {
  sectionId: number;
  version: number;
  error: string;
};

export type SetSavingPayload = {
  sectionId: number;
  saving: boolean;
};

export type SetSaveErrorPayload = {
  sectionId: number;
  error: string;
};

export type UpdatePatrolColorPayload = {
  sectionId: number;
  patrolId: string;
  color: string | null; // null to remove
};

const settingsSlice = createSlice({
  name: 'settings',
  initialState,
  reducers: {
    /**
     * Sets the canonical settings from the server
     */
    setCanonicalSettings: (state, action: PayloadAction<SetCanonicalSettingsPayload>) => {
      const { sectionId, version, patrolColors, patrols } = action.payload;
      const existing = state.sections[sectionId];

      if (existing && existing.version >= version) {
        return;
      }

      state.sections[sectionId] = {
        loadState: 'ready',
        version,
        patrolColors,
        patrols,
        saving: false,
      };
    },

    /**
     * Sets the loading state for a section's settings
     */
    setSettingsState: (state, action: PayloadAction<{ sectionId: number; stateName: LoadingState }>) => {
      const { sectionId, stateName } = action.payload;
      if (!state.sections[sectionId]) {
        state.sections[sectionId] = {
          loadState: stateName,
          version: -1,
          patrolColors: {},
          patrols: [],
          saving: false,
        };
      } else {
        state.sections[sectionId].loadState = stateName;
      }
    },

    /**
     * Sets an error state for settings
     */
    setSettingsError: (state, action: PayloadAction<SetSettingsErrorPayload>) => {
      const { sectionId, version, error } = action.payload;
      const existing = state.sections[sectionId];

      if (existing && existing.version >= version) {
        return;
      }

      state.sections[sectionId] = {
        ...state.sections[sectionId],
        loadState: 'error',
        version,
        error,
        saving: false,
      };
    },

    /**
     * Updates a single patrol's color locally (optimistic update)
     */
    updatePatrolColor: (state, action: PayloadAction<UpdatePatrolColorPayload>) => {
      const { sectionId, patrolId, color } = action.payload;
      const section = state.sections[sectionId];

      if (!section) return;

      if (color === null) {
        delete section.patrolColors[patrolId];
      } else {
        section.patrolColors[patrolId] = color;
      }
    },

    /**
     * Sets the saving state
     */
    setSaving: (state, action: PayloadAction<SetSavingPayload>) => {
      const { sectionId, saving } = action.payload;
      const section = state.sections[sectionId];

      if (!section) return;

      section.saving = saving;
      if (saving) {
        section.saveError = undefined;
      }
    },

    /**
     * Sets a save error
     */
    setSaveError: (state, action: PayloadAction<SetSaveErrorPayload>) => {
      const { sectionId, error } = action.payload;
      const section = state.sections[sectionId];

      if (!section) return;

      section.saving = false;
      section.saveError = error;
    },

    /**
     * Clears the save error
     */
    clearSaveError: (state, action: PayloadAction<{ sectionId: number }>) => {
      const section = state.sections[action.payload.sectionId];
      if (section) {
        section.saveError = undefined;
      }
    },
  },
});

export const {
  setCanonicalSettings,
  setSettingsState,
  setSettingsError,
  updatePatrolColor,
  setSaving,
  setSaveError,
  clearSaveError,
} = settingsSlice.actions;

export type { SettingsState as SettingsStateType };

// Selectors

export const selectSettingsBySectionId = (state: SettingsState, sectionId: number): SectionSettingsState | null =>
  state.sections[sectionId] ?? null;

export const selectPatrolColorsBySectionId: (state: SettingsState, sectionId: number) => Record<string, string> =
  (state, sectionId) => state.sections[sectionId]?.patrolColors ?? {};

export const selectPatrolsBySectionId: (state: SettingsState, sectionId: number) => PatrolInfo[] =
  (state, sectionId) => state.sections[sectionId]?.patrols ?? [];

export const selectSettingsLoadState: (state: SettingsState, sectionId: number) => LoadingState =
  (state, sectionId) => state.sections[sectionId]?.loadState ?? 'uninitialized';

export const selectIsSaving: (state: SettingsState, sectionId: number) => boolean =
  (state, sectionId) => state.sections[sectionId]?.saving ?? false;

export const selectSaveError: (state: SettingsState, sectionId: number) => string | undefined =
  (state, sectionId) => state.sections[sectionId]?.saveError;

export default settingsSlice.reducer;
