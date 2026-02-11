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
  /** Optional CSS class for styling variants (e.g., 'error-state') */
  className?: string;
}

/**
 * Generic message card component for displaying placeholder content and persistent errors.
 *
 * Used for various scenarios:
 * - No section selected
 * - No patrols available
 * - Error states with retry action (persistent, blocks UI)
 * - Loading states without data
 *
 * Displays a centered card with title, message, and optional action button.
 *
 * Note: This component is for persistent states that occupy the main content area.
 * For transient, dismissable errors (e.g., service errors), see ErrorDialog component.
 *
 * @example
 * ```tsx
 * <MessageCard
 *   title="No Section Selected"
 *   message="Please select a section to view scores."
 * />
 *
 * <MessageCard
 *   title="Error Loading Scores"
 *   message={error}
 *   action={{ label: 'Retry', onClick: handleRetry }}
 * />
 * ```
 */
export function MessageCard({ title, message, action, className }: EmptyStateProps) {
  const classNames = ['empty-state', className].filter(Boolean).join(' ');

  return (
    <div className={classNames}>
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
