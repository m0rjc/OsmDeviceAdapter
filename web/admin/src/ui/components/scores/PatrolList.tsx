import { PatrolCard } from './PatrolCard';
import type { UIPatrol } from '../../state';

interface PatrolListProps {
  /** Array of patrols to display */
  patrols: UIPatrol[];
  /** Map of patrol IDs to pending sync points from IndexedDB */
  pendingPointsMap: Map<string, number>;
  /** Callback when user changes points for any patrol */
  onPointsChange: (patrolId: string, value: string) => void;
}

/**
 * Renders a list of patrol score cards.
 *
 * Displays an empty state message if no patrols are available.
 * Otherwise renders a PatrolCard for each patrol in the array.
 */
export function PatrolList({ patrols, pendingPointsMap, onPointsChange }: PatrolListProps) {
  if (patrols.length === 0) {
    return (
      <div className="empty-state">
        <h3>No Patrols Found</h3>
        <p>This section doesn't have any patrols configured.</p>
      </div>
    );
  }

  return (
    <div className="patrol-cards">
      {patrols.map(patrol => {
        const pendingPoints = pendingPointsMap.get(patrol.id) || 0;
        return (
          <PatrolCard
            key={patrol.id}
            patrol={patrol}
            pendingPoints={pendingPoints}
            onPointsChange={onPointsChange}
          />
        );
      })}
    </div>
  );
}
