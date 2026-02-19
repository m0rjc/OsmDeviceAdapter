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
		hub:        hub,
		send:       make(chan Message, 1),
		deviceCode: "device-abc",
		sectionID:  "42",
	}

	assert.False(t, hub.IsConnected("device-abc"), "not connected before register")

	hub.RegisterDevice("device-abc", "42", dc)
	assert.True(t, hub.IsConnected("device-abc"), "connected after register")

	hub.UnregisterDevice("device-abc", "42")
	assert.False(t, hub.IsConnected("device-abc"), "not connected after unregister")
}

func TestRegisterUnregisterSectionTracking(t *testing.T) {
	rc, _ := newTestRedis(t)
	hub := NewHub(rc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	dc1 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-1", sectionID: "7"}
	dc2 := &deviceConn{hub: hub, send: make(chan Message, 1), deviceCode: "dev-2", sectionID: "7"}

	hub.RegisterDevice("dev-1", "7", dc1)
	hub.RegisterDevice("dev-2", "7", dc2)

	hub.UnregisterDevice("dev-1", "7")

	// Section should still have dev-2
	hub.mu.RLock()
	devs := hub.sectionDevices["7"]
	_, hasDev2 := devs["dev-2"]
	hub.mu.RUnlock()

	assert.True(t, hasDev2, "section 7 should still track dev-2")
	assert.True(t, hub.IsConnected("dev-2"))
	assert.False(t, hub.IsConnected("dev-1"))

	hub.UnregisterDevice("dev-2", "7")

	hub.mu.RLock()
	_, sectionExists := hub.sectionDevices["7"]
	hub.mu.RUnlock()
	assert.False(t, sectionExists, "section map entry should be removed when empty")
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
	dc := &deviceConn{hub: hub, send: send, deviceCode: "device-xyz", sectionID: "99"}
	hub.RegisterDevice("device-xyz", "99", dc)

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
	dc := &deviceConn{hub: hub, send: send, deviceCode: "dev-close", sectionID: "1"}
	hub.RegisterDevice("dev-close", "1", dc)

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
