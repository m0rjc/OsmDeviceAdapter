import { createSlice, type PayloadAction } from '@reduxjs/toolkit';

/**
 * App-level state for PWA lifecycle and global app concerns.
 * Manages service worker updates and other framework-level state.
 */
export interface AppState {
  /** Whether a new service worker version is available */
  updateAvailable: boolean;
  /** Whether the user has dismissed the current update prompt */
  updateDismissed: boolean;
}

const initialState: AppState = {
  updateAvailable: false,
  updateDismissed: false,
};

const appSlice = createSlice({
  name: 'app',
  initialState,
  reducers: {
    /**
     * Set update availability status (triggered by PWA lifecycle).
     */
    setUpdateAvailable: (state, action: PayloadAction<boolean>) => {
      state.updateAvailable = action.payload;
      // Reset dismissed state when new update appears
      if (action.payload) {
        state.updateDismissed = false;
      }
    },

    /**
     * Dismiss the current update prompt.
     * User can still update later.
     */
    dismissUpdate: (state) => {
      state.updateDismissed = true;
    },
  },
});

export const { setUpdateAvailable, dismissUpdate } = appSlice.actions;

// Slice-relative selectors

/**
 * Selects whether an update is available and not dismissed.
 * This is what components should use to determine if they should show the update prompt.
 */
export const selectShouldShowUpdatePrompt = (state: AppState): boolean =>
  state.updateAvailable && !state.updateDismissed;

/**
 * Selects whether an update is available (regardless of dismissed state).
 */
export const selectUpdateAvailable = (state: AppState): boolean => state.updateAvailable;

export default appSlice.reducer;
