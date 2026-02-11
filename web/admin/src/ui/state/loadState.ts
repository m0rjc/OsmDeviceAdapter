/**
 * Loading state for async patrol data.
 */
export type LoadingState =
    | 'uninitialized'  // No data, no request sent
    | 'loading'        // Request in flight
    | 'ready'          // Data loaded successfully
    | 'error';         // Load failed

