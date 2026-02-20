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
	// Full channel names: ws:section:{sectionID} or ws:adhoc:{osmUserID}
	redisChanPrefix = "ws:"
)

// deviceConn holds a single device's WebSocket connection state.
type deviceConn struct {
	hub         *Hub
	conn        *ws.Conn
	send        chan Message
	deviceCode  string
	channelKeys []string // routing keys, e.g. ["section:42", "device:abc123"]
}

// Hub is the in-memory registry of active device WebSocket connections.
// It bridges Redis pub/sub messages to locally-connected devices.
type Hub struct {
	mu             sync.RWMutex
	deviceConns    map[string]*deviceConn         // keyed by device code
	channelDevices map[string]map[string]struct{} // channelKey → set of device codes

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
		channelDevices: make(map[string]map[string]struct{}),
		redis:          redis,
		subCh:          make(chan string, 8),
		unsubCh:        make(chan string, 8),
		closeCh:        make(chan struct{}),
	}
}

// RegisterDevice adds dc to the hub and requests Redis subscriptions for the
// device's channels if they have no existing subscribers.
// channelKeys should include the section/adhoc routing key and the per-device key.
func (h *Hub) RegisterDevice(deviceCode string, dc *deviceConn, channelKeys ...string) {
	needsSub := make([]bool, len(channelKeys))

	h.mu.Lock()
	h.deviceConns[deviceCode] = dc
	for i, channelKey := range channelKeys {
		if h.channelDevices[channelKey] == nil {
			h.channelDevices[channelKey] = make(map[string]struct{})
		}
		needsSub[i] = len(h.channelDevices[channelKey]) == 0
		h.channelDevices[channelKey][deviceCode] = struct{}{}
	}
	h.mu.Unlock()

	slog.Info("websocket.hub.device_registered",
		"component", "websocket",
		"event", "hub.register",
		"device_code_prefix", deviceCode[:min(8, len(deviceCode))],
		"channel_keys", channelKeys,
	)

	for i, channelKey := range channelKeys {
		if needsSub[i] {
			select {
			case h.subCh <- redisChanPrefix + channelKey:
			default:
			}
		}
	}
}

// UnregisterDevice removes the device from the hub and requests Redis
// unsubscriptions for any channels that now have no remaining subscribers.
// channelKeys must match the values used in RegisterDevice.
func (h *Hub) UnregisterDevice(deviceCode string, channelKeys ...string) {
	var toUnsub []string

	h.mu.Lock()
	delete(h.deviceConns, deviceCode)
	for _, channelKey := range channelKeys {
		if devs, ok := h.channelDevices[channelKey]; ok {
			delete(devs, deviceCode)
			if len(devs) == 0 {
				delete(h.channelDevices, channelKey)
				toUnsub = append(toUnsub, channelKey)
			}
		}
	}
	h.mu.Unlock()

	slog.Info("websocket.hub.device_unregistered",
		"component", "websocket",
		"event", "hub.unregister",
		"device_code_prefix", deviceCode[:min(8, len(deviceCode))],
		"channel_keys", channelKeys,
	)

	for _, channelKey := range toUnsub {
		select {
		case h.unsubCh <- redisChanPrefix + channelKey:
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

// BroadcastToSection publishes msg to all devices connected for the given OSM sectionID.
func (h *Hub) BroadcastToSection(sectionID string, msg Message) {
	h.publish("section:"+sectionID, msg)
}

// BroadcastToAdhocUser publishes msg to all devices connected for the given
// OSM user's ad-hoc section.
func (h *Hub) BroadcastToAdhocUser(userID string, msg Message) {
	h.publish("adhoc:"+userID, msg)
}

// BroadcastToDevice publishes msg to the specific device identified by deviceCode.
func (h *Hub) BroadcastToDevice(deviceCode string, msg Message) {
	h.publish("device:"+deviceCode, msg)
}

// publish sends msg to the Redis pub/sub channel for channelKey.
func (h *Hub) publish(channelKey string, msg Message) {
	channel := redisChanPrefix + channelKey
	if err := h.redis.Publish(context.Background(), channel, msg); err != nil {
		slog.Error("websocket.hub.publish_failed",
			"component", "websocket",
			"event", "hub.publish_error",
			"channel_key", channelKey,
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
			channelKey := redisMsg.Channel[len(redisChanPrefix):]

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
			h.deliverToChannel(channelKey, msg)
		}
	}
}

// deliverToChannel sends msg to all locally-registered devices for channelKey.
func (h *Hub) deliverToChannel(channelKey string, msg Message) {
	h.mu.RLock()
	devs := h.channelDevices[channelKey]
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
				"channel_key", channelKey,
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
		dc.hub.UnregisterDevice(dc.deviceCode, dc.channelKeys...)
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
				"channel_keys", dc.channelKeys,
				"uptime", msg.Uptime,
			)
		}
	}
}
