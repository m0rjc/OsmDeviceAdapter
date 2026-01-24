interface ScoreActionsProps {
  /** Whether user has entered any points (enables Clear and Submit buttons) */
  hasChanges: boolean;
  /** Whether scores are currently being refreshed from server */
  isRefreshing: boolean;
  /** Whether the app is online (disables Refresh when offline) */
  isOnline: boolean;
  /** Callback to refresh scores from server */
  onRefresh: () => void;
  /** Callback to clear all user-entered points */
  onClear: () => void;
  /** Callback to submit score changes */
  onSubmit: () => void;
}

/**
 * Action button bar for score entry.
 *
 * Provides three actions:
 * - Refresh: Fetches latest scores from server (disabled when offline or refreshing)
 * - Clear: Resets all user entries to zero (disabled when no changes)
 * - Add Scores: Submits score changes (disabled when no changes)
 *
 * Buttons are automatically enabled/disabled based on state.
 */
export function ScoreActions({
  hasChanges,
  isRefreshing,
  isOnline,
  onRefresh,
  onClear,
  onSubmit,
}: ScoreActionsProps) {
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
    </div>
  );
}
