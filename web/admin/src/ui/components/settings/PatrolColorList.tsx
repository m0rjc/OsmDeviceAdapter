import type { PatrolInfo } from '../../state';
import { PatrolColorRow } from './PatrolColorRow';

interface PatrolColorListProps {
  patrols: PatrolInfo[];
  patrolColors: Record<string, string>;
  onChange: (patrolId: string, color: string | null) => void;
  disabled?: boolean;
}

/**
 * List of patrol color configuration rows.
 */
export function PatrolColorList({ patrols, patrolColors, onChange, disabled }: PatrolColorListProps) {
  if (patrols.length === 0) {
    return (
      <div className="patrol-color-empty">
        No patrols found in this section.
      </div>
    );
  }

  return (
    <div className="patrol-color-list">
      {patrols.map((patrol) => (
        <PatrolColorRow
          key={patrol.id}
          patrol={patrol}
          color={patrolColors[patrol.id]}
          onChange={onChange}
          disabled={disabled}
        />
      ))}
    </div>
  );
}
