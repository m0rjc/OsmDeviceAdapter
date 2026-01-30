import { createSlice, type PayloadAction } from '@reduxjs/toolkit';

/**
 * Dialog state for displaying error and info dialogs.
 */
export interface DialogState {
  globalError?: string;
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

export type ShowErrorDialogPayload = {
  title: string;
  message: string;
};

const dialogSlice = createSlice({
  name: 'dialog',
  initialState,
  reducers: {
    /**
     * Show an error dialog with a title and message.
     */
    showErrorDialog: (state, action: PayloadAction<ShowErrorDialogPayload>) => {
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

    setGlobalError: (state, action: PayloadAction<string>) => {
      state.globalError = action.payload;
    },
  },
});

export const { showErrorDialog, closeErrorDialog, setGlobalError } = dialogSlice.actions;

// Slice-relative selectors (take DialogState, not RootState)
export const selectIsErrorDialogOpen = (state: DialogState) => state.isErrorDialogOpen;
export const selectErrorTitle = (state: DialogState) => state.errorTitle;
export const selectErrorMessage = (state: DialogState) => state.errorMessage;
export const selectGlobalError = (state: DialogState) => state.globalError;

export default dialogSlice.reducer;
