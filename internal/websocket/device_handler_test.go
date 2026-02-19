package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wslib "github.com/gorilla/websocket"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuthenticator implements deviceAuthenticator for testing.
type stubAuthenticator struct {
	user types.User
	err  error
}

func (s *stubAuthenticator) Authenticate(_ context.Context, _ string) (types.User, error) {
	return s.user, s.err
}

// stubUser implements types.User and deviceCodeProvider.
type stubUser struct {
	deviceCode *db.DeviceCode
}

func (u *stubUser) UserID() *int {
	return u.deviceCode.OsmUserID
}
func (u *stubUser) AccessToken() string { return "stub-token" }
func (u *stubUser) DeviceCode() *db.DeviceCode {
	return u.deviceCode
}

// newTestHub creates a hub wired to miniredis.
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)
	time.Sleep(20 * time.Millisecond)
	return hub
}

// wsDialURL converts an httptest server URL to a WebSocket URL.
func wsDialURL(serverURL, path string) string {
	u := strings.Replace(serverURL, "http://", "ws://", 1)
	return u + path
}

func TestDeviceHandler_InvalidToken(t *testing.T) {
	hub := newTestHub(t)
	auth := &stubAuthenticator{err: ErrAuthFailed}
	srv := httptest.NewServer(DeviceWebSocketHandler(hub, auth, "http://localhost"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws/device?token=bad")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestDeviceHandler_MissingToken(t *testing.T) {
	hub := newTestHub(t)
	auth := &stubAuthenticator{err: ErrAuthFailed}
	srv := httptest.NewServer(DeviceWebSocketHandler(hub, auth, "http://localhost"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws/device")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestDeviceHandler_ValidTokenUpgradesAndRegisters(t *testing.T) {
	hub := newTestHub(t)

	sectionID := 42
	osmUserID := 7
	device := &db.DeviceCode{
		DeviceCode:        "test-device-code-001",
		DeviceAccessToken: strPtr("valid-token"),
		SectionID:         &sectionID,
		OsmUserID:         &osmUserID,
	}
	auth := &stubAuthenticator{user: &stubUser{deviceCode: device}}
	srv := httptest.NewServer(DeviceWebSocketHandler(hub, auth, "http://localhost"))
	defer srv.Close()

	dialer := wslib.Dialer{}
	conn, resp, err := dialer.Dial(wsDialURL(srv.URL, "/ws/device?token=valid-token"), nil)
	require.NoError(t, err, "WebSocket dial should succeed")
	defer conn.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Give hub a moment to register the device.
	time.Sleep(30 * time.Millisecond)
	assert.True(t, hub.IsConnected("test-device-code-001"), "device should be registered in hub")
}

func TestDeviceHandler_ReceivesRefreshScores(t *testing.T) {
	hub := newTestHub(t)

	sectionID := 77
	osmUserID := 3
	device := &db.DeviceCode{
		DeviceCode:        "recv-test-device",
		DeviceAccessToken: strPtr("recv-token"),
		SectionID:         &sectionID,
		OsmUserID:         &osmUserID,
	}
	auth := &stubAuthenticator{user: &stubUser{deviceCode: device}}
	srv := httptest.NewServer(DeviceWebSocketHandler(hub, auth, "http://localhost"))
	defer srv.Close()

	conn, _, err := wslib.DefaultDialer.Dial(wsDialURL(srv.URL, "/ws/device?token=recv-token"), nil)
	require.NoError(t, err)
	defer conn.Close()

	// Wait for the device to register and the hub to subscribe.
	time.Sleep(100 * time.Millisecond)

	// Broadcast a refresh-scores from the server side.
	hub.BroadcastToSection("77", RefreshScoresMessage())

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	var msg Message
	err = conn.ReadJSON(&msg)
	require.NoError(t, err)
	assert.Equal(t, "refresh-scores", msg.Type)
}

func TestDeviceHandler_DeviceNotConfigured(t *testing.T) {
	hub := newTestHub(t)

	// Device with no SectionID
	device := &db.DeviceCode{
		DeviceCode:        "no-section-device",
		DeviceAccessToken: strPtr("pending-token"),
	}
	auth := &stubAuthenticator{user: &stubUser{deviceCode: device}}
	srv := httptest.NewServer(DeviceWebSocketHandler(hub, auth, "http://localhost"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws/device?token=pending-token")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ErrAuthFailed is a sentinel error for stub authenticators.
var ErrAuthFailed = stubError("authentication failed")

type stubError string

func (e stubError) Error() string { return string(e) }

func strPtr(s string) *string { return &s }
