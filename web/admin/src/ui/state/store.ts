import { configureStore } from '@reduxjs/toolkit';
import userReducer from './userSlice';
import appReducer from './appSlice';
import dialogReducer from './dialogSlice';
import pendingRequestsReducer from './pendingRequestsSlice';
import { listenerMiddleware } from './listenerMiddleware';

export const store = configureStore({
  reducer: {
    user: userReducer,
    app: appReducer,
    dialog: dialogReducer,
    pendingRequests: pendingRequestsReducer,
  },
  middleware: (getDefaultMiddleware) =>
    getDefaultMiddleware().prepend(listenerMiddleware.middleware),
});

export type RootState = ReturnType<typeof store.getState>;
export type AppDispatch = typeof store.dispatch;
