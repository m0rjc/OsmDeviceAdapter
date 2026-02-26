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

type subscribeReq struct {
	channel string
	respCh  chan error // buffered (size 1) so hub.Run never blocks
}

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
	subCh     chan subscribeReq
	unsubCh   chan string
	closeCh   chan struct{}
	closeOnce sync.Once
}

// NewHub creates a new Hub backed by the given RedisClient.
func NewHub(redis *db.RedisClient) *Hub {
	return &Hub{
		deviceConns:    make(map[string]*deviceConn),
		channelDevices: make(map[string]map[string]struct{}),
		redis:          redis,
		subCh:          make(chan subscribeReq, 8),
		unsubCh:        make(chan string, 8),
		closeCh:        make(chan struct{}),
	}
}

func (h *Hub) subscribeSync(ctx context.Context, channel string) error {
	respCh := make(chan error, 1)
	req := subscribeReq{channel: channel, respCh: respCh}

	// Subscription requests MUST NOT be dropped: if we accept a WebSocket connection
	// but fail to subscribe to its Redis channels, the hub will silently miss messages.
	// We therefore block here (bounded by ctx timeout) to keep semantics correct.
	select {
	case h.subCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-respCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RegisterDeviceAndSubscribe registers dc and ensures required Redis subscriptions
// are in place before returning. If subscription fails, the connection is
// unregistered so the client can retry cleanly.
func (h *Hub) RegisterDeviceAndSubscribe(ctx context.Context, deviceCode string, dc *deviceConn, channelKeys ...string) error {
	needsSub := make([]bool, len(channelKeys))
	var toUnsub []string
	var replaced *deviceConn

	h.mu.Lock()

	// If this device is already connected, replace the old connection.
	if old := h.deviceConns[deviceCode]; old != nil && old != dc {
		replaced = old

		// Remove stale channel tracking tied to the previous connection's channel keys.
		for _, channelKey := range old.channelKeys {
			if devs, ok := h.channelDevices[channelKey]; ok {
				delete(devs, deviceCode)
				if len(devs) == 0 {
					delete(h.channelDevices, channelKey)
					toUnsub = append(toUnsub, channelKey)
				}
			}
		}
	}

	// Register the new connection.
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

	// Unsubscribe requests are best-effort, but we still avoid silent drops:
	// block unless ctx is done.
	for _, channelKey := range toUnsub {
		select {
		case h.unsubCh <- redisChanPrefix + channelKey:
		case <-ctx.Done():
			// If we're already timing out/failing the connect, don't hang here.
			return ctx.Err()
		}
	}

	// Ensure subscriptions are actually applied before we proceed.
	for i, channelKey := range channelKeys {
		if !needsSub[i] {
			continue
		}
		if err := h.subscribeSync(ctx, redisChanPrefix+channelKey); err != nil {
			slog.Error("websocket.hub.subscribe_failed_before_accept",
				"component", "websocket",
				"event", "hub.subscribe_sync_error",
				"channel", redisChanPrefix+channelKey,
				"device_code_prefix", deviceCode[:min(8, len(deviceCode))],
				"error", err,
			)
			h.UnregisterDeviceConn(dc)
			return err
		}
	}

	// Ask the replaced connection to disconnect (outside the hub lock).
	if replaced != nil {
		select {
		case replaced.send <- DisconnectMessage("replaced by new connection"):
		default:
		}
		close(replaced.send)
		if replaced.conn != nil {
			replaced.conn.Close() //nolint:errcheck
		}
	}

	return nil
}

// IsConnected reports whether a device with the given code is currently
// registered in this Hub instance.
func (h *Hub) IsConnected(deviceCode string) bool {
	h.mu.RLock()
	_, ok := h.deviceConns[deviceCode]
	h.mu.RUnlock()
	return ok
}

// UnregisterDeviceConn removes a specific connection from the hub and requests Redis
// unsubscriptions for any channels that now have no remaining subscribers.
//
// IMPORTANT: This is connection-aware to avoid a stale connection unregistering
// a newer replacement that reused the same device code.
func (h *Hub) UnregisterDeviceConn(dc *deviceConn) {
	if dc == nil {
		return
	}

	var toUnsub []string

	h.mu.Lock()

	current := h.deviceConns[dc.deviceCode]
	if current != dc {
		h.mu.Unlock()
		return
	}

	delete(h.deviceConns, dc.deviceCode)
	for _, channelKey := range dc.channelKeys {
		if devs, ok := h.channelDevices[channelKey]; ok {
			delete(devs, dc.deviceCode)
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
		"device_code_prefix", dc.deviceCode[:min(8, len(dc.deviceCode))],
		"channel_keys", dc.channelKeys,
	)

	// Unsubscribe is best-effort; block unless the hub is shutting down.
	for _, channelKey := range toUnsub {
		// No ctx here; avoid deadlock by allowing shutdown to proceed.
		select {
		case h.unsubCh <- redisChanPrefix + channelKey:
		case <-h.closeCh:
			return
		}
	}
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

	// Events() uses ChannelWithSubscriptions so we receive both subscription
	// confirmations and actual messages. This allows subscribeSync to wait
	// until Redis has truly registered the subscription before returning,
	// eliminating the race between SUBSCRIBE and a subsequent PUBLISH.
	eventCh := pubSub.Events()

	// pendingSubs maps a fully-prefixed channel name to the response channel
	// of the subscribeSync call awaiting Redis confirmation.
	pendingSubs := make(map[string]chan<- error)

	for {
		select {
		case <-ctx.Done():
			h.closeAllConnections("server shutting down")
			return
		case <-h.closeCh:
			h.closeAllConnections("hub closed")
			return

		case req := <-h.subCh:
			err := pubSub.Subscribe(ctx, req.channel)
			if err != nil {
				// Send error immediately; no confirmation will arrive.
				select {
				case req.respCh <- err:
				default:
				}
			} else {
				// Confirmation will arrive as a PubSubSubscribed event.
				pendingSubs[req.channel] = req.respCh
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

		case event, ok := <-eventCh:
			if !ok {
				return
			}
			switch event.Kind {
			case db.PubSubSubscribed:
				// Redis confirmed the subscription; unblock the waiting subscribeSync.
				if respCh, ok := pendingSubs[event.Channel]; ok {
					delete(pendingSubs, event.Channel)
					select {
					case respCh <- nil:
					default:
					}
				}

			case db.PubSubMessage:
				if len(event.Channel) <= len(redisChanPrefix) {
					continue
				}
				channelKey := event.Channel[len(redisChanPrefix):]
				var msg Message
				if err := json.Unmarshal([]byte(event.Payload), &msg); err != nil {
					slog.Warn("websocket.hub.bad_redis_payload",
						"component", "websocket",
						"event", "hub.decode_error",
						"channel", event.Channel,
						"error", err,
					)
					continue
				}
				h.deliverToChannel(channelKey, msg)
			}
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
		// Per-connection send is deliberately non-blocking:
		// a slow/unhealthy client must not stall delivery to all other clients.
		// When the buffer is full we drop the message and rely on the next refresh/update.
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

// Close shuts down the hub, disconnecting all devices. Safe to call multiple times.
func (h *Hub) Close() {
	h.closeOnce.Do(func() { close(h.closeCh) })
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
		dc.hub.UnregisterDeviceConn(dc)
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
