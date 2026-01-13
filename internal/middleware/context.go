package middleware

import (
	"context"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

// Context keys for storing request metadata
type contextKey string

const (
	userContextKey    contextKey = "user"
	remoteIPKey       contextKey = "remote_ip"
	remoteProtocolKey contextKey = "remote_protocol"
	remoteCountryKey  contextKey = "remote_country"
)

// RemoteMetadata holds remote request metadata (e.g., from reverse proxy headers)
type RemoteMetadata struct {
	IP       string
	Protocol string
	Country  string
}

// ContextWithUser adds a user to the context
func ContextWithUser(ctx context.Context, user types.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext retrieves the user from the context
func UserFromContext(ctx context.Context) (types.User, bool) {
	user, ok := ctx.Value(userContextKey).(types.User)
	return user, ok
}

// ContextWithRemote adds remote metadata to the context
func ContextWithRemote(ctx context.Context, metadata RemoteMetadata) context.Context {
	ctx = context.WithValue(ctx, remoteIPKey, metadata.IP)
	ctx = context.WithValue(ctx, remoteProtocolKey, metadata.Protocol)
	ctx = context.WithValue(ctx, remoteCountryKey, metadata.Country)
	return ctx
}

// RemoteFromContext retrieves remote metadata from the context
func RemoteFromContext(ctx context.Context) RemoteMetadata {
	return RemoteMetadata{
		IP:       getStringFromContext(ctx, remoteIPKey),
		Protocol: getStringFromContext(ctx, remoteProtocolKey),
		Country:  getStringFromContext(ctx, remoteCountryKey),
	}
}

func getStringFromContext(ctx context.Context, key contextKey) string {
	if val, ok := ctx.Value(key).(string); ok {
		return val
	}
	return ""
}
