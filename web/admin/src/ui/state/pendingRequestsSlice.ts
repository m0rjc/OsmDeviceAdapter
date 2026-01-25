import { createSlice, type PayloadAction } from '@reduxjs/toolkit';
import type { RootState } from './store';

/**
 * Type of worker request being tracked.
 */
export type PendingRequestType = 'get-profile' | 'refresh' | 'submit-scores';

/**
 * Information about a pending request to the worker.
 */
export interface PendingRequest {
  /** Unique correlation ID for this request */
  requestId: string;
  /** Type of request */
  type: PendingRequestType;
  /** Section ID (for refresh/submit requests) */
  sectionId?: number;
  /** User ID (for refresh/submit requests) */
  userId?: number;
  /** Timestamp when request was sent (for timeout detection) */
  timestamp: number;
}

/**
 * State tracking all pending worker requests.
 * Keyed by requestId for O(1) lookup when responses arrive.
 */
export interface PendingRequestsState {
  /** Map of requestId -> PendingRequest */
  requests: Record<string, PendingRequest>;
}

const initialState: PendingRequestsState = {
  requests: {},
};

const pendingRequestsSlice = createSlice({
  name: 'pendingRequests',
  initialState,
  reducers: {
    /**
     * Add a new pending request.
     */
    addPendingRequest: (state, action: PayloadAction<PendingRequest>) => {
      state.requests[action.payload.requestId] = action.payload;
    },

    /**
     * Remove a pending request (when response received or timeout).
     */
    removePendingRequest: (state, action: PayloadAction<string>) => {
      delete state.requests[action.payload];
    },

    /**
     * Remove all pending requests (e.g., on logout).
     */
    clearAllPendingRequests: (state) => {
      state.requests = {};
    },
  },
});

export const {
  addPendingRequest,
  removePendingRequest,
  clearAllPendingRequests,
} = pendingRequestsSlice.actions;

// Selectors
export const selectPendingRequests = (state: RootState) => state.pendingRequests.requests;

export const selectPendingRequest = (requestId: string) => (state: RootState) =>
  state.pendingRequests.requests[requestId];

export const selectPendingRefreshForSection = (sectionId: number) => (state: RootState) => {
  const requests = Object.values(state.pendingRequests.requests);
  return requests.find(r => r.type === 'refresh' && r.sectionId === sectionId);
};

export const selectHasPendingRefreshForSection = (sectionId: number) => (state: RootState) => {
  return selectPendingRefreshForSection(sectionId)(state) !== undefined;
};

export default pendingRequestsSlice.reducer;
