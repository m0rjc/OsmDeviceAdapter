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

try:
    import qrcode
    from PIL import Image
    QR_AVAILABLE = True
except ImportError:
    QR_AVAILABLE = False
    if MATRIX_AVAILABLE:  # Only warn if we have the matrix but not QR
        print("WARNING: qrcode library not available. QR codes will not be displayed.")


class PatrolScore:
    """Represents a patrol and its score."""
    def __init__(self, name: str, score: int, pending_delta: int = 0, has_pending: bool = False):
        self.name = name
        self.score = score
        self.pending_delta = pending_delta
        self.has_pending = has_pending


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

            # Load BDF fonts (required by rgbmatrix library)
            # Try common font locations for rpi-rgb-led-matrix
            font_paths = [
                "/usr/local/share/fonts/",  # Common install location
                "/usr/share/fonts/",         # Alternative location
                "./fonts/",                  # Local fonts directory
            ]

            self.font = graphics.Font()
            self.small_font = graphics.Font()

            # Try to load fonts from known locations
            font_loaded = False
            for base_path in font_paths:
                try:
                    self.font.LoadFont(f"{base_path}7x13.bdf")  # Normal font
                    self.small_font.LoadFont(f"{base_path}5x7.bdf")  # Small font
                    font_loaded = True
                    print(f"[DISPLAY] Loaded fonts from {base_path}")
                    break
                except Exception:
                    continue

            if not font_loaded:
                print("WARNING: Could not load BDF fonts. Text may not display.")
                print("Install rpi-rgb-led-matrix fonts or specify font path.")
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

    def draw_status_indicator(self, rate_limit_state: str):
        """Draw a 1x1 status indicator in the top-right corner.

        Args:
            rate_limit_state: One of "LOADING", "NONE", "DEGRADED",
                             "USER_TEMPORARY_BLOCK", or "SERVICE_BLOCKED"
        """
        # Status colors
        colors = {
            "LOADING": (128, 128, 128),        # Grey
            "NONE": (0, 255, 0),               # Green
            "DEGRADED": (255, 191, 0),         # Amber
            "USER_TEMPORARY_BLOCK": (255, 0, 0),  # Red
            "SERVICE_BLOCKED": (255, 0, 0),    # Red (but will show message)
        }

        color = colors.get(rate_limit_state, (128, 128, 128))  # Default to grey

        # Draw 1x1 pixel in top-right corner
        x = self.cols - 2  # 2 pixels from right edge
        y = 1              # 1 pixel from top

        if not self.simulate:
            status_color = graphics.Color(color[0], color[1], color[2])
            self.canvas.SetPixel(x, y, status_color.red, status_color.green, status_color.blue)
        else:
            print(f"[DISPLAY] Status indicator: {rate_limit_state} at ({x},{y}) color={color}")

    def generate_qr_image(self, url: str):
        """Generate a QR code image for the given URL.

        Args:
            url: The URL to encode in the QR code

        Returns:
            PIL Image object containing the QR code (32x32 pixels - black on white with border)
        """
        if not QR_AVAILABLE:
            # Return a blank 32x32 white image if QR library not available
            return Image.new('RGB', (32, 32), color=(255, 255, 255))

        try:
            # Try version 2 with 2-pixel border (25+4=29px) for better quiet zone
            qr = qrcode.QRCode(
                version=2,  # 25x25 modules
                error_correction=qrcode.constants.ERROR_CORRECT_L,
                box_size=1,  # 1 pixel per module
                border=2,    # 2-pixel white border (quiet zone for scanning)
            )
            qr.add_data(url)
            qr.make(fit=False)  # Don't auto-adjust, fail if URL too long

            img = qr.make_image(fill_color="black", back_color="white")

            # Version 2 + border 2 = 29x29, center in 32x32
            padded = Image.new('RGB', (32, 32), color=(255, 255, 255))
            padded.paste(img, (1, 1))  # Center with 1-2px padding
            return padded

        except Exception as version2_error:
            # If version 2 too small, fall back to version 3 with smaller border
            try:
                qr = qrcode.QRCode(
                    version=3,  # 29x29 modules - fits longer URLs
                    error_correction=qrcode.constants.ERROR_CORRECT_L,
                    box_size=1,  # 1 pixel per module
                    border=1,    # 1-pixel white border (minimal quiet zone)
                )
                qr.add_data(url)
                qr.make(fit=False)

                img = qr.make_image(fill_color="black", back_color="white")

                # Version 3 + border 1 = 31x31, center in 32x32
                padded = Image.new('RGB', (32, 32), color=(255, 255, 255))
                padded.paste(img, (0, 0))
                return padded

            except Exception as e:
                print(f"ERROR: Failed to generate QR code: {e}")
                print(f"URL may be too long ({len(url)} chars): {url}")
                # Return a blank 32x32 white image as fallback
                return Image.new('RGB', (32, 32), color=(255, 255, 255))

    def show_device_code(self, code: str, url: str, url_short: Optional[str] = None):
        """Display the device authorization code with QR code and text.

        Args:
            code: User code to display (e.g., "MRHQ-TDY4")
            url: Verification URL (basic URL for fallback display)
            url_short: Short verification URL for QR code (e.g., /d/MRHQTDY4)
        """
        self.clear()

        # Use QR code layout if available and url_short provided
        if QR_AVAILABLE and url_short and not self.simulate:
            # Generate and display QR code on left side (32x32 at position 0,0)
            qr_img = self.generate_qr_image(url_short)
            self.canvas.SetImage(qr_img.convert('RGB'), 0, 0)

            # Display wrapped device code on right side
            # Code format: "MRHQ-TDY4" (9 chars total)
            # Split into two lines to fit in 30px width
            self.draw_text(34, 10, code[:4], color=(255, 255, 0))      # "MRHQ"
            self.draw_text(34, 20, code[5:], color=(255, 255, 0))      # "TDY4" (skip hyphen)

            self.show()

        elif self.simulate and QR_AVAILABLE and url_short:
            # Simulation mode with QR capability - print both
            print(f"\n{'='*50}")
            print(f"DEVICE AUTHORIZATION")
            print(f"{'='*50}")
            print(f"Scan QR code or visit: {url}")
            print(f"Short link: {url_short}")
            print(f"\nDevice Code: {code}")
            print(f"  Line 1: {code[:4]}")
            print(f"  Line 2: {code[5:]}")
            print(f"{'='*50}\n")

            # Generate ASCII QR for terminal display
            try:
                qr = qrcode.QRCode()
                qr.add_data(url_short)
                qr.make(fit=True)
                qr.print_ascii(invert=True)
            except Exception as e:
                print(f"Could not generate ASCII QR: {e}")

        else:
            # Fallback to text-only display (QR not available or no complete URL)
            self.draw_text(2, 6, "Enter Code:", color=(0, 255, 255))
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

    def show_scores(self, patrols: List[PatrolScore], rate_limit_state: str = "NONE"):
        """Display patrol names and scores with status indicator.

        Args:
            patrols: List of PatrolScore objects (up to 4)
            rate_limit_state: Current rate limit state for status indicator
        """
        self.clear()

        # Special handling for service blocked - show message instead of scores
        if rate_limit_state == "SERVICE_BLOCKED":
            self.draw_text(2, 10, "Service", color=(255, 0, 0))
            self.draw_text(2, 20, "Blocked", color=(255, 0, 0))
            self.draw_text(2, 28, "Contact", color=(255, 100, 0))
            self.draw_text(2, 32, "Admin", color=(255, 100, 0))
            self.draw_status_indicator(rate_limit_state)
            self.show()

            if self.simulate:
                print("\n" + "="*40)
                print("SERVICE BLOCKED - Contact Administrator")
                print("="*40 + "\n")
            return

        # Display up to 4 patrols
        row_height = 8
        for i, patrol in enumerate(patrols[:4]):
            y = (i * row_height) + 8  # Baseline for text (moved down slightly)

            # Draw patrol name (left justified, using small font)
            name = patrol.name
            if len(name) > 11:  # Truncate long names (small font fits more)
                name = name[:11]
            self.draw_text(1, y, name, color=(0, 255, 0), font_size="small")

            # Draw score (right justified, using small font)
            score_text = str(patrol.score)
            # Small font is 5 pixels wide per character
            score_width = len(score_text) * 5
            score_x = self.cols - score_width - 2  # Extra padding from edge
            self.draw_text(score_x, y, score_text, color=(255, 255, 0), font_size="small")

            # Draw orange pending indicator dot if patrol has pending changes
            if patrol.has_pending:
                pending_x = self.cols - 1  # Right edge
                pending_y = (i * row_height) + 4  # Vertically centered in row
                if not self.simulate:
                    self.canvas.SetPixel(pending_x, pending_y, 255, 140, 0)  # Orange color
                else:
                    print(f"[DISPLAY] Pending indicator at ({pending_x},{pending_y}) for {patrol.name}")

        # Draw status indicator
        self.draw_status_indicator(rate_limit_state)

        self.show()

        if self.simulate:
            print("\n" + "="*40)
            print("SCOREBOARD")
            print("="*40)
            for patrol in patrols[:4]:
                pending_indicator = " [PENDING]" if patrol.has_pending else ""
                print(f"{patrol.name:<20} {patrol.score:>10}{pending_indicator}")
            print(f"\nStatus: {rate_limit_state}")
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
