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

from api_client import (
    OSMDeviceClient, DeviceFlowError, AccessDenied, ExpiredToken,
    SectionNotFound, NotInTerm, UserTemporaryBlock, ServiceBlocked
)
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
        self.cache_expires_at = None  # Track when to poll next
        self.current_rate_limit_state = "NONE"  # Track current state

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

        def on_code_received(user_code: str, verification_uri: str, verification_uri_complete: str):
            """Called when device code is received."""
            logger.info(f"User code: {user_code}")
            logger.info(f"Verification URI: {verification_uri}")
            logger.info(f"Complete URI: {verification_uri_complete}")
            self.display.show_device_code(user_code, verification_uri, verification_uri_complete)

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
        # Show loading indicator if we have existing scores
        if self.cache_expires_at is not None:
            # We already have scores displayed, just show loading in corner
            self.current_rate_limit_state = "LOADING"

        try:
            response = self.client.get_patrol_scores()

            logger.info(
                f"Received {len(response.patrols)} patrol scores "
                f"(cached: {response.from_cache}, state: {response.rate_limit_state})"
            )

            # Store cache expiry for intelligent polling
            self.cache_expires_at = response.cache_expires_at
            self.current_rate_limit_state = response.rate_limit_state

            # Convert to display format
            display_patrols = [
                DisplayPatrolScore(name=p.name, score=p.score)
                for p in response.patrols
            ]

            # Update display with status indicator
            self.display.show_scores(display_patrols, response.rate_limit_state)

        except SectionNotFound as e:
            logger.error(f"Section not found: {e}")
            self.display.show_error("Section Lost")
            self.authenticated = False
            time.sleep(5)

        except NotInTerm as e:
            logger.warning(f"Not in term: {e}")
            self.display.show_message("Between Terms", color=(255, 191, 0))
            # Retry after 24 hours as per API docs
            from datetime import datetime, timedelta
            self.cache_expires_at = datetime.now(tz=self.cache_expires_at.tzinfo if self.cache_expires_at else None) + timedelta(hours=24)

        except UserTemporaryBlock as e:
            logger.warning(f"User temporarily blocked until {e.blocked_until}")
            self.display.show_message("Rate Limited", color=(255, 0, 0))
            # Use the blocked_until time for next poll
            self.cache_expires_at = e.blocked_until
            time.sleep(2)

        except ServiceBlocked as e:
            logger.error(f"Service blocked: {e}")
            # Display will show service blocked message with red indicator
            # Keep whatever scores we have and show service blocked
            self.current_rate_limit_state = "SERVICE_BLOCKED"
            self.display.show_scores([], "SERVICE_BLOCKED")
            # Retry after a long time (1 hour)
            from datetime import datetime, timedelta
            self.cache_expires_at = datetime.now(tz=self.cache_expires_at.tzinfo if self.cache_expires_at else None) + timedelta(hours=1)

        except DeviceFlowError as e:
            logger.error(f"Failed to get patrol scores: {e}")
            self.display.show_error("Score Error")

            # If authentication error, mark as not authenticated
            if "Authentication" in str(e) or "401" in str(e):
                self.authenticated = False
                logger.warning("Authentication appears invalid, will re-authenticate")

    def run(self):
        """Main application loop."""
        from datetime import datetime, timezone

        logger.info("Scoreboard application starting...")
        logger.info(f"API: {API_BASE_URL}")
        logger.info(f"Client ID: {CLIENT_ID}")
        logger.info(f"Default poll interval: {POLL_INTERVAL}s (will use cache_expires_at when available)")

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

            # Main loop: intelligently poll based on cache expiry
            while self.running:
                # Re-authenticate if needed
                if not self.authenticated:
                    self.authenticate()

                # Determine when to poll next
                now = datetime.now(timezone.utc)
                should_poll = False

                if self.cache_expires_at is None:
                    # First poll or no cache info - poll immediately
                    should_poll = True
                else:
                    # Poll shortly after cache expires (add 7 seconds buffer as per API docs)
                    poll_time = self.cache_expires_at.replace(microsecond=0)
                    time_until_poll = (poll_time - now).total_seconds() + 7

                    if time_until_poll <= 0:
                        should_poll = True
                    else:
                        logger.debug(f"Next poll in {time_until_poll:.0f}s (cache expires at {self.cache_expires_at})")

                if should_poll:
                    # Update scores
                    self.update_scores()

                # Sleep briefly to avoid busy-waiting (check every second)
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
