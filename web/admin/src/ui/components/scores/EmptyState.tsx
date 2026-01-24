interface EmptyStateProps {
  /** Main heading to display */
  title: string;
  /** Descriptive message explaining the empty state */
  message: string;
  /** Optional action button (e.g., "Retry" for error states) */
  action?: {
    label: string;
    onClick: () => void;
  };
}

/**
 * Generic empty state component for displaying placeholder content.
 *
 * Used for various scenarios:
 * - No section selected
 * - No patrols available
 * - Error states with retry action
 * - Loading states without data
 *
 * Displays a centered card with title, message, and optional action button.
 *
 * @example
 * ```tsx
 * <EmptyState
 *   title="No Section Selected"
 *   message="Please select a section to view scores."
 * />
 *
 * <EmptyState
 *   title="Error Loading Scores"
 *   message={error}
 *   action={{ label: 'Retry', onClick: handleRetry }}
 * />
 * ```
 */
export function EmptyState({ title, message, action }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <h3>{title}</h3>
      <p>{message}</p>
      {action && (
        <button className="btn btn-primary" onClick={action.onClick} style={{ marginTop: '1rem' }}>
          {action.label}
        </button>
      )}
    </div>
  );
}
