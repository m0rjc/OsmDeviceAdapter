"""LED Matrix Display Module for Scoreboard

Handles all LED matrix display operations for the 64x32 Adafruit RGB Matrix HAT.
Uses a PIL frame buffer for all rendering with 1-bit (no anti-aliasing) text,
which gives crisp pixel-perfect output on LED matrices where the gaps between
pixels make anti-aliased intermediate brightness look muddy.
"""

import os
import tempfile
import time
import math
from typing import Dict, List, Tuple, Optional
from PIL import Image, ImageDraw, ImageFont, BdfFontFile

try:
    from rgbmatrix import RGBMatrix, RGBMatrixOptions
    MATRIX_AVAILABLE = True
except ImportError:
    MATRIX_AVAILABLE = False
    print("WARNING: rgbmatrix library not available. Running in simulation mode.")

try:
    import qrcode
    QR_AVAILABLE = True
except ImportError:
    QR_AVAILABLE = False
    if MATRIX_AVAILABLE:  # Only warn if we have the matrix but not QR
        print("WARNING: qrcode library not available. QR codes will not be displayed.")


# Score color is consistent across all themes (reserved for future rising/falling indicators)
SCORE_COLOR = (255, 255, 0)

# Per-color theme palettes: bar (scaled by BAR_BRIGHTNESS), text (bright), border (subtle frame)
THEME_PALETTES = {
    "red":     {"bar": (255, 0, 0),     "text": (255, 80, 80),   "border": (100, 0, 0)},
    "green":   {"bar": (0, 255, 0),     "text": (80, 255, 80),   "border": (0, 100, 0)},
    "blue":    {"bar": (0, 0, 255),     "text": (80, 80, 255),   "border": (0, 0, 100)},
    "yellow":  {"bar": (255, 255, 0),   "text": (255, 255, 100), "border": (100, 100, 0)},
    "cyan":    {"bar": (0, 255, 255),   "text": (80, 255, 255),  "border": (0, 100, 100)},
    "magenta": {"bar": (255, 0, 255),   "text": (255, 80, 255),  "border": (100, 0, 100)},
    "orange":  {"bar": (255, 128, 0),   "text": (255, 160, 80),  "border": (100, 50, 0)},
    "white":   {"bar": (255, 255, 255), "text": (200, 200, 200), "border": (80, 80, 80)},
}

# Bar brightness scale factor (0.0-1.0). Keeps bars dimmer than text for readability.
BAR_BRIGHTNESS = 0.40

# Border brightness scale factor (0.0-1.0). Brighter than bars for a visible frame.
BORDER_BRIGHTNESS = 0.60

# Composite text colors: white at different intensities depending on whether
# the text pixel overlaps a lit bar pixel or a dark background pixel.
TEXT_ON_BAR = (255, 255, 255)   # 100% white over lit bar pixels
TEXT_ON_DARK = (180, 180, 180)  # 80% white over dark pixels

# Bar graph dimensions within each 8-row patrol strip
BAR_HEIGHT = 5   # rows
BAR_WIDTH = 48   # columns (max LEDs = BAR_WIDTH * BAR_HEIGHT = 240)
BAR_MAX_LEDS = BAR_WIDTH * BAR_HEIGHT  # 240

# Default bar color when patrol has no configured color
DEFAULT_BAR_COLOR = "green"

# BDF font search paths (pixel-perfect bitmap fonts for LED matrices)
BDF_FONT_DIRS = [
    "/usr/local/share/fonts/",  # Common install location
    "/usr/share/fonts/",         # Alternative location
    "./fonts/",                  # Local fonts directory
]

# TrueType fallback paths (used only if BDF fonts not available)
TTF_FONT_PATHS = [
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
    "/usr/share/fonts/truetype/freefont/FreeSans.ttf",
    "/usr/share/fonts/TTF/DejaVuSans.ttf",
]

# Fallback TrueType font sizes (at PIL's default 72 DPI, 1pt ≈ 1px)
SMALL_FONT_SIZE = 8
SMALL_FONT = "5x8.bdf"
NORMAL_FONT_SIZE = 9
NORMAL_FONT = "7x13.bdf"
LARGE_FONT_SIZE = 18
LARGE_FONT = "9x18B.bdf"

def _load_bdf_font(bdf_path: str) -> Optional[ImageFont.ImageFont]:
    """Convert a BDF font to PIL format and load it.

    PIL can't load BDF directly but has a converter. We convert to PIL's
    bitmap font format (.pil/.pbm) in a temp directory and load from there.
    """
    try:
        with open(bdf_path, "rb") as f:
            bdf = BdfFontFile.BdfFontFile(f)
            tmpdir = tempfile.mkdtemp(prefix="pilfonts_")
            pil_path = os.path.join(tmpdir, "font")
            bdf.save(pil_path)
            font = ImageFont.load(pil_path + ".pil")
            print(f"[DISPLAY] Loaded BDF font: {bdf_path}")
            return font
    except Exception as e:
        print(f"[DISPLAY] Failed to load BDF font {bdf_path}: {e}")
        return None


def _load_truetype_font(size: int) -> ImageFont.FreeTypeFont:
    """Try to load a TrueType font from common system locations."""
    for path in TTF_FONT_PATHS:
        try:
            return ImageFont.truetype(path, size)
        except (OSError, IOError):
            continue
    print(f"WARNING: No TrueType font found at size {size}, using PIL default bitmap font")
    return ImageFont.load_default()


def _load_fonts() -> Tuple[ImageFont.ImageFont, ImageFont.ImageFont, ImageFont.ImageFont]:
    """Load fonts, preferring BDF bitmap fonts over TrueType.

    Returns:
        (normal_font, small_font, large_font) tuple
    """
    # Try BDF fonts first (pixel-perfect for LED matrices)
    for base_path in BDF_FONT_DIRS:
        normal_path = os.path.join(base_path, NORMAL_FONT)
        small_path = os.path.join(base_path, SMALL_FONT)
        large_path = os.path.join(base_path, LARGE_FONT)
        if os.path.exists(normal_path) and os.path.exists(small_path) and os.path.exists(large_path):
            normal = _load_bdf_font(normal_path)
            small = _load_bdf_font(small_path)
            large = _load_bdf_font(large_path)
            if normal and small and large:
                return normal, small, large

    # Fall back to TrueType with 1-bit rendering
    print("[DISPLAY] BDF fonts not found, falling back to TrueType (1-bit mode)")
    return _load_truetype_font(NORMAL_FONT_SIZE), _load_truetype_font(SMALL_FONT_SIZE), _load_truetype_font(LARGE_FONT_SIZE)


class PatrolScore:
    """Represents a patrol and its score."""
    def __init__(self, name: str, score: int, patrol_id: str = ""):
        self.name = name
        self.score = score
        self.id = patrol_id


class MatrixDisplay:
    """Manages the LED matrix display for the scoreboard.

    All drawing operations render to a PIL Image frame buffer.
    Text uses TrueType fonts with anti-aliasing for smooth edges.
    The frame buffer is pushed to the hardware in show().
    """

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

        # PIL frame buffer - all drawing goes here
        self.frame = Image.new('RGB', (cols, rows), (0, 0, 0))
        self.draw = ImageDraw.Draw(self.frame)
        # 1-bit text rendering: every pixel fully on or off, no anti-aliasing.
        # Crisp and readable on LED matrices with visible gaps between pixels.
        self.draw.fontmode = "1"

        # Load fonts (BDF bitmap preferred, TrueType fallback)
        self.font, self.small_font, self.large_font = _load_fonts()

        if not self.simulate:
            # Configure the matrix
            options = RGBMatrixOptions()
            options.rows = rows
            options.cols = cols
            options.chain_length = 1
            options.parallel = 1
            options.hardware_mapping = 'adafruit-hat'
            options.gpio_slowdown = 4  # Adjust if flickering occurs
            options.brightness = 100  # 0-100
            options.pwm_bits = 4  # Fewer brightness levels = faster refresh, less flicker at low PWM
            options.pwm_lsb_nanoseconds = 130  # Lower values = higher refresh rate

            self.matrix = RGBMatrix(options=options)
            self.canvas = self.matrix.CreateFrameCanvas()
        else:
            print(f"[DISPLAY] Simulation mode: {cols}x{rows} matrix")
            self.matrix = None
            self.canvas = None

    def clear(self):
        """Clear the frame buffer."""
        self.draw.rectangle([(0, 0), (self.cols - 1, self.rows - 1)], fill=(0, 0, 0))
        if self.simulate:
            print("[DISPLAY] Clear")

    def show(self):
        """Push the frame buffer to the LED matrix."""
        if not self.simulate:
            self.canvas.SetImage(self.frame)
            self.canvas = self.matrix.SwapOnVSync(self.canvas)

    def draw_text(self, x: int, y: int, text: str,
                  color: Tuple[int, int, int] = (255, 255, 255),
                  font_size: str = "normal"):
        """Draw 1-bit crisp text at specified position.

        Args:
            x: X coordinate (0 = left edge)
            y: Y coordinate (baseline of text, matching BDF convention)
            text: Text to display
            color: RGB tuple (0-255 each)
            font_size: "normal", "small", or "large"
        """
        if font_size == "large":
            font = self.large_font
        elif font_size == "normal":
            font = self.font
        else:
            font = self.small_font
        # Convert baseline y to top-left y for PIL.
        # TrueType fonts have getmetrics(), BDF/PIL bitmap fonts do not.
        if hasattr(font, 'getmetrics'):
            ascent, descent = font.getmetrics()
            top_y = y - ascent
        else:
            # PIL bitmap font: getbbox gives us the bounding box
            bbox = font.getbbox(text)
            top_y = y - (bbox[3] - bbox[1])
        self.draw.text((x, top_y), text, fill=color, font=font)

        if self.simulate:
            print(f"[DISPLAY] Text at ({x},{y}): '{text}' color={color}")

    def text_width(self, text: str, font_size: str = "normal") -> int:
        """Get the pixel width of rendered text.

        Args:
            text: Text to measure
            font_size: "normal", "small", or "large"

        Returns:
            Width in pixels
        """
        if font_size == "large":
            font = self.large_font
        elif font_size == "normal":
            font = self.font
        else:
            font = self.small_font
        bbox = font.getbbox(text)
        return bbox[2] - bbox[0]

    def draw_line(self, x1: int, y1: int, x2: int, y2: int,
                  color: Tuple[int, int, int] = (100, 100, 100)):
        """Draw a line between two points.

        Args:
            x1, y1: Start coordinates
            x2, y2: End coordinates
            color: RGB tuple
        """
        self.draw.line([(x1, y1), (x2, y2)], fill=color)
        if self.simulate:
            print(f"[DISPLAY] Line from ({x1},{y1}) to ({x2},{y2})")

    def draw_status_indicator(self, rate_limit_state: str, ws_connected: bool = False):
        """Draw a status indicator pixel in the top-right corner.

        Args:
            rate_limit_state: One of "LOADING", "NONE", "DEGRADED",
                             "USER_TEMPORARY_BLOCK", or "SERVICE_BLOCKED"
            ws_connected: True when the device has an active WebSocket connection
                          to the server (shows blue instead of green when state is NONE)
        """
        # Status colors
        colors = {
            "LOADING": (128, 128, 128),           # Grey
            "NONE": (0, 255, 0),                  # Green (polling only)
            "DEGRADED": (255, 191, 0),            # Amber
            "USER_TEMPORARY_BLOCK": (255, 0, 0),  # Red
            "SERVICE_BLOCKED": (255, 0, 0),       # Red (but will show message)
        }

        color = colors.get(rate_limit_state, (128, 128, 128))  # Default to grey

        # Blue dot when WebSocket is open and all else is healthy
        if ws_connected and rate_limit_state == "NONE":
            color = (0, 0, 255)

        # Draw 1x1 pixel in top-right corner
        x = self.cols - 2  # 2 pixels from right edge
        y = 0
        self.frame.putpixel((x, y), color)

        if self.simulate:
            ws_label = " [WS]" if ws_connected else ""
            print(f"[DISPLAY] Status indicator: {rate_limit_state}{ws_label} at ({x},{y}) color={color}")

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
            # Generate QR code and paste onto frame buffer
            qr_img = self.generate_qr_image(url_short)
            self.frame.paste(qr_img.convert('RGB'), (0, 0))

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
            url_display = url.replace("https://", "").replace("http://", "")
            if len(url_display) > 12:
                url_display = url_display[:12] + "..."
            self.draw_text(2, 30, url_display, color=(100, 100, 100))

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

    def draw_composite_text(self, x: int, y: int, text: str,
                            font_size: str = "small",
                            color_on_lit: Tuple[int, int, int] = TEXT_ON_BAR,
                            color_on_dark: Tuple[int, int, int] = TEXT_ON_DARK):
        """Draw text composited with existing frame content.

        Text pixels over lit (non-black) bar pixels get color_on_lit,
        text pixels over dark background get color_on_dark. This gives
        a subtle brightness difference that improves readability.

        Args:
            x: X coordinate (0 = left edge)
            y: Y coordinate (baseline of text)
            text: Text to display
            font_size: "normal", "small", or "large"
            color_on_lit: RGB color where text overlaps a lit pixel
            color_on_dark: RGB color where text overlaps a dark pixel
        """
        if font_size == "large":
            font = self.large_font
        elif font_size == "normal":
            font = self.font
        else:
            font = self.small_font

        # Compute top_y (same logic as draw_text)
        if hasattr(font, 'getmetrics'):
            ascent, _ = font.getmetrics()
            top_y = y - ascent
        else:
            bbox = font.getbbox(text)
            top_y = y - (bbox[3] - bbox[1])

        # Render text to a grayscale mask at the same frame coordinates
        text_mask = Image.new('L', (self.cols, self.rows), 0)
        mask_draw = ImageDraw.Draw(text_mask)
        mask_draw.fontmode = "1"
        mask_draw.text((x, top_y), text, fill=255, font=font)

        # Composite: check each text pixel against the existing frame
        for py in range(max(0, top_y), min(self.rows, top_y + 16)):
            for px in range(max(0, x), min(self.cols, x + self.text_width(text, font_size) + 2)):
                if text_mask.getpixel((px, py)) > 0:
                    r, g, b = self.frame.getpixel((px, py))
                    if r > 0 or g > 0 or b > 0:
                        self.frame.putpixel((px, py), color_on_lit)
                    else:
                        self.frame.putpixel((px, py), color_on_dark)

    def draw_bar(self, x: int, y: int, width: int, height: int,
                 score: int, max_score: int, color: Tuple[int, int, int]):
        """Draw a bar graph using bottom-fill algorithm.

        LEDs fill from the bottom of each column upward, then move to the
        next column left-to-right. LED n maps to:
            column = n // height
            row    = (height - 1) - (n % height)

        Args:
            x: Left edge X coordinate of bar area
            y: Top edge Y coordinate of bar area
            width: Number of columns available
            height: Number of rows for the bar
            score: Number of LEDs to light
            max_score: Maximum LEDs (width * height), used for clamping
            color: Base RGB tuple (will be scaled by BAR_BRIGHTNESS)
        """
        num_leds = min(score, max_score)
        if num_leds <= 0:
            return

        # Scale color by brightness
        scaled = (int(color[0] * BAR_BRIGHTNESS),
                  int(color[1] * BAR_BRIGHTNESS),
                  int(color[2] * BAR_BRIGHTNESS))

        for n in range(num_leds):
            col = n // height
            row = (height - 1) - (n % height)
            self.frame.putpixel((x + col, y + row), scaled)

    def draw_zigzag(self, x: int, y: int, height: int,
                    color: Tuple[int, int, int] = (80, 80, 80)):
        """Draw a broken-axis zigzag indicator in a single column.

        Alternating pixels: ON at even rows, OFF at odd rows.

        Args:
            x: Column X coordinate
            y: Top edge Y coordinate
            height: Number of rows
            color: RGB tuple for the zigzag pixels
        """
        for row in range(height):
            if row % 2 == 0:  # ON at rows 0, 2, 4
                self.frame.putpixel((x, y + row), color)

    def show_scores(self, patrols: List[PatrolScore], rate_limit_state: str = "NONE",
                    patrol_colors: Dict[str, str] = None, score_offset: int = 0,
                    ws_connected: bool = False):
        """Display patrol names and scores with bar graphs and status indicator.

        Args:
            patrols: List of PatrolScore objects (up to 4)
            rate_limit_state: Current rate limit state for status indicator
            patrol_colors: Dict mapping patrol ID to color name (e.g., {"123": "red"})
            score_offset: Score offset for broken-axis display (subtracted from scores)
            ws_connected: True when an active WebSocket connection is open (shows blue dot)
        """
        if patrol_colors is None:
            patrol_colors = {}

        self.clear()

        # Special handling for service blocked - show message instead of scores
        if rate_limit_state == "SERVICE_BLOCKED":
            self.draw_text(2, 10, "Service", color=(255, 0, 0))
            self.draw_text(2, 20, "Blocked", color=(255, 0, 0))
            self.draw_text(2, 28, "Contact", color=(255, 100, 0))
            self.draw_text(2, 32, "Admin", color=(255, 100, 0))
            self.draw_status_indicator(rate_limit_state, ws_connected=ws_connected)
            self.show()

            if self.simulate:
                print("\n" + "="*40)
                print("SERVICE BLOCKED - Contact Administrator")
                print("="*40 + "\n")
            return

        # Determine bar start column and max LEDs based on offset
        has_offset = score_offset > 0
        bar_start_col = 1 if has_offset else 0
        bar_cols = BAR_WIDTH - bar_start_col
        bar_max = bar_cols * BAR_HEIGHT

        # Display up to 4 patrols
        row_height = 8
        for i, patrol in enumerate(patrols[:4]):
            strip_y = i * row_height       # Top of this patrol's strip
            border_top_y = strip_y + 1     # Top border line
            bar_y = strip_y + 2            # Bar occupies rows 2-6 within the strip (5 rows)
            border_bottom_y = strip_y + 7  # Bottom border line
            text_y = strip_y + 8           # Baseline for text (1px below bottom border)

            # Calculate display score (after offset)
            display_score = max(0, patrol.score - score_offset)
            bar_length = int(math.ceil(float(display_score)/BAR_HEIGHT))

            # Look up theme palette
            color_name = patrol_colors.get(patrol.id, DEFAULT_BAR_COLOR)
            palette = THEME_PALETTES.get(color_name, THEME_PALETTES[DEFAULT_BAR_COLOR])

            # Compute border color from bar base color at BORDER_BRIGHTNESS
            bar_base = palette["bar"]
            border_color = (int(bar_base[0] * BORDER_BRIGHTNESS),
                            int(bar_base[1] * BORDER_BRIGHTNESS),
                            int(bar_base[2] * BORDER_BRIGHTNESS))

            # Draw top and bottom border lines (matching bar length)
            if bar_length > 0:
                border_end_x = bar_start_col + bar_length - 1
                self.draw_line(bar_start_col, border_top_y, border_end_x, border_top_y, border_color)

            # Draw zigzag broken-axis indicator if offset is active
            if has_offset:
                self.draw_zigzag(0, bar_y, BAR_HEIGHT)

            # Draw bar graph behind text
            self.draw_bar(bar_start_col, bar_y, bar_cols, BAR_HEIGHT,
                          display_score, bar_max, palette["bar"])

            if bar_length > 0:
                border_end_x = bar_start_col + bar_length - 1
                self.draw_line(bar_start_col, border_bottom_y, border_end_x, border_bottom_y, border_color)

            # Draw patrol name (left justified, composited over bar)
            name = patrol.name
            if len(name) > 11:  # Truncate long names (small font fits more)
                name = name[:11]
            self.draw_composite_text(1, text_y, name, font_size="small")

            # Draw score (right justified, composited over bar)
            score_text = str(patrol.score)
            score_w = self.text_width(score_text, font_size="small")
            score_x = self.cols - score_w - 2  # Extra padding from edge
            self.draw_composite_text(score_x, text_y, score_text, font_size="small")

        # Draw status indicator
        self.draw_status_indicator(rate_limit_state, ws_connected=ws_connected)

        self.show()

        if self.simulate:
            print("\n" + "="*40)
            print(f"SCOREBOARD (offset={score_offset})")
            print("="*40)
            for patrol in patrols[:4]:
                display_score = max(0, patrol.score - score_offset)
                color_name = patrol_colors.get(patrol.id, DEFAULT_BAR_COLOR)
                print(f"{patrol.name:<15} {patrol.score:>6}  bar={display_score:>3} [{color_name}]")
            print(f"\nStatus: {rate_limit_state}")
            if has_offset:
                print(f"Broken axis: offset={score_offset}")
            print("="*40 + "\n")

    def show_countdown(self, seconds_remaining: int, paused: bool = False):
        """Display a countdown timer.

        Args:
            seconds_remaining: Number of seconds left on the timer
            paused: If True, show a "PAUSED" label at the bottom
        """
        self.clear()

        # Format as MM:SS
        minutes = max(0, seconds_remaining) // 60
        seconds = max(0, seconds_remaining) % 60
        time_str = f"{minutes:02d}:{seconds:02d}"

        # Center the time string horizontally
        text_w = self.text_width(time_str, font_size="large")
        x = max(0, (self.cols - text_w) // 2)
        y = self.rows // 2

        color = (255, 255, 0) if not paused else (255, 128, 0)
        self.draw_text(x, y, time_str, color=color, font_size="large")

        if paused:
            label = "PAUSED"
            label_w = self.text_width(label, font_size="small")
            label_x = max(0, (self.cols - label_w) // 2)
            self.draw_text(label_x, self.rows, label, color=(200, 100, 0), font_size="small")

        self.show()

        if self.simulate:
            state = "PAUSED" if paused else "running"
            print(f"[DISPLAY] Timer: {time_str} [{state}]")

    def show_message(self, message: str, color: Tuple[int, int, int] = (255, 255, 255)):
        """Display a centered message.

        Args:
            message: Message to display
            color: RGB color tuple
        """
        self.clear()
        text_w = self.text_width(message)
        x = max(0, (self.cols - text_w) // 2)
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
