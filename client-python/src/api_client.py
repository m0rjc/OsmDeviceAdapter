"""API Client for OSM Device Adapter

Handles device flow authentication and score polling.
"""

import time
import requests
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass
from datetime import datetime


@dataclass
class DeviceAuthResponse:
    """Response from device authorization request."""
    device_code: str
    user_code: str
    verification_uri: str
    verification_uri_complete: str
    expires_in: int
    interval: int


@dataclass
class TokenResponse:
    """Response from token request."""
    access_token: str
    token_type: str
    expires_in: int
    refresh_token: Optional[str] = None


@dataclass
class PatrolScore:
    """Represents a patrol and its score."""
    id: str
    name: str
    score: int


class DeviceFlowError(Exception):
    """Base exception for device flow errors."""
    pass


class AuthorizationPending(DeviceFlowError):
    """Authorization is still pending."""
    pass


class AccessDenied(DeviceFlowError):
    """User denied the authorization."""
    pass


class ExpiredToken(DeviceFlowError):
    """Device code has expired."""
    pass


class OSMDeviceClient:
    """Client for OSM Device Adapter API."""

    def __init__(self, base_url: str, client_id: str, timeout: int = 10):
        """Initialize the API client.

        Args:
            base_url: Base URL of the OSM Device Adapter (e.g., "https://example.com")
            client_id: Unique client identifier for this device
            timeout: Request timeout in seconds
        """
        self.base_url = base_url.rstrip('/')
        self.client_id = client_id
        self.timeout = timeout
        self.session = requests.Session()
        self.access_token: Optional[str] = None

    def request_device_code(self, scope: str = "section:member:read") -> DeviceAuthResponse:
        """Request a device code to start the authorization flow.

        Args:
            scope: OAuth scope to request

        Returns:
            DeviceAuthResponse with device_code, user_code, etc.

        Raises:
            DeviceFlowError: If request fails
        """
        url = f"{self.base_url}/device/authorize"
        payload = {
            "client_id": self.client_id,
            "scope": scope
        }

        try:
            response = self.session.post(url, json=payload, timeout=self.timeout)
            response.raise_for_status()
            data = response.json()

            return DeviceAuthResponse(
                device_code=data["device_code"],
                user_code=data["user_code"],
                verification_uri=data["verification_uri"],
                verification_uri_complete=data["verification_uri_complete"],
                expires_in=data["expires_in"],
                interval=data["interval"]
            )
        except requests.exceptions.RequestException as e:
            raise DeviceFlowError(f"Failed to request device code: {e}")

    def poll_for_token(self, device_code: str) -> TokenResponse:
        """Poll for an access token.

        Args:
            device_code: Device code from request_device_code()

        Returns:
            TokenResponse with access_token

        Raises:
            AuthorizationPending: User hasn't authorized yet
            AccessDenied: User denied authorization
            ExpiredToken: Device code expired
            DeviceFlowError: Other errors
        """
        url = f"{self.base_url}/device/token"
        payload = {
            "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
            "device_code": device_code,
            "client_id": self.client_id
        }

        try:
            response = self.session.post(url, json=payload, timeout=self.timeout)

            # Check for pending authorization
            if response.status_code == 400:
                error_data = response.json()
                error = error_data.get("error", "")

                if error == "authorization_pending":
                    raise AuthorizationPending("Authorization pending")
                elif error == "access_denied":
                    raise AccessDenied("User denied authorization")
                elif error == "expired_token":
                    raise ExpiredToken("Device code expired")
                else:
                    raise DeviceFlowError(f"Token error: {error}")

            response.raise_for_status()
            data = response.json()

            return TokenResponse(
                access_token=data["access_token"],
                token_type=data["token_type"],
                expires_in=data["expires_in"],
                refresh_token=data.get("refresh_token")
            )

        except requests.exceptions.RequestException as e:
            if not isinstance(e, DeviceFlowError):
                raise DeviceFlowError(f"Failed to poll for token: {e}")
            raise

    def get_patrol_scores(self) -> Tuple[List[PatrolScore], datetime, datetime]:
        """Get current patrol scores.

        Returns:
            Tuple of (patrol_scores, cached_at, expires_at)

        Raises:
            DeviceFlowError: If request fails or not authenticated
        """
        if not self.access_token:
            raise DeviceFlowError("Not authenticated. Call authenticate() first.")

        url = f"{self.base_url}/api/v1/patrols"
        headers = {
            "Authorization": f"Bearer {self.access_token}"
        }

        try:
            response = self.session.get(url, headers=headers, timeout=self.timeout)
            response.raise_for_status()
            data = response.json()

            patrols = [
                PatrolScore(
                    id=p["id"],
                    name=p["name"],
                    score=p["score"]
                )
                for p in data["patrols"]
            ]

            cached_at = datetime.fromisoformat(data["cached_at"].replace('Z', '+00:00'))
            expires_at = datetime.fromisoformat(data["expires_at"].replace('Z', '+00:00'))

            return patrols, cached_at, expires_at

        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                raise DeviceFlowError("Authentication expired or invalid")
            raise DeviceFlowError(f"Failed to get patrol scores: {e}")
        except requests.exceptions.RequestException as e:
            raise DeviceFlowError(f"Failed to get patrol scores: {e}")

    def authenticate(self, on_code_received=None, on_waiting=None) -> TokenResponse:
        """Perform full device flow authentication.

        Args:
            on_code_received: Callback(user_code, verification_uri) when code is ready
            on_waiting: Callback() called on each poll attempt

        Returns:
            TokenResponse with access_token

        Raises:
            DeviceFlowError: If authentication fails
        """
        # Step 1: Request device code
        auth = self.request_device_code()

        # Notify about the code
        if on_code_received:
            on_code_received(auth.user_code, auth.verification_uri)

        # Step 2: Poll for token
        start_time = time.time()
        poll_interval = auth.interval

        while True:
            elapsed = time.time() - start_time
            if elapsed > auth.expires_in:
                raise ExpiredToken("Device code expired before authorization")

            # Wait before polling
            time.sleep(poll_interval)

            if on_waiting:
                on_waiting()

            try:
                token = self.poll_for_token(auth.device_code)
                self.access_token = token.access_token
                return token

            except AuthorizationPending:
                # Keep waiting
                continue
            except AccessDenied:
                raise
            except ExpiredToken:
                raise

    def is_authenticated(self) -> bool:
        """Check if client has an access token.

        Returns:
            True if authenticated
        """
        return self.access_token is not None

    def set_access_token(self, token: str):
        """Set the access token manually.

        Args:
            token: Access token string
        """
        self.access_token = token
