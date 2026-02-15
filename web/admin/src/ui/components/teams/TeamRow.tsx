import { useState, useCallback } from 'react';
import type { AdhocTeam } from '../../state';
import { COLOR_PALETTE } from '../settings/PatrolColorRow';

interface TeamRowProps {
  team: AdhocTeam;
  onUpdate: (id: string, name: string, color: string) => void;
  onDelete: (id: string) => void;
  disabled?: boolean;
}

const COLOR_HEX_MAP: Record<string, string> = Object.fromEntries(
    COLOR_PALETTE.map(c => [c.value, c.hex])
);

export function TeamRow({ team, onUpdate, onDelete, disabled }: TeamRowProps) {
  const [name, setName] = useState(team.name);
  const [editing, setEditing] = useState(false);

  const handleBlur = useCallback(() => {
    const trimmed = name.trim();
    if (trimmed && trimmed !== team.name) {
      onUpdate(team.id, trimmed, team.color);
    } else {
      setName(team.name); // Revert
    }
    setEditing(false);
  }, [name, team, onUpdate]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      (e.target as HTMLInputElement).blur();
    } else if (e.key === 'Escape') {
      setName(team.name);
      setEditing(false);
    }
  }, [team.name]);

  const handleColorChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    onUpdate(team.id, team.name, e.target.value);
  }, [team, onUpdate]);

  return (
    <div className="team-row">
      <div className="team-row-name">
        {editing ? (
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={handleBlur}
            onKeyDown={handleKeyDown}
            maxLength={50}
            disabled={disabled}
            autoFocus
            className="team-name-input"
          />
        ) : (
          <span
            className="team-name-display"
            onClick={() => !disabled && setEditing(true)}
            title="Click to edit"
          >
            {team.name}
          </span>
        )}
      </div>

      <div className="team-row-score">
        {team.score}
      </div>

      <div className="team-row-color">
        <select
          value={team.color}
          onChange={handleColorChange}
          disabled={disabled}
          className="patrol-color-select"
        >
          <option value="">None</option>
          {COLOR_PALETTE.map((c) => (
            <option key={c.value} value={c.value}>{c.label}</option>
          ))}
        </select>
        {team.color && (
          <span
            className="patrol-color-preview"
            style={{ backgroundColor: COLOR_HEX_MAP[team.color] ?? team.color }}
          />
        )}
      </div>

      <button
        className="btn btn-danger btn-sm"
        onClick={() => onDelete(team.id)}
        disabled={disabled}
        title="Delete team"
      >
        Delete
      </button>
    </div>
  );
}
