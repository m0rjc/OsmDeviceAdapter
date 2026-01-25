import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from './store';

/**
 * Dialog state for displaying error and info dialogs.
 */
export interface DialogState {
  /** Whether an error dialog is visible */
  isErrorDialogOpen: boolean;
  /** Error dialog title */
  errorTitle: string | null;
  /** Error dialog message */
  errorMessage: string | null;
}

const initialState: DialogState = {
  isErrorDialogOpen: false,
  errorTitle: null,
  errorMessage: null,
};

const dialogSlice = createSlice({
  name: 'dialog',
  initialState,
  reducers: {
    /**
     * Show an error dialog with a title and message.
     */
    showErrorDialog: (state, action: PayloadAction<{ title: string; message: string }>) => {
      state.isErrorDialogOpen = true;
      state.errorTitle = action.payload.title;
      state.errorMessage = action.payload.message;
    },

    /**
     * Close the error dialog.
     */
    closeErrorDialog: (state) => {
      state.isErrorDialogOpen = false;
      state.errorTitle = null;
      state.errorMessage = null;
    },
  },
});

export const { showErrorDialog, closeErrorDialog } = dialogSlice.actions;

// Selectors
export const selectDialogState = (state: RootState) => state.dialog;
export const selectIsErrorDialogOpen = (state: RootState) => state.dialog.isErrorDialogOpen;
export const selectErrorTitle = (state: RootState) => state.dialog.errorTitle;
export const selectErrorMessage = (state: RootState) => state.dialog.errorMessage;

export default dialogSlice.reducer;
