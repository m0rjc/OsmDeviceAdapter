#!/usr/bin/env python3
"""LED Matrix Scoreboard Application

Displays scout patrol scores on a 64x32 LED matrix using the Adafruit RGB Matrix HAT.
Authenticates using OAuth device flow and polls for score updates.
"""

import os
import sys
import time
import signal
import logging
from pathlib import Path
from typing import Optional

from api_client import OSMDeviceClient, DeviceFlowError, AccessDenied, ExpiredToken
from display import MatrixDisplay, PatrolScore as DisplayPatrolScore


# Configuration from environment variables
API_BASE_URL = os.getenv("API_BASE_URL", "http://localhost:8080")
CLIENT_ID = os.getenv("CLIENT_ID", "scoreboard-rpi")
POLL_INTERVAL = int(os.getenv("POLL_INTERVAL", "30"))  # Seconds between score updates
TOKEN_FILE = Path(os.getenv("TOKEN_FILE", "/var/lib/scoreboard/token.txt"))
SIMULATE_DISPLAY = os.getenv("SIMULATE_DISPLAY", "false").lower() == "true"

# Logging configuration
LOG_LEVEL = os.getenv("LOG_LEVEL", "INFO")
logging.basicConfig(
    level=LOG_LEVEL,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class ScoreboardApp:
    """Main scoreboard application."""

    def __init__(self):
        """Initialize the scoreboard application."""
        self.display = MatrixDisplay(rows=32, cols=64, simulate=SIMULATE_DISPLAY)
        self.client = OSMDeviceClient(base_url=API_BASE_URL, client_id=CLIENT_ID)
        self.running = True
        self.authenticated = False

        # Set up signal handlers for graceful shutdown
        signal.signal(signal.SIGINT, self._signal_handler)
        signal.signal(signal.SIGTERM, self._signal_handler)

    def _signal_handler(self, signum, frame):
        """Handle shutdown signals."""
        logger.info(f"Received signal {signum}, shutting down...")
        self.running = False

    def load_token(self) -> bool:
        """Load saved access token from file.

        Returns:
            True if token was loaded successfully
        """
        try:
            if TOKEN_FILE.exists():
                token = TOKEN_FILE.read_text().strip()
                if token:
                    self.client.set_access_token(token)
                    logger.info("Loaded saved access token")
                    return True
        except Exception as e:
            logger.warning(f"Failed to load token: {e}")
        return False

    def save_token(self, token: str):
        """Save access token to file.

        Args:
            token: Access token to save
        """
        try:
            TOKEN_FILE.parent.mkdir(parents=True, exist_ok=True)
            TOKEN_FILE.write_text(token)
            TOKEN_FILE.chmod(0o600)  # Secure permissions
            logger.info("Saved access token")
        except Exception as e:
            logger.error(f"Failed to save token: {e}")

    def authenticate(self):
        """Perform device flow authentication."""
        logger.info("Starting device flow authentication...")

        def on_code_received(user_code: str, verification_uri: str):
            """Called when device code is received."""
            logger.info(f"User code: {user_code}")
            logger.info(f"Verification URI: {verification_uri}")
            self.display.show_device_code(user_code, verification_uri)

        def on_waiting():
            """Called while waiting for authorization."""
            logger.debug("Waiting for user authorization...")

        try:
            token = self.client.authenticate(
                on_code_received=on_code_received,
                on_waiting=on_waiting
            )

            logger.info("Authentication successful!")
            self.save_token(token.access_token)
            self.authenticated = True

            # Show success message briefly
            self.display.show_message("Authorized!", color=(0, 255, 0))
            time.sleep(2)

        except AccessDenied:
            logger.error("User denied authorization")
            self.display.show_error("Access Denied")
            time.sleep(5)
            raise

        except ExpiredToken:
            logger.error("Device code expired")
            self.display.show_error("Code Expired")
            time.sleep(5)
            raise

        except DeviceFlowError as e:
            logger.error(f"Authentication failed: {e}")
            self.display.show_error("Auth Failed")
            time.sleep(5)
            raise

    def update_scores(self):
        """Fetch and display current patrol scores."""
        try:
            patrols, cached_at, expires_at = self.client.get_patrol_scores()

            logger.info(f"Received {len(patrols)} patrol scores (cached at {cached_at})")

            # Convert to display format
            display_patrols = [
                DisplayPatrolScore(name=p.name, score=p.score)
                for p in patrols
            ]

            # Update display
            self.display.show_scores(display_patrols)

        except DeviceFlowError as e:
            logger.error(f"Failed to get patrol scores: {e}")
            self.display.show_error("Score Error")

            # If authentication error, mark as not authenticated
            if "Authentication" in str(e) or "401" in str(e):
                self.authenticated = False
                logger.warning("Authentication appears invalid, will re-authenticate")

    def run(self):
        """Main application loop."""
        logger.info("Scoreboard application starting...")
        logger.info(f"API: {API_BASE_URL}")
        logger.info(f"Client ID: {CLIENT_ID}")
        logger.info(f"Poll interval: {POLL_INTERVAL}s")

        try:
            # Show startup message
            self.display.show_message("Starting...")
            time.sleep(1)

            # Try to load saved token
            if self.load_token():
                # Verify token works by trying to get scores
                try:
                    self.update_scores()
                    self.authenticated = True
                    logger.info("Saved token is valid")
                except DeviceFlowError:
                    logger.warning("Saved token is invalid, will re-authenticate")
                    self.authenticated = False

            # Authenticate if needed
            if not self.authenticated:
                self.authenticate()

            # Main loop: periodically update scores
            last_update = 0
            while self.running:
                current_time = time.time()

                # Update scores at specified interval
                if current_time - last_update >= POLL_INTERVAL:
                    # Re-authenticate if needed
                    if not self.authenticated:
                        self.authenticate()

                    # Update scores
                    self.update_scores()
                    last_update = current_time

                # Sleep briefly to avoid busy-waiting
                time.sleep(1)

        except KeyboardInterrupt:
            logger.info("Interrupted by user")
        except Exception as e:
            logger.exception(f"Unexpected error: {e}")
            self.display.show_error("Fatal Error")
            time.sleep(5)
        finally:
            logger.info("Cleaning up...")
            self.display.cleanup()
            logger.info("Scoreboard application stopped")


def main():
    """Main entry point."""
    app = ScoreboardApp()
    app.run()


if __name__ == "__main__":
    main()
