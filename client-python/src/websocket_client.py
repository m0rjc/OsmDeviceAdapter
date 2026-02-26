import json
import logging
import threading
from typing import Optional, Callable, List

logger = logging.getLogger(__name__)

class WebSocketClient:
    """Persistent WebSocket connection to the server for real-time score notifications.

    Runs in a daemon thread and reconnects automatically with exponential backoff.
    Calls on_message(data) for every parsed JSON message received.
    """

    def __init__(self, ws_url: str, on_message, headers: Optional[List[str]] = None,
                 on_state_change: Optional[Callable[[bool], None]] = None):
        self.url = ws_url
        self.headers = headers or []
        self._on_message = on_message
        self._on_state_change = on_state_change
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

    def _set_connected(self, value: bool):
        if self._connected != value:
            self._connected = value
            if self._on_state_change is not None:
                try:
                    self._on_state_change(value)
                except Exception as e:
                    logger.debug(f"WebSocket state-change callback error: {e}")

    def _run_loop(self):
        import random

        base_backoff = 2  # seconds before first retry
        backoff = base_backoff

        while not self._stop.is_set():
            try:
                self._connect()
            except Exception as e:
                logger.debug(f"WebSocket run error: {e}")

            if self._stop.is_set():
                break

            # If we were connected at any point during the last attempt,
            # treat it as "success" and reset backoff so brief drops recover quickly.
            if self._connected:
                backoff = base_backoff
            else:
                backoff = min(backoff * 2, 60)

            # Add jitter to avoid thundering herd reconnects
            sleep_for = backoff + random.uniform(0, min(1.0, backoff * 0.1))
            logger.debug(f"WebSocket reconnecting in {sleep_for:.1f}s")
            self._stop.wait(timeout=sleep_for)

    def _connect(self):
        try:
            import websocket as ws_lib
        except ImportError:
            logger.warning("websocket-client not installed; real-time updates unavailable")
            self._stop.set()
            return

        def on_open(ws):
            self._set_connected(True)
            logger.info("WebSocket connected — real-time score updates active")

        def on_message(ws, message):
            try:
                data = json.loads(message)
                self._on_message(data)
            except Exception as e:
                logger.warning(f"WebSocket message error: {e}")

        def on_close(ws, close_status_code, close_msg):
            self._set_connected(False)
            logger.debug(f"WebSocket connection closed (code={close_status_code}, msg={close_msg})")

        def on_error(ws, error):
            self._set_connected(False)
            logger.debug(f"WebSocket error: {error}")

        self._app = ws_lib.WebSocketApp(
            self.url,
            header=self.headers,
            on_open=on_open,
            on_message=on_message,
            on_close=on_close,
            on_error=on_error,
        )

        # run_forever blocks until the connection closes
        self._app.run_forever(ping_interval=30, ping_timeout=10)
        self._set_connected(False)
        self._app = None
