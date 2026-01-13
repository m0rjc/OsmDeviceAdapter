package middleware

import (
	"context"
	"testing"
)

// mockUser implements types.User for testing
type mockUser struct {
	userID      *int
	accessToken string
}

func (m *mockUser) UserID() *int {
	return m.userID
}

func (m *mockUser) AccessToken() string {
	return m.accessToken
}

func TestUserContext(t *testing.T) {
	ctx := context.Background()
	userID := 123
	expectedUser := &mockUser{
		userID:      &userID,
		accessToken: "test-token",
	}

	// Test adding user to context
	ctx = ContextWithUser(ctx, expectedUser)

	// Test retrieving user from context
	user, ok := UserFromContext(ctx)
	if !ok {
		t.Fatal("Expected user to be in context")
	}

	if user.AccessToken() != "test-token" {
		t.Errorf("Expected access token 'test-token', got '%s'", user.AccessToken())
	}

	if user.UserID() == nil || *user.UserID() != 123 {
		t.Errorf("Expected user ID 123, got %v", user.UserID())
	}
}

func TestUserContext_NotFound(t *testing.T) {
	ctx := context.Background()

	// Test retrieving from empty context
	_, ok := UserFromContext(ctx)
	if ok {
		t.Error("Expected user not to be in context")
	}
}

func TestRemoteMetadataContext(t *testing.T) {
	ctx := context.Background()
	expectedMetadata := RemoteMetadata{
		IP:       "1.2.3.4",
		Protocol: "https",
		Country:  "US",
	}

	// Test adding metadata to context
	ctx = ContextWithRemote(ctx, expectedMetadata)

	// Test retrieving metadata from context
	metadata := RemoteFromContext(ctx)

	if metadata.IP != "1.2.3.4" {
		t.Errorf("Expected IP '1.2.3.4', got '%s'", metadata.IP)
	}

	if metadata.Protocol != "https" {
		t.Errorf("Expected protocol 'https', got '%s'", metadata.Protocol)
	}

	if metadata.Country != "US" {
		t.Errorf("Expected country 'US', got '%s'", metadata.Country)
	}
}

func TestRemoteMetadataContext_Empty(t *testing.T) {
	ctx := context.Background()

	// Test retrieving from empty context
	metadata := RemoteFromContext(ctx)

	if metadata.IP != "" {
		t.Errorf("Expected empty IP, got '%s'", metadata.IP)
	}

	if metadata.Protocol != "" {
		t.Errorf("Expected empty protocol, got '%s'", metadata.Protocol)
	}

	if metadata.Country != "" {
		t.Errorf("Expected empty country, got '%s'", metadata.Country)
	}
}

func TestRemoteMetadataContext_Partial(t *testing.T) {
	ctx := context.Background()

	// Add only IP
	ctx = context.WithValue(ctx, remoteIPKey, "5.6.7.8")

	metadata := RemoteFromContext(ctx)

	if metadata.IP != "5.6.7.8" {
		t.Errorf("Expected IP '5.6.7.8', got '%s'", metadata.IP)
	}

	if metadata.Protocol != "" {
		t.Errorf("Expected empty protocol, got '%s'", metadata.Protocol)
	}

	if metadata.Country != "" {
		t.Errorf("Expected empty country, got '%s'", metadata.Country)
	}
}
