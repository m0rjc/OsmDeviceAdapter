import { createSlice, type PayloadAction } from '@reduxjs/toolkit';

/**
 * User authentication state.
 */
export interface UserState {
  /** Whether we are waiting for the user profile to load */
  loading: boolean;
  /** OSM user ID (null when not authenticated) */
  userId: number | null;
  /** User's display name (null when not authenticated) */
  userName: string | null;
  /** CSRF token for authenticated requests (null when not authenticated) */
  csrfToken: string | null;
}

const initialState: UserState = {
  loading: true, // Start as loading since we immediately request profile on bootstrap
  userId: null,
  userName: null,
  csrfToken: null,
};

const userSlice = createSlice({
  name: 'user',
  initialState,
  reducers: {
    setUser: (state, action: PayloadAction<{ userId: number; userName: string; csrfToken?: string }>) => {
      state.userId = action.payload.userId;
      state.userName = action.payload.userName;
      state.csrfToken = action.payload.csrfToken ?? null;
      state.loading = false;
    },
    setUnauthenticated: (state) => {
      state.userId = null;
      state.userName = null;
      state.csrfToken = null;
      state.loading = false;
    },
  },
});

export const { setUser, setUnauthenticated } = userSlice.actions;

// Slice-relative selectors (take UserState, not RootState)
export const selectUserId = (state: UserState) => state.userId;
export const selectUserName = (state: UserState) => state.userName;
export const selectIsAuthenticated = (state: UserState) => state.userId !== null;
export const selectIsLoading = (state: UserState) => state.loading;

export default userSlice.reducer;
