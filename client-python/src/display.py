"""LED Matrix Display Module for Scoreboard

Handles all LED matrix display operations for the 64x32 Adafruit RGB Matrix HAT.
"""

import time
from typing import List, Tuple, Optional
try:
    from rgbmatrix import RGBMatrix, RGBMatrixOptions, graphics
    MATRIX_AVAILABLE = True
except ImportError:
    MATRIX_AVAILABLE = False
    print("WARNING: rgbmatrix library not available. Running in simulation mode.")


class PatrolScore:
    """Represents a patrol and its score."""
    def __init__(self, name: str, score: int):
        self.name = name
        self.score = score


class MatrixDisplay:
    """Manages the LED matrix display for the scoreboard."""

    def __init__(self, rows: int = 32, cols: int = 64, simulate: bool = False):
        """Initialize the LED matrix.

        Args:
            rows: Number of LED rows (default: 32)
            cols: Number of LED columns (default: 64)
            simulate: If True, run in simulation mode without hardware
        """
        self.rows = rows
        self.cols = cols
        self.simulate = simulate or not MATRIX_AVAILABLE

        if not self.simulate:
            # Configure the matrix
            options = RGBMatrixOptions()
            options.rows = rows
            options.cols = cols
            options.chain_length = 1
            options.parallel = 1
            options.hardware_mapping = 'adafruit-hat'
            options.gpio_slowdown = 2  # Adjust if flickering occurs
            options.brightness = 60  # 0-100
            options.pwm_lsb_nanoseconds = 130  # Lower values = higher refresh rate

            self.matrix = RGBMatrix(options=options)
            self.canvas = self.matrix.CreateFrameCanvas()

            # Load fonts
            self.font = graphics.Font()
            self.font.LoadFont("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
            self.small_font = graphics.Font()
            self.small_font.LoadFont("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
        else:
            print(f"[DISPLAY] Simulation mode: {cols}x{rows} matrix")
            self.matrix = None
            self.canvas = None

    def clear(self):
        """Clear the display."""
        if not self.simulate:
            self.canvas.Clear()
        else:
            print("[DISPLAY] Clear")

    def show(self):
        """Update the display with current canvas content."""
        if not self.simulate:
            self.canvas = self.matrix.SwapOnVSync(self.canvas)

    def draw_text(self, x: int, y: int, text: str,
                  color: Tuple[int, int, int] = (255, 255, 255),
                  font_size: str = "normal"):
        """Draw text at specified position.

        Args:
            x: X coordinate (0 = left edge)
            y: Y coordinate (baseline of text, not top)
            text: Text to display
            color: RGB tuple (0-255 each)
            font_size: "normal" or "small"
        """
        if not self.simulate:
            font = self.font if font_size == "normal" else self.small_font
            text_color = graphics.Color(color[0], color[1], color[2])
            graphics.DrawText(self.canvas, font, x, y, text_color, text)
        else:
            print(f"[DISPLAY] Text at ({x},{y}): '{text}' color={color}")

    def draw_line(self, x1: int, y1: int, x2: int, y2: int,
                  color: Tuple[int, int, int] = (100, 100, 100)):
        """Draw a line between two points.

        Args:
            x1, y1: Start coordinates
            x2, y2: End coordinates
            color: RGB tuple
        """
        if not self.simulate:
            line_color = graphics.Color(color[0], color[1], color[2])
            graphics.DrawLine(self.canvas, x1, y1, x2, y2, line_color)
        else:
            print(f"[DISPLAY] Line from ({x1},{y1}) to ({x2},{y2})")

    def show_device_code(self, code: str, url: str):
        """Display the device authorization code.

        Args:
            code: User code to display (e.g., "ABCD-EFGH")
            url: Verification URL
        """
        self.clear()

        # Draw title
        self.draw_text(2, 6, "Enter Code:", color=(0, 255, 255))

        # Draw the code prominently
        self.draw_text(4, 20, code, color=(255, 255, 0))

        # Draw URL hint (may be truncated)
        url_short = url.replace("https://", "").replace("http://", "")
        if len(url_short) > 12:
            url_short = url_short[:12] + "..."
        self.draw_text(2, 30, url_short, color=(100, 100, 100))

        self.show()

        if self.simulate:
            print(f"\n{'='*40}")
            print(f"DEVICE CODE: {code}")
            print(f"Visit: {url}")
            print(f"{'='*40}\n")

    def show_waiting(self, message: str = "Waiting..."):
        """Display a waiting message.

        Args:
            message: Message to display
        """
        self.clear()
        self.draw_text(2, 16, message, color=(255, 200, 0))
        self.show()

        if self.simulate:
            print(f"[DISPLAY] {message}")

    def show_error(self, error: str):
        """Display an error message.

        Args:
            error: Error message to display
        """
        self.clear()

        # Draw "ERROR" in red
        self.draw_text(2, 8, "ERROR:", color=(255, 0, 0))

        # Draw error message, truncated if needed
        if len(error) > 11:
            error = error[:11] + "..."
        self.draw_text(2, 24, error, color=(255, 100, 100))

        self.show()

        if self.simulate:
            print(f"[DISPLAY ERROR] {error}")

    def show_scores(self, patrols: List[PatrolScore]):
        """Display patrol names and scores.

        Args:
            patrols: List of PatrolScore objects (up to 4)
        """
        self.clear()

        # Display up to 4 patrols
        row_height = 8
        for i, patrol in enumerate(patrols[:4]):
            y = (i * row_height) + 7  # Baseline for text

            # Draw patrol name (left justified)
            name = patrol.name
            if len(name) > 8:  # Truncate long names
                name = name[:8]
            self.draw_text(1, y, name, color=(0, 255, 0))

            # Draw score (right justified)
            score_text = str(patrol.score)
            # Estimate text width (rough: 5 pixels per character)
            score_width = len(score_text) * 5
            score_x = self.cols - score_width - 1
            self.draw_text(score_x, y, score_text, color=(255, 255, 0))

            # Draw separator line between patrols
            if i < len(patrols) - 1:
                line_y = (i + 1) * row_height - 1
                self.draw_line(0, line_y, self.cols - 1, line_y, color=(50, 50, 50))

        self.show()

        if self.simulate:
            print("\n" + "="*40)
            print("SCOREBOARD")
            print("="*40)
            for patrol in patrols[:4]:
                print(f"{patrol.name:<20} {patrol.score:>10}")
            print("="*40 + "\n")

    def show_message(self, message: str, color: Tuple[int, int, int] = (255, 255, 255)):
        """Display a centered message.

        Args:
            message: Message to display
            color: RGB color tuple
        """
        self.clear()
        # Rough centering (assumes ~5 pixels per character)
        text_width = len(message) * 5
        x = max(0, (self.cols - text_width) // 2)
        y = self.rows // 2
        self.draw_text(x, y, message, color=color)
        self.show()

        if self.simulate:
            print(f"[DISPLAY] {message}")

    def cleanup(self):
        """Clean up resources."""
        if not self.simulate and self.matrix:
            self.clear()
            self.show()
