import type { UIPatrol } from '../../state';

interface PatrolCardProps {
  /** The patrol data including current scores and user entry */
  patrol: UIPatrol;
  /** Points queued in IndexedDB for offline sync (not yet sent to server) */
  pendingPoints: number;
  /** Callback when user changes the points input field */
  onPointsChange: (patrolId: string, value: string) => void;
}

/**
 * Displays a single patrol's score information and input field.
 *
 * Shows:
 * - Patrol name with pending sync badge if applicable
 * - Current committed score
 * - Projected new score (if user has entered points or there are pending points)
 * - Input field for entering points to add
 *
 * The input field clamps values to [-1000, 1000] range.
 */
export function PatrolCard({ patrol, pendingPoints, onPointsChange }: PatrolCardProps) {
  const totalScore = patrol.committedScore + patrol.pendingScore + patrol.userEntry + pendingPoints;
  const hasChanges = patrol.userEntry !== 0 || pendingPoints !== 0;

  return (
    <div className={`patrol-card${pendingPoints ? ' has-pending' : ''}`}>
      <div className="patrol-card-header">
        <span className="patrol-name">
          {patrol.name}
          {pendingPoints !== 0 && (
            <span className="patrol-pending-badge" title="Pending sync">
              {pendingPoints > 0 ? '+' : ''}{pendingPoints}
            </span>
          )}
        </span>
        <span className="patrol-current-score">{patrol.committedScore}</span>
        {hasChanges && (
          <span className="patrol-new-score">
            {totalScore}
          </span>
        )}
      </div>
      <div className="patrol-card-body">
        <div className="patrol-input">
          <span className="patrol-input-label">Add points:</span>
          <input
            type="number"
            min={-1000}
            max={1000}
            value={patrol.userEntry === 0 ? '' : patrol.userEntry}
            onChange={e => onPointsChange(patrol.id, e.target.value)}
            placeholder="0"
            className={
              patrol.userEntry > 0
                ? 'positive'
                : patrol.userEntry < 0
                ? 'negative'
                : ''
            }
          />
        </div>
      </div>
    </div>
  );
}
