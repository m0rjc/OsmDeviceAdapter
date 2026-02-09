# Bar Graph Display Design

Design for LED bar graph score display on the patrol scoreboard device.

## Physical Layout

The display is a 64x32 LED matrix supporting up to 4 patrols. Each patrol gets **8 rows** (32 / 4), containing both the text (patrol name and score) and a bar graph rendered behind it.

The bar graph occupies a **48x5 LED rectangle** (240 LEDs total) within each patrol's 8-row strip. The bar is drawn at reduced brightness behind the text, so text remains the primary information and the bar serves as a quick visual indicator. The text will partially obscure the rightmost lit LEDs of the bar, but this is acceptable — the exact score is always readable as a number.

The 5-row bar height was chosen as a factor of 10 for easy mental mapping in decimal, while fitting within the 8-row budget alongside text. 10 rows would give a nicer bar but won't fit 4 patrols in 32 rows. With fewer patrols (e.g. 3), a taller bar and larger font would be possible, but the added complexity of switching layouts dynamically isn't worthwhile — design for the 4-patrol case and leave unused strips empty.

Each patrol's bar is coloured according to its configured theme colour.

## LED Fill Order

LEDs fill from the bottom of each column upward, creating a bar that grows horizontally left-to-right. Each column fills bottom-to-top before moving to the next column.

For a 5-row bar (rows indexed 0-4, where row 4 is the bottom), the fill order within each column is:

```
Row 4 (bottom)  -> 1st
Row 3            -> 2nd
Row 2            -> 3rd
Row 1            -> 4th
Row 0 (top)      -> 5th
```

So the sequence for the first 10 LEDs is:

```
(0,4), (0,3), (0,2), (0,1), (0,0),   <- column 0 filled
(1,4), (1,3), (1,2), (1,1), (1,0),   <- column 1 filled
...
```

In general, LED number `n` (0-indexed) maps to:

```
column = n / 5          (integer division)
row    = 4 - (n % 5)    (bottom-up)
```

This gives a natural "filling up" visual, like a liquid level or thermometer turned on its side.

### Visual Example (12 points, showing first 3 columns)

```
col:  0  1  2
row 0: #  #  .       <- 5th per column (top)
row 1: #  #  .       <- 4th per column
row 2: #  #  #       <- 3rd per column
row 3: #  #  #       <- 2nd per column
row 4: #  #  #       <- 1st per column (bottom)
```

12 LEDs = 2 full columns (10) + 2 into column 2 (bottom + row 3).

## Broken Axis (Score Offset)

When all patrol scores fit within 240, display them directly (1 LED = 1 point). When scores exceed 240, subtract an offset so the bars fit.

### Offset Calculation

**On power-on / first data load:**

```
if max_score > 240:
    offset = max_score - 200
else:
    offset = 0
```

Setting the top score to 200 (not 240) leaves a 40-point buffer, reducing the chance of needing to recalculate during the evening.

**During the session:**

The offset is held constant. Bars only grow, never jump. Recalculate only if forced:

```
if any score - offset > 240:
    offset = max_score - 200
```

This should be rare — it requires 40+ points gained beyond the leader's starting score.

**Display score for each patrol:**

```
display_score = max(0, score - offset)
```

Clamping to 0 handles patrols that are far behind the leader (they show as empty with the broken-axis indicator making it clear the graph is truncated).

### Zigzag Indicator

When `offset > 0`, draw a zigzag pattern in column 0 to indicate a broken axis (the bars don't start at zero). Column 0 is then not available for score LEDs, so the effective bar width becomes 47 columns (235 LEDs) — the offset calculation using 200 as the target ensures this always fits.

Zigzag pattern for column 0 (5 rows):

```
row 0: ON
row 1:  OFF
row 2: ON
row 3:  OFF
row 4: ON
```

This is the standard broken-axis convention (a zigzag/cut line) translated to a 5-pixel column. Use a neutral colour (e.g. white or dim grey) distinct from the patrol theme colour.

When the zigzag is shown, bar fill starts from column 1 instead of column 0.

## Patrol Theme Colours

Each patrol can have a configured theme colour (stored by name: `red`, `green`, `blue`, `yellow`, `cyan`, `magenta`, `orange`, `white`). The bar LEDs are drawn in this colour. Patrols without a colour assigned use a default (e.g. white or green).

The colour names map to RGB values for the LED driver:

| Name     | RGB            |
|----------|----------------|
| red      | (255, 0, 0)    |
| green    | (0, 255, 0)    |
| blue     | (0, 0, 255)    |
| yellow   | (255, 255, 0)  |
| cyan     | (0, 255, 255)  |
| magenta  | (255, 0, 255)  |
| orange   | (255, 128, 0)  |
| white    | (255, 255, 255) |

These match the `COLOR_PALETTE` defined in the admin UI (`web/admin/src/ui/components/settings/PatrolColorRow.tsx`). The server stores and returns the colour name string; the firmware maps it to an RGB value.

## API Integration

Patrol colours are returned by the server alongside score data. The firmware should:

1. Fetch scores from `GET /api/v1/patrols`
2. Use the patrol's configured colour for its bar
3. Calculate the offset on first successful fetch
4. Hold the offset stable for the session

## Brightness and Power

LED bars light a significant number of LEDs simultaneously (up to 240 per patrol, 960 for 4 patrols). This has two implications:

**Readability:** Patrol name and score text must remain legible over or alongside the bar. Keep bars at a lower brightness than the text — e.g. bars at 25-30% brightness with text at full brightness. This creates a clear visual hierarchy where the bar is a background indicator and the text remains the primary information.

**Power draw:** Each lit LED draws current. On battery power, a full bar graph (4 patrols, all near 240 LEDs) represents a substantial load. Strategies to manage this:

- Cap bar brightness (e.g. 20-30% max) — this is the simplest and most effective control
- Use dimmer variants of theme colours for the bar fill rather than full-saturation RGB values
- Consider the worst case: 4 patrols x 240 LEDs = 960 lit LEDs at whatever brightness level is chosen

The exact brightness values will need tuning on real hardware, balancing visibility in a scout hall (which can range from well-lit to quite dim) against battery life for portable use.

## Implementation Notes

- The fill algorithm is the same regardless of display size — only the constants change (48 columns, 5 rows, 240 max)
- The offset logic is display-side only; the server always returns actual scores
- The zigzag column costs 5 LEDs of display space but only activates when scores exceed 240 anyway
