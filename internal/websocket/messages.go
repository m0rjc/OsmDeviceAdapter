package websocket

// Message is a JSON message sent or received on the device WebSocket.
type Message struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"` // used in "disconnect" messages
	Uptime int64  `json:"uptime,omitempty"` // used in "status" messages (device→server)
}

// RefreshScoresMessage creates a server→device message asking the device to reload scores.
func RefreshScoresMessage() Message {
	return Message{Type: "refresh-scores"}
}

// DisconnectMessage creates a server→device message indicating the connection is closing.
func DisconnectMessage(reason string) Message {
	return Message{Type: "disconnect", Reason: reason}
}

// ReconnectMessage creates a server→device message asking the device to drop
// its current WebSocket connection and reconnect. Used when the device's section
// assignment changes so it resubscribes to the correct channel.
func ReconnectMessage() Message {
	return Message{Type: "reconnect"}
}
