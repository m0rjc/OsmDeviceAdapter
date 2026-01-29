import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from './rootReducer';

/**
 * User authentication state.
 */
export interface UserState {
  /** OSM user ID (null when not authenticated) */
  userId: number | null;
  /** User's display name (null when not authenticated) */
  userName: string | null;
}

const initialState: UserState = {
  userId: null,
  userName: null,
};

const userSlice = createSlice({
  name: 'user',
  initialState,
  reducers: {
    setUser: (state, action: PayloadAction<{ userId: number; userName: string }>) => {
      state.userId = action.payload.userId;
      state.userName = action.payload.userName;
    },
    clearUser: (state) => {
      state.userId = null;
      state.userName = null;
    },
  },
});

export const { setUser, clearUser } = userSlice.actions;

// Selectors
export const selectUserId = (state: RootState) => state.user.userId;
export const selectUserName = (state: RootState) => state.user.userName;
export const selectIsAuthenticated = (state: RootState) => state.user.userId !== null;

export default userSlice.reducer;
