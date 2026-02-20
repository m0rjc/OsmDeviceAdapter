#!/usr/bin/env python3
"""LED Matrix Scoreboard Application

Displays scout patrol scores on a 64x32 LED matrix using the Adafruit RGB Matrix HAT.
Authenticates using OAuth device flow and polls for score updates.
"""

import json
import os
import sys
import threading
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


class WebSocketClient:
    """Persistent WebSocket connection to the server for real-time score notifications.

    Runs in a daemon thread and reconnects automatically with exponential backoff.
    Calls on_message(data) for every parsed JSON message received.
    """

    def __init__(self, ws_url: str, on_message):
        self.url = ws_url
        self._on_message = on_message
        self._stop = threading.Event()
        self._connected = False
        self._app = None  # websocket.WebSocketApp instance
        self._thread = threading.Thread(target=self._run_loop, daemon=True, name="ws-client")

    def start(self):
        """Start the background connection thread."""
        self._thread.start()

    def stop(self):
        """Signal the connection thread to exit and close any open connection."""
        self._stop.set()
        if self._app is not None:
            self._app.close()

    @property
    def connected(self) -> bool:
        return self._connected

    def _run_loop(self):
        backoff = 5  # seconds before first retry
        while not self._stop.is_set():
            try:
                self._connect()
            except Exception as e:
                logger.debug(f"WebSocket run error: {e}")
            if not self._stop.is_set():
                logger.debug(f"WebSocket reconnecting in {backoff}s")
                self._stop.wait(timeout=backoff)
                backoff = min(backoff * 2, 60)

    def _connect(self):
        try:
            import websocket as ws_lib
        except ImportError:
            logger.warning("websocket-client not installed; real-time updates unavailable")
            self._stop.set()
            return

        def on_open(ws):
            self._connected = True
            logger.info("WebSocket connected — real-time score updates active")

        def on_message(ws, message):
            try:
                data = json.loads(message)
                self._on_message(data)
            except Exception as e:
                logger.warning(f"WebSocket message error: {e}")

        def on_close(ws, close_status_code, close_msg):
            self._connected = False
            logger.debug("WebSocket connection closed")

        def on_error(ws, error):
            self._connected = False
            logger.debug(f"WebSocket error: {error}")

        app = ws_lib.WebSocketApp(
            self.url,
            on_open=on_open,
            on_message=on_message,
            on_close=on_close,
            on_error=on_error,
        )
        self._app = app
        # run_forever blocks until the connection closes
        app.run_forever(ping_interval=30, ping_timeout=10)
        self._connected = False
        self._app = None


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
        self.score_offset = 0          # Bar graph offset for broken-axis display
        self.offset_initialized = False  # False until first successful score fetch

        # WebSocket state
        self._ws_client: Optional[WebSocketClient] = None
        self._refresh_event = threading.Event()  # Set by WebSocket thread to wake main loop

        # Timer state
        self._timer_state: str = 'inactive'  # 'inactive' | 'running' | 'paused' | 'finished'
        self._timer_remaining: int = 0
        self._timer_tick = threading.Event()  # Signalled to interrupt timer sleeps
        self._timer_thread: Optional[threading.Thread] = None

        # Set up signal handler for SIGTERM (SIGINT/CTRL-C handled by KeyboardInterrupt)
        signal.signal(signal.SIGTERM, self._signal_handler)

    def _signal_handler(self, signum, frame):
        """Handle SIGTERM signal by raising KeyboardInterrupt for clean shutdown."""
        logger.info(f"Received signal {signum}, shutting down...")
        raise KeyboardInterrupt()

    def _start_websocket(self):
        """Start (or restart) the WebSocket client using the current access token."""
        self._stop_websocket()
        token = self.client.access_token
        if not token:
            return
        ws_url = (
            API_BASE_URL
            .replace("https://", "wss://")
            .replace("http://", "ws://")
            .rstrip("/")
            + f"/ws/device?token={token}"
        )
        logger.info("Starting WebSocket client")
        client = WebSocketClient(ws_url, on_message=self._on_ws_message)
        client.start()
        self._ws_client = client

    def _stop_websocket(self):
        """Stop the WebSocket client if one is running."""
        if self._ws_client is not None:
            logger.info("Stopping WebSocket client")
            self._ws_client.stop()
            self._ws_client = None

    @property
    def _ws_connected(self) -> bool:
        return self._ws_client is not None and self._ws_client.connected

    def _on_ws_message(self, data: dict):
        """Route incoming WebSocket messages to the appropriate handler."""
        msg_type = data.get("type")
        if msg_type == "refresh-scores":
            logger.debug("WebSocket: received refresh-scores")
            self._refresh_event.set()
        elif msg_type == "disconnect":
            logger.info(f"WebSocket: server requested disconnect ({data.get('reason', '')})")
        elif msg_type == "timer-start":
            duration = data.get("duration", 0)
            logger.info(f"WebSocket: timer-start duration={duration}s")
            self._start_timer(duration)
        elif msg_type == "timer-pause":
            logger.info("WebSocket: timer-pause")
            self._timer_state = 'paused'
            self._timer_tick.set()
        elif msg_type == "timer-resume":
            logger.info("WebSocket: timer-resume")
            self._timer_state = 'running'
            self._timer_tick.set()
        elif msg_type == "timer-reset":
            logger.info("WebSocket: timer-reset")
            self._timer_state = 'inactive'
            self._timer_tick.set()

    def _start_timer(self, duration: int):
        """Start a new countdown timer, stopping any existing one."""
        # Stop existing timer thread
        self._timer_state = 'inactive'
        self._timer_tick.set()
        if self._timer_thread is not None and self._timer_thread.is_alive():
            self._timer_thread.join(timeout=2)

        self._timer_remaining = duration
        self._timer_tick.clear()
        self._timer_state = 'running'
        self._timer_thread = threading.Thread(target=self._timer_loop, daemon=True, name="timer")
        self._timer_thread.start()

    def _timer_loop(self):
        """Daemon thread: decrement timer and update display each second."""
        while self._timer_state != 'inactive':
            if self._timer_state == 'running':
                # Wait up to 1 second; shorter if signalled
                signalled = self._timer_tick.wait(timeout=1.0)
                self._timer_tick.clear()
                if not signalled and self._timer_state == 'running':
                    # One second elapsed, decrement
                    self._timer_remaining -= 1
                    if self._timer_remaining <= 0:
                        self._timer_remaining = 0
                        self._timer_state = 'finished'
            else:
                # Paused or finished — wait for a signal
                self._timer_tick.wait(timeout=0.5)
                self._timer_tick.clear()

            # Update display
            paused = (self._timer_state == 'paused')
            self.display.show_countdown(self._timer_remaining, paused=paused)

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

        def on_code_received(user_code: str, verification_uri: str, verification_uri_complete: str, verification_uri_short: str):
            """Called when device code is received."""
            logger.info(f"User code: {user_code}")
            logger.info(f"Verification URI: {verification_uri}")
            logger.info(f"Complete URI: {verification_uri_complete}")
            logger.info(f"Short URI: {verification_uri_short}")
            self.display.show_device_code(user_code, verification_uri, verification_uri_short)

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

            # Calculate bar graph offset
            if response.patrols:
                max_score = max(p.score for p in response.patrols)
                if not self.offset_initialized:
                    # First fetch: set offset if scores exceed bar capacity
                    if max_score > 240:
                        self.score_offset = max_score - 200
                    else:
                        self.score_offset = 0
                    self.offset_initialized = True
                else:
                    # Subsequent fetches: only recalculate if a score overflows
                    if any(p.score - self.score_offset > 240 for p in response.patrols):
                        self.score_offset = max_score - 200

            # Convert to display format
            display_patrols = [
                DisplayPatrolScore(name=p.name, score=p.score, patrol_id=p.id)
                for p in response.patrols
            ]

            # Start WebSocket if server supports it and we don't have one yet
            if response.websocket_requested and self._ws_client is None:
                self._start_websocket()

            # Update display with bar graphs and status indicator
            self.display.show_scores(
                display_patrols,
                response.rate_limit_state,
                patrol_colors=response.patrol_colors,
                score_offset=self.score_offset,
                ws_connected=self._ws_connected,
            )

        except SectionNotFound as e:
            logger.error(f"Section not found: {e}")
            self.display.show_error("Section Lost")
            self.authenticated = False
            self._stop_websocket()
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
                self._stop_websocket()
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
                # Re-authenticate if needed (stop WebSocket first — token is stale)
                if not self.authenticated:
                    self._stop_websocket()
                    self.authenticate()

                # While timer is active, skip score polling and let the timer thread drive the display
                if self._timer_state != 'inactive':
                    self._refresh_event.wait(timeout=0.1)
                    self._refresh_event.clear()
                    continue

                # Determine when to poll next
                now = datetime.now(timezone.utc)
                should_poll = False

                if self._refresh_event.is_set():
                    # WebSocket pushed a refresh-scores notification
                    self._refresh_event.clear()
                    should_poll = True
                    logger.info("WebSocket triggered immediate score refresh")
                elif self.cache_expires_at is None:
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
                    self.update_scores()

                # Sleep up to 1s; wake early if WebSocket signals a refresh
                self._refresh_event.wait(timeout=1)

        except KeyboardInterrupt:
            logger.info("Interrupted by user")
        except Exception as e:
            logger.exception(f"Unexpected error: {e}")
            self.display.show_error("Fatal Error")
            time.sleep(5)
        finally:
            logger.info("Cleaning up...")
            self._stop_websocket()
            self.display.cleanup()
            logger.info("Scoreboard application stopped")


def main():
    """Main entry point."""
    app = ScoreboardApp()
    app.run()


if __name__ == "__main__":
    main()
