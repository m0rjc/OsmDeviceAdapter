import { createEntityAdapter, createSlice, type EntityAdapter, type PayloadAction } from '@reduxjs/toolkit';
import type { LoadingState } from './loadState';

export type Scoreboard = {
  deviceCodePrefix: string;
  sectionId: number | null;
  sectionName: string;
  clientId: string;
  lastUsedAt?: string;
};

const entityAdapter: EntityAdapter<Scoreboard, string> = createEntityAdapter<Scoreboard, string>({
  selectId: (s: Scoreboard): string => s.deviceCodePrefix,
});

const initialState = entityAdapter.getInitialState({
  loadState: 'uninitialized' as LoadingState,
  error: undefined as string | undefined,
  saving: false,
});

const scoreboardsSlice = createSlice({
  name: 'scoreboards',
  initialState,
  reducers: {
    setScoreboards: (state, action: PayloadAction<Scoreboard[]>) => {
      entityAdapter.setAll(state, action.payload);
      state.loadState = 'ready';
      state.error = undefined;
    },
    setScoreboardsLoading: (state) => {
      state.loadState = 'loading';
    },
    setScoreboardsError: (state, action: PayloadAction<string>) => {
      state.loadState = 'error';
      state.error = action.payload;
    },
    updateScoreboardSection: (state, action: PayloadAction<{ deviceCodePrefix: string; sectionId: number; sectionName: string }>) => {
      entityAdapter.updateOne(state, {
        id: action.payload.deviceCodePrefix,
        changes: { sectionId: action.payload.sectionId, sectionName: action.payload.sectionName },
      });
    },
    setScoreboardsSaving: (state, action: PayloadAction<boolean>) => {
      state.saving = action.payload;
    },
  },
});

export const {
  setScoreboards,
  setScoreboardsLoading,
  setScoreboardsError,
  updateScoreboardSection,
  setScoreboardsSaving,
} = scoreboardsSlice.actions;

export type ScoreboardsState = ReturnType<typeof scoreboardsSlice.reducer>;

// Slice-relative selectors
const selectors = entityAdapter.getSelectors((state: ScoreboardsState) => state);

export const selectAllScoreboards = (state: ScoreboardsState) => selectors.selectAll(state);
export const selectScoreboardsLoadState = (state: ScoreboardsState): LoadingState => state.loadState;
export const selectScoreboardsError = (state: ScoreboardsState): string | undefined => state.error;
export const selectScoreboardsSaving = (state: ScoreboardsState): boolean => state.saving;

export default scoreboardsSlice.reducer;
