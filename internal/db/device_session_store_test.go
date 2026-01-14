package db

import (
	"testing"
	"time"
)

func TestDeleteExpiredDeviceSessions(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create test data: mix of expired and non-expired sessions
	expiredSession1 := &DeviceSession{
		SessionID:  "expired-session-1",
		DeviceCode: "device-1",
		ExpiresAt:  now.Add(-1 * time.Hour), // Expired 1 hour ago
	}
	expiredSession2 := &DeviceSession{
		SessionID:  "expired-session-2",
		DeviceCode: "device-2",
		ExpiresAt:  now.Add(-24 * time.Hour), // Expired 1 day ago
	}
	validSession := &DeviceSession{
		SessionID:  "valid-session",
		DeviceCode: "device-3",
		ExpiresAt:  now.Add(1 * time.Hour), // Expires in 1 hour
	}

	// Insert test data
	if err := CreateDeviceSession(conns, expiredSession1); err != nil {
		t.Fatalf("Failed to create expired session 1: %v", err)
	}
	if err := CreateDeviceSession(conns, expiredSession2); err != nil {
		t.Fatalf("Failed to create expired session 2: %v", err)
	}
	if err := CreateDeviceSession(conns, validSession); err != nil {
		t.Fatalf("Failed to create valid session: %v", err)
	}

	// Run cleanup
	if err := DeleteExpiredDeviceSessions(conns); err != nil {
		t.Fatalf("DeleteExpiredDeviceSessions failed: %v", err)
	}

	// Verify expired sessions are deleted
	found1, err := FindDeviceSessionByID(conns, "expired-session-1")
	if err != nil {
		t.Fatalf("Error checking expired-session-1: %v", err)
	}
	if found1 != nil {
		t.Error("Expected expired-session-1 to be deleted")
	}

	found2, err := FindDeviceSessionByID(conns, "expired-session-2")
	if err != nil {
		t.Fatalf("Error checking expired-session-2: %v", err)
	}
	if found2 != nil {
		t.Error("Expected expired-session-2 to be deleted")
	}

	// Verify valid session still exists
	found3, err := FindDeviceSessionByID(conns, "valid-session")
	if err != nil {
		t.Fatalf("Error checking valid-session: %v", err)
	}
	if found3 == nil {
		t.Error("Expected valid-session to still exist")
	}
}

func TestDeleteExpiredDeviceSessions_EmptyDatabase(t *testing.T) {
	conns := setupTestDB(t)

	// Run cleanup on empty database (should not error)
	if err := DeleteExpiredDeviceSessions(conns); err != nil {
		t.Fatalf("DeleteExpiredDeviceSessions should not fail on empty database: %v", err)
	}
}

func TestDeleteExpiredDeviceSessions_OnlyValidSessions(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create only valid sessions
	session1 := &DeviceSession{
		SessionID:  "valid-session-1",
		DeviceCode: "device-1",
		ExpiresAt:  now.Add(10 * time.Minute),
	}
	session2 := &DeviceSession{
		SessionID:  "valid-session-2",
		DeviceCode: "device-2",
		ExpiresAt:  now.Add(1 * time.Hour),
	}

	if err := CreateDeviceSession(conns, session1); err != nil {
		t.Fatalf("Failed to create session 1: %v", err)
	}
	if err := CreateDeviceSession(conns, session2); err != nil {
		t.Fatalf("Failed to create session 2: %v", err)
	}

	// Run cleanup
	if err := DeleteExpiredDeviceSessions(conns); err != nil {
		t.Fatalf("DeleteExpiredDeviceSessions failed: %v", err)
	}

	// Verify both sessions still exist
	found1, err := FindDeviceSessionByID(conns, "valid-session-1")
	if err != nil {
		t.Fatalf("Error checking valid-session-1: %v", err)
	}
	if found1 == nil {
		t.Error("Expected valid-session-1 to still exist")
	}

	found2, err := FindDeviceSessionByID(conns, "valid-session-2")
	if err != nil {
		t.Fatalf("Error checking valid-session-2: %v", err)
	}
	if found2 == nil {
		t.Error("Expected valid-session-2 to still exist")
	}
}
