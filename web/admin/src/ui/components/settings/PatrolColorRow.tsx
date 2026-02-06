import type { PatrolInfo } from '../../state';

// Predefined color palette based on primary and secondary colors
// The stored color represents the hue/theme - device firmware controls actual brightness
export const COLOR_PALETTE = [
  { value: '#FF0000', label: 'Red' },
  { value: '#00FF00', label: 'Green' },
  { value: '#0000FF', label: 'Blue' },
  { value: '#FFFF00', label: 'Yellow' },
  { value: '#00FFFF', label: 'Cyan' },
  { value: '#FF00FF', label: 'Magenta' },
  { value: '#FF8000', label: 'Orange' },
  { value: '#FFFFFF', label: 'White' },
] as const;

interface PatrolColorRowProps {
  patrol: PatrolInfo;
  color: string | undefined;
  onChange: (patrolId: string, color: string | null) => void;
  disabled?: boolean;
}

/**
 * A single row in the patrol color list showing patrol name and color selector.
 */
export function PatrolColorRow({ patrol, color, onChange, disabled }: PatrolColorRowProps) {
  const handleColorChange = (newColor: string) => {
    // Empty string means "None" / unset
    onChange(patrol.id, newColor === '' ? null : newColor);
  };

  return (
    <div className="patrol-color-row">
      <span className="patrol-color-name">{patrol.name}</span>
      <div className="patrol-color-picker">
        <select
          value={color ?? ''}
          onChange={(e) => handleColorChange(e.target.value)}
          disabled={disabled}
          className="patrol-color-select"
        >
          <option value="">None</option>
          {COLOR_PALETTE.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
        {color && (
          <span
            className="patrol-color-preview"
            style={{ backgroundColor: color }}
            title={color}
          />
        )}
      </div>
    </div>
  );
}
