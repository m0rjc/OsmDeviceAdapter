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
	return rc, mr
}

func TestRegisterUnregister(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	dc := &deviceConn{
		hub:         hub,
		send:        make(chan Message, 1),
		deviceCode:  "device-abc",
		channelKeys: []string{"section:42", "device:device-abc"},
	}

	assert.False(t, hub.IsConnected("device-abc"), "not connected before register")

	hub.RegisterDevice("device-abc", dc, "section:42", "device:device-abc")
	assert.True(t, hub.IsConnected("device-abc"), "connected after register")

	hub.UnregisterDevice("device-abc", "section:42", "device:device-abc")
	assert.False(t, hub.IsConnected("device-abc"), "not connected after unregister")
}

func TestRegisterUnregisterSectionTracking(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	dc1 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-1", channelKeys: []string{"section:7", "device:dev-1"}}
	dc2 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-2", channelKeys: []string{"section:7", "device:dev-2"}}

	hub.RegisterDevice("dev-1", dc1, "section:7", "device:dev-1")
	hub.RegisterDevice("dev-2", dc2, "section:7", "device:dev-2")

	hub.UnregisterDevice("dev-1", "section:7", "device:dev-1")

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

	hub.UnregisterDevice("dev-2", "section:7", "device:dev-2")

	hub.mu.RLock()
	_, channelExists := hub.channelDevices["section:7"]
	hub.mu.RUnlock()
	assert.False(t, channelExists, "channel map entry should be removed when empty")
}

func TestBroadcastToSectionDelivery(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give hub.Run time to set up its Redis subscription goroutine.
	time.Sleep(20 * time.Millisecond)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "device-xyz", channelKeys: []string{"section:99", "device:device-xyz"}}
	hub.RegisterDevice("device-xyz", dc, "section:99", "device:device-xyz")

	// Allow the subscribe request to be processed by hub.Run.
	time.Sleep(50 * time.Millisecond)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Give hub.Run time to set up its Redis subscription goroutine.
	time.Sleep(20 * time.Millisecond)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "device-per", channelKeys: []string{"section:55", "device:device-per"}}
	hub.RegisterDevice("device-per", dc, "section:55", "device:device-per")

	// Allow the subscribe request to be processed by hub.Run.
	time.Sleep(50 * time.Millisecond)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// No device registered for section 55 — BroadcastToSection must not panic.
	hub.BroadcastToSection("55", RefreshScoresMessage())
}

func TestCloseDisconnectsDevices(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	time.Sleep(20 * time.Millisecond)

	send := make(chan Message, 4)
	dc := &deviceConn{hub: hub, send: send, deviceCode: "dev-close", channelKeys: []string{"section:1", "device:dev-close"}}
	hub.RegisterDevice("dev-close", dc, "section:1", "device:dev-close")

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
