import type { PatrolInfo } from '../../state';

// Predefined color palette based on primary and secondary colors
// The stored value is the color name (used as CSS class suffix for theming)
export const COLOR_PALETTE = [
  { value: 'red', label: 'Red', hex: '#FF0000' },
  { value: 'green', label: 'Green', hex: '#00FF00' },
  { value: 'blue', label: 'Blue', hex: '#0000FF' },
  { value: 'yellow', label: 'Yellow', hex: '#FFFF00' },
  { value: 'cyan', label: 'Cyan', hex: '#00FFFF' },
  { value: 'magenta', label: 'Magenta', hex: '#FF00FF' },
  { value: 'orange', label: 'Orange', hex: '#FF8000' },
  { value: 'white', label: 'White', hex: '#FFFFFF' },
] as const;

/** Maps color name to hex for preview swatches */
const COLOR_HEX_MAP: Record<string, string> = Object.fromEntries(
    COLOR_PALETTE.map(c => [c.value, c.hex])
);

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
            style={{ backgroundColor: COLOR_HEX_MAP[color] ?? color }}
            title={color}
          />
        )}
      </div>
    </div>
  );
}
