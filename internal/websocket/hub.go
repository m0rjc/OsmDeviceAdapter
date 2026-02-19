package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

const (
	pingInterval   = 30 * time.Second
	pongTimeout    = 60 * time.Second
	idleTimeout    = 30 * time.Minute
	writeTimeout   = 10 * time.Second
	readLimit      = 512
	sendBufferSize = 16
	// redisChanPrefix is the prefix for pub/sub channel names. Not a key prefix.
	redisChanPrefix = "ws:section:"
)

// deviceConn holds a single device's WebSocket connection state.
type deviceConn struct {
	hub        *Hub
	conn       *ws.Conn
	send       chan Message
	deviceCode string
	sectionID  string
}

// Hub is the in-memory registry of active device WebSocket connections.
// It bridges Redis pub/sub messages to locally-connected devices.
type Hub struct {
	mu             sync.RWMutex
	deviceConns    map[string]*deviceConn         // keyed by device code
	sectionDevices map[string]map[string]struct{} // sectionID → set of device codes

	redis *db.RedisClient

	// subCh and unsubCh carry channel names to the Run goroutine.
	subCh   chan string
	unsubCh chan string
	closeCh chan struct{}
}

// NewHub creates a new Hub backed by the given RedisClient.
func NewHub(redis *db.RedisClient) *Hub {
	return &Hub{
		deviceConns:    make(map[string]*deviceConn),
		sectionDevices: make(map[string]map[string]struct{}),
		redis:          redis,
		subCh:          make(chan string, 8),
		unsubCh:        make(chan string, 8),
		closeCh:        make(chan struct{}),
	}
}

// RegisterDevice adds dc to the hub and requests a Redis subscription for the
// device's section channel if this is the first device in that section.
func (h *Hub) RegisterDevice(deviceCode, sectionID string, dc *deviceConn) {
	h.mu.Lock()
	h.deviceConns[deviceCode] = dc
	if h.sectionDevices[sectionID] == nil {
		h.sectionDevices[sectionID] = make(map[string]struct{})
	}
	wasEmpty := len(h.sectionDevices[sectionID]) == 0
	h.sectionDevices[sectionID][deviceCode] = struct{}{}
	h.mu.Unlock()

	slog.Info("websocket.hub.device_registered",
		"component", "websocket",
		"event", "hub.register",
		"device_code_prefix", deviceCode[:min(8, len(deviceCode))],
		"section_id", sectionID,
	)

	if wasEmpty {
		select {
		case h.subCh <- redisChanPrefix + sectionID:
		default:
		}
	}
}

// UnregisterDevice removes the device from the hub and requests a Redis
// unsubscription if no other devices remain in that section.
func (h *Hub) UnregisterDevice(deviceCode, sectionID string) {
	h.mu.Lock()
	delete(h.deviceConns, deviceCode)
	if devs, ok := h.sectionDevices[sectionID]; ok {
		delete(devs, deviceCode)
		if len(devs) == 0 {
			delete(h.sectionDevices, sectionID)
		}
	}
	nowEmpty := h.sectionDevices[sectionID] == nil
	h.mu.Unlock()

	slog.Info("websocket.hub.device_unregistered",
		"component", "websocket",
		"event", "hub.unregister",
		"device_code_prefix", deviceCode[:min(8, len(deviceCode))],
		"section_id", sectionID,
	)

	if nowEmpty {
		select {
		case h.unsubCh <- redisChanPrefix + sectionID:
		default:
		}
	}
}

// IsConnected reports whether a device with the given code is currently
// registered in this Hub instance.
func (h *Hub) IsConnected(deviceCode string) bool {
	h.mu.RLock()
	_, ok := h.deviceConns[deviceCode]
	h.mu.RUnlock()
	return ok
}

// BroadcastToSection publishes msg to the Redis pub/sub channel for sectionID.
// All Hub instances (on any server) subscribed to that channel will deliver
// the message to their locally-connected devices.
func (h *Hub) BroadcastToSection(sectionID string, msg Message) {
	channel := redisChanPrefix + sectionID
	if err := h.redis.Publish(context.Background(), channel, msg); err != nil {
		slog.Error("websocket.hub.publish_failed",
			"component", "websocket",
			"event", "hub.publish_error",
			"section_id", sectionID,
			"error", err,
		)
	}
}

// Run starts the hub's Redis pub/sub listener. Call it in a goroutine.
// It blocks until ctx is cancelled or Close is called.
func (h *Hub) Run(ctx context.Context) {
	pubSub := h.redis.Subscribe(ctx)
	defer pubSub.Close()

	msgCh := pubSub.Channel()

	for {
		select {
		case <-ctx.Done():
			h.closeAllConnections("server shutting down")
			return
		case <-h.closeCh:
			h.closeAllConnections("hub closed")
			return

		case channel := <-h.subCh:
			if err := pubSub.Subscribe(ctx, channel); err != nil {
				slog.Error("websocket.hub.subscribe_failed",
					"component", "websocket",
					"event", "hub.subscribe_error",
					"channel", channel,
					"error", err,
				)
			}

		case channel := <-h.unsubCh:
			if err := pubSub.Unsubscribe(ctx, channel); err != nil {
				slog.Error("websocket.hub.unsubscribe_failed",
					"component", "websocket",
					"event", "hub.unsubscribe_error",
					"channel", channel,
					"error", err,
				)
			}

		case redisMsg, ok := <-msgCh:
			if !ok {
				return
			}
			if len(redisMsg.Channel) <= len(redisChanPrefix) {
				continue
			}
			sectionID := redisMsg.Channel[len(redisChanPrefix):]

			var msg Message
			if err := json.Unmarshal([]byte(redisMsg.Payload), &msg); err != nil {
				slog.Warn("websocket.hub.bad_redis_payload",
					"component", "websocket",
					"event", "hub.decode_error",
					"channel", redisMsg.Channel,
					"error", err,
				)
				continue
			}
			h.deliverToSection(sectionID, msg)
		}
	}
}

// deliverToSection sends msg to all locally-registered devices for sectionID.
func (h *Hub) deliverToSection(sectionID string, msg Message) {
	h.mu.RLock()
	devs := h.sectionDevices[sectionID]
	conns := make([]*deviceConn, 0, len(devs))
	for code := range devs {
		if dc, ok := h.deviceConns[code]; ok {
			conns = append(conns, dc)
		}
	}
	h.mu.RUnlock()

	for _, dc := range conns {
		select {
		case dc.send <- msg:
		default:
			slog.Warn("websocket.hub.send_buffer_full",
				"component", "websocket",
				"event", "hub.drop_message",
				"device_code_prefix", dc.deviceCode[:min(8, len(dc.deviceCode))],
				"section_id", sectionID,
			)
		}
	}
}

// closeAllConnections sends a disconnect message to every connected device and
// closes their send channels, causing their write pumps to terminate.
func (h *Hub) closeAllConnections(reason string) {
	h.mu.Lock()
	conns := make([]*deviceConn, 0, len(h.deviceConns))
	for _, dc := range h.deviceConns {
		conns = append(conns, dc)
	}
	h.mu.Unlock()

	msg := DisconnectMessage(reason)
	for _, dc := range conns {
		select {
		case dc.send <- msg:
		default:
		}
		close(dc.send)
	}
}

// Close shuts down the hub, disconnecting all devices.
func (h *Hub) Close() {
	close(h.closeCh)
}

// writePump runs in a goroutine per device. It writes outgoing messages,
// sends periodic pings, and closes the connection after idleTimeout.
func (dc *deviceConn) writePump() {
	pingTicker := time.NewTicker(pingInterval)
	idleTimer := time.NewTimer(idleTimeout)
	defer func() {
		pingTicker.Stop()
		idleTimer.Stop()
		dc.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-dc.send:
			dc.conn.SetWriteDeadline(time.Now().Add(writeTimeout)) //nolint:errcheck
			if !ok {
				dc.conn.WriteMessage(ws.CloseMessage, ws.FormatCloseMessage(ws.CloseNormalClosure, "")) //nolint:errcheck
				return
			}
			if err := dc.conn.WriteJSON(msg); err != nil {
				return
			}
			// Reset idle timer after each outbound message.
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

		case <-pingTicker.C:
			dc.conn.SetWriteDeadline(time.Now().Add(writeTimeout)) //nolint:errcheck
			if err := dc.conn.WriteMessage(ws.PingMessage, nil); err != nil {
				return
			}

		case <-idleTimer.C:
			dc.conn.SetWriteDeadline(time.Now().Add(writeTimeout)) //nolint:errcheck
			dc.conn.WriteJSON(DisconnectMessage("idle timeout"))   //nolint:errcheck
			return
		}
	}
}

// readPump runs in the handler goroutine. It reads incoming messages from the
// device, logging "status" payloads. When it returns the device is unregistered.
func (dc *deviceConn) readPump() {
	defer func() {
		dc.hub.UnregisterDevice(dc.deviceCode, dc.sectionID)
		dc.conn.Close()
	}()

	dc.conn.SetReadLimit(readLimit)
	dc.conn.SetReadDeadline(time.Now().Add(pongTimeout)) //nolint:errcheck
	dc.conn.SetPongHandler(func(string) error {
		return dc.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	})

	for {
		var msg Message
		if err := dc.conn.ReadJSON(&msg); err != nil {
			if ws.IsUnexpectedCloseError(err, ws.CloseGoingAway, ws.CloseAbnormalClosure, ws.CloseNormalClosure) {
				slog.Warn("websocket.device.unexpected_close",
					"component", "websocket",
					"event", "device.read_error",
					"device_code_prefix", dc.deviceCode[:min(8, len(dc.deviceCode))],
					"error", err,
				)
			}
			break
		}

		if msg.Type == "status" {
			slog.Debug("websocket.device.status",
				"component", "websocket",
				"event", "device.status",
				"device_code_prefix", dc.deviceCode[:min(8, len(dc.deviceCode))],
				"section_id", dc.sectionID,
				"uptime", msg.Uptime,
			)
		}
	}
}
