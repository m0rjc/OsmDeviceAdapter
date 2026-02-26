package websocket

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRedis creates a miniredis-backed RedisClient for tests.
func newTestRedis(t *testing.T) (*db.RedisClient, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc, err := db.NewRedisClient("redis://"+mr.Addr(), "")
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = rc.Close()
		mr.Close()
	})

	return rc, mr
}

func startHub(t *testing.T, hub *Hub) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		hub.Run(ctx)
	}()

	t.Cleanup(func() {
		// Close() makes hub.Run return even if ctx isn't cancelled yet.
		// Cancel() makes sure any Redis ops using ctx unwind too.
		hub.Close()
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for hub.Run to stop")
		}
	})

	return ctx
}

func TestRegisterUnregister(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	dc := &deviceConn{
		hub:         hub,
		send:        make(chan Message, 1),
		deviceCode:  "device-abc",
		channelKeys: []string{"section:42", "device:device-abc"},
	}

	assert.False(t, hub.IsConnected("device-abc"), "not connected before register")

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "device-abc", dc, "section:42", "device:device-abc"))
	assert.True(t, hub.IsConnected("device-abc"), "connected after register")

	hub.UnregisterDeviceConn(dc)
	assert.False(t, hub.IsConnected("device-abc"), "not connected after unregister")
}

func TestRegisterUnregisterSectionTracking(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	dc1 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-1", channelKeys: []string{"section:7", "device:dev-1"}}
	dc2 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-2", channelKeys: []string{"section:7", "device:dev-2"}}

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "dev-1", dc1, "section:7", "device:dev-1"))
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "dev-2", dc2, "section:7", "device:dev-2"))

	hub.UnregisterDeviceConn(dc1)

	// Section channel should still have dev-2
	hub.mu.RLock()
	devs := hub.channelDevices["section:7"]
	_, hasDev2 := devs["dev-2"]
	_, hasDev1PerDevice := hub.channelDevices["device:dev-1"]
	hub.mu.RUnlock()

	assert.True(t, hasDev2, "section:7 should still track dev-2")
	assert.False(t, hasDev1PerDevice, "device:dev-1 channel should be removed after unregister")
	assert.True(t, hub.IsConnected("dev-2"))
	assert.False(t, hub.IsConnected("dev-1"))

	hub.UnregisterDeviceConn(dc2)

	hub.mu.RLock()
	_, channelExists := hub.channelDevices["section:7"]
	hub.mu.RUnlock()
	assert.False(t, channelExists, "channel map entry should be removed when empty")
}

func TestRegisterReplaceKeepsMostRecentConnection(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	dcOld := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-r", channelKeys: []string{"section:1", "device:dev-r"}}
	dcNew := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-r", channelKeys: []string{"section:1", "device:dev-r"}}

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "dev-r", dcOld, "section:1", "device:dev-r"))
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "dev-r", dcNew, "section:1", "device:dev-r"))

	assert.True(t, hub.IsConnected("dev-r"), "still connected after replacement")

	// Stale unregister must not remove the newer connection.
	hub.UnregisterDeviceConn(dcOld)
	assert.True(t, hub.IsConnected("dev-r"), "stale unregister must not disconnect the new connection")

	hub.UnregisterDeviceConn(dcNew)
	assert.False(t, hub.IsConnected("dev-r"))
}

func TestBroadcastToSectionDelivery(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "device-xyz", channelKeys: []string{"section:99", "device:device-xyz"}}

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "device-xyz", dc, "section:99", "device:device-xyz"))

	hub.BroadcastToSection("99", RefreshScoresMessage())

	select {
	case msg := <-send:
		assert.Equal(t, "refresh-scores", msg.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for refresh-scores message")
	}
}

func TestBroadcastToDeviceDelivery(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "device-per", channelKeys: []string{"section:55", "device:device-per"}}

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "device-per", dc, "section:55", "device:device-per"))

	hub.BroadcastToDevice("device-per", ReconnectMessage())

	select {
	case msg := <-send:
		assert.Equal(t, "reconnect", msg.Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect message")
	}
}

func TestBroadcastToUnknownSectionIsNoop(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	_ = startHub(t, hub)

	// No device registered for section 55 — BroadcastToSection must not panic.
	hub.BroadcastToSection("55", RefreshScoresMessage())
}

func TestCloseDisconnectsDevices(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx := startHub(t, hub)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "dev-close", channelKeys: []string{"section:1", "device:dev-close"}}

	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	defer regCancel()
	require.NoError(t, hub.RegisterDeviceAndSubscribe(regCtx, "dev-close", dc, "section:1", "device:dev-close"))

	hub.Close()

	select {
	case msg, ok := <-send:
		if ok {
			assert.Equal(t, "disconnect", msg.Type)
		}
		// Either a disconnect message or channel close is acceptable.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for disconnect on hub.Close()")
	}
}
