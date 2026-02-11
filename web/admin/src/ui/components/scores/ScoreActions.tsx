interface ScoreActionsProps {
  /** Whether user has entered any points (enables Clear and Submit buttons) */
  hasChanges: boolean;
  /** Whether scores are currently being refreshed from server */
  isRefreshing: boolean;
  /** Whether the app is online (disables Refresh when offline) */
  isOnline: boolean;
  /** Number of entries ready to sync now (enables Sync Now button) */
  readyCount: number;
  /** Total number of pending entries (shows Force Sync button if stuck entries exist) */
  pendingCount: number;
  /** Callback to refresh scores from server */
  onRefresh: () => void;
  /** Callback to clear all user-entered points */
  onClear: () => void;
  /** Callback to submit score changes */
  onSubmit: () => void;
  /** Callback to sync pending scores now */
  onSyncNow: () => void;
  /** Callback to force sync (clears permanent errors) */
  onForceSync: () => void;
}

/**
 * Action button bar for score entry.
 *
 * Provides actions:
 * - Refresh: Fetches latest scores from server (disabled when offline or refreshing)
 * - Clear: Resets all user entries to zero (disabled when no changes)
 * - Add Scores: Submits score changes (disabled when no changes)
 * - Sync Now: Syncs pending scores immediately (enabled when readyCount > 0)
 * - Force Sync: Clears permanent errors and retries (shown when stuck entries exist)
 *
 * Buttons are automatically enabled/disabled based on state.
 */
export function ScoreActions({
  hasChanges,
  isRefreshing,
  isOnline,
  readyCount,
  pendingCount,
  onRefresh,
  onClear,
  onSubmit,
  onSyncNow,
  onForceSync,
}: ScoreActionsProps) {
  // Show Force Sync if there are pending entries that aren't ready to sync
  // (likely stuck with permanent errors or rate limits)
  const showForceSync = pendingCount > 0 && readyCount === 0;

  return (
    <div className="action-bar">
      <button
        className="btn btn-secondary"
        onClick={onRefresh}
        disabled={isRefreshing || !isOnline}
        title={!isOnline ? 'Refresh disabled while offline' : undefined}
      >
        {isRefreshing ? 'Refreshing...' : 'Refresh'}
      </button>
      <button
        className="btn btn-secondary"
        onClick={onClear}
        disabled={!hasChanges}
      >
        Clear
      </button>
      <button
        className="btn btn-success"
        onClick={onSubmit}
        disabled={!hasChanges}
      >
        Add Scores
      </button>
      {readyCount > 0 && (
        <button
          className="btn btn-primary"
          onClick={onSyncNow}
          title="Sync pending scores now"
        >
          Sync Now ({readyCount})
        </button>
      )}
      {showForceSync && (
        <button
          className="btn btn-warning"
          onClick={onForceSync}
          title="Clear permanent errors and retry (use with caution)"
        >
          Force Sync ({pendingCount})
        </button>
      )}
    </div>
  );
}
