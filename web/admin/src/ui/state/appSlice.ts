import { createSlice } from '@reduxjs/toolkit';

/**
 * App-level state for global app concerns.
 * Service worker updates are now handled automatically (autoUpdate mode).
 */
export interface AppState {
}

const initialState: AppState = {
};

const appSlice = createSlice({
  name: 'app',
  initialState,
  reducers: {
  },
});

export default appSlice.reducer;
