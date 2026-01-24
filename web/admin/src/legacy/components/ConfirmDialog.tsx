interface Change {
  patrolId: string;
  patrolName: string;
  points: number;
}

interface ConfirmDialogProps {
  changes: Change[];
  isSubmitting: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({ changes, isSubmitting, onConfirm, onCancel }: ConfirmDialogProps) {
  const formatPoints = (points: number): string => {
    if (points > 0) return `+${points}`;
    return String(points);
  };

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <h3>Confirm Score Changes</h3>
        </div>
        <div className="modal-body">
          <p style={{ marginBottom: '1rem', color: 'var(--color-text-muted)' }}>
            The following changes will be applied:
          </p>
          <ul className="confirmation-list">
            {changes.map(change => (
              <li key={change.patrolId} className="confirmation-item">
                <span className="confirmation-patrol">{change.patrolName}</span>
                <span
                  className={`confirmation-points ${change.points > 0 ? 'positive' : 'negative'}`}
                >
                  {formatPoints(change.points)} points
                </span>
              </li>
            ))}
          </ul>
        </div>
        <div className="modal-footer">
          <button
            className="btn btn-secondary"
            onClick={onCancel}
            disabled={isSubmitting}
          >
            Cancel
          </button>
          <button
            className="btn btn-success"
            onClick={onConfirm}
            disabled={isSubmitting}
          >
            {isSubmitting ? 'Saving...' : 'Confirm'}
          </button>
        </div>
      </div>
    </div>
  );
}
