import { createEntityAdapter, createSlice, type EntityAdapter, type PayloadAction } from '@reduxjs/toolkit';
import type { LoadingState } from './loadState';

export type AdhocTeam = {
  id: string;
  name: string;
  color: string;
  score: number;
  position: number;
};

const entityAdapter: EntityAdapter<AdhocTeam, string> = createEntityAdapter<AdhocTeam, string>({
  selectId: (team: AdhocTeam): string => team.id,
  sortComparer: (a, b) => a.position - b.position,
});

const initialState = entityAdapter.getInitialState({
  loadState: 'uninitialized' as LoadingState,
  error: undefined as string | undefined,
  saving: false,
});

const teamsSlice = createSlice({
  name: 'teams',
  initialState,
  reducers: {
    setTeams: (state, action: PayloadAction<AdhocTeam[]>) => {
      entityAdapter.setAll(state, action.payload);
      state.loadState = 'ready';
      state.error = undefined;
    },
    setTeamsLoading: (state) => {
      state.loadState = 'loading';
    },
    setTeamsError: (state, action: PayloadAction<string>) => {
      state.loadState = 'error';
      state.error = action.payload;
    },
    addTeam: (state, action: PayloadAction<AdhocTeam>) => {
      entityAdapter.addOne(state, action.payload);
    },
    updateTeam: (state, action: PayloadAction<AdhocTeam>) => {
      entityAdapter.upsertOne(state, action.payload);
    },
    removeTeam: (state, action: PayloadAction<string>) => {
      entityAdapter.removeOne(state, action.payload);
    },
    resetAllTeamScores: (state) => {
      const allTeams = entityAdapter.getSelectors().selectAll(state);
      const updates = allTeams.map(t => ({ id: t.id, changes: { score: 0 } }));
      entityAdapter.updateMany(state, updates);
    },
    setTeamsSaving: (state, action: PayloadAction<boolean>) => {
      state.saving = action.payload;
    },
  },
});

export const {
  setTeams,
  setTeamsLoading,
  setTeamsError,
  addTeam,
  updateTeam,
  removeTeam,
  resetAllTeamScores,
  setTeamsSaving,
} = teamsSlice.actions;

export type TeamsState = ReturnType<typeof teamsSlice.reducer>;

// Slice-relative selectors
const selectors = entityAdapter.getSelectors((state: TeamsState) => state);

export const selectAllTeams = (state: TeamsState) => selectors.selectAll(state);
export const selectTeamById = (state: TeamsState, id: string) => selectors.selectById(state, id);
export const selectTeamsLoadState = (state: TeamsState): LoadingState => state.loadState;
export const selectTeamsError = (state: TeamsState): string | undefined => state.error;
export const selectTeamsSaving = (state: TeamsState): boolean => state.saving;

export default teamsSlice.reducer;
