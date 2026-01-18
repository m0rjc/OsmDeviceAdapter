package devicesession

import (
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

func TestDeleteExpired(t *testing.T) {
	conns := db.SetupTestDB(t)
	now := time.Now()

	// Create test data: mix of expired and non-expired sessions
	expiredSession1 := &db.DeviceSession{
		SessionID:  "expired-session-1",
		DeviceCode: "device-1",
		ExpiresAt:  now.Add(-1 * time.Hour), // Expired 1 hour ago
	}
	expiredSession2 := &db.DeviceSession{
		SessionID:  "expired-session-2",
		DeviceCode: "device-2",
		ExpiresAt:  now.Add(-24 * time.Hour), // Expired 1 day ago
	}
	validSession := &db.DeviceSession{
		SessionID:  "valid-session",
		DeviceCode: "device-3",
		ExpiresAt:  now.Add(1 * time.Hour), // Expires in 1 hour
	}

	// Insert test data
	if err := Create(conns, expiredSession1); err != nil {
		t.Fatalf("Failed to create expired session 1: %v", err)
	}
	if err := Create(conns, expiredSession2); err != nil {
		t.Fatalf("Failed to create expired session 2: %v", err)
	}
	if err := Create(conns, validSession); err != nil {
		t.Fatalf("Failed to create valid session: %v", err)
	}

	// Run cleanup
	if err := DeleteExpired(conns); err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}

	// Verify expired sessions are deleted
	found1, err := FindByID(conns, "expired-session-1")
	if err != nil {
		t.Fatalf("Error checking expired-session-1: %v", err)
	}
	if found1 != nil {
		t.Error("Expected expired-session-1 to be deleted")
	}

	found2, err := FindByID(conns, "expired-session-2")
	if err != nil {
		t.Fatalf("Error checking expired-session-2: %v", err)
	}
	if found2 != nil {
		t.Error("Expected expired-session-2 to be deleted")
	}

	// Verify valid session still exists
	found3, err := FindByID(conns, "valid-session")
	if err != nil {
		t.Fatalf("Error checking valid-session: %v", err)
	}
	if found3 == nil {
		t.Error("Expected valid-session to still exist")
	}
}

func TestDeleteExpired_EmptyDatabase(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Run cleanup on empty database (should not error)
	if err := DeleteExpired(conns); err != nil {
		t.Fatalf("DeleteExpired should not fail on empty database: %v", err)
	}
}

func TestDeleteExpired_OnlyValidSessions(t *testing.T) {
	conns := db.SetupTestDB(t)
	now := time.Now()

	// Create only valid sessions
	session1 := &db.DeviceSession{
		SessionID:  "valid-session-1",
		DeviceCode: "device-1",
		ExpiresAt:  now.Add(10 * time.Minute),
	}
	session2 := &db.DeviceSession{
		SessionID:  "valid-session-2",
		DeviceCode: "device-2",
		ExpiresAt:  now.Add(1 * time.Hour),
	}

	if err := Create(conns, session1); err != nil {
		t.Fatalf("Failed to create session 1: %v", err)
	}
	if err := Create(conns, session2); err != nil {
		t.Fatalf("Failed to create session 2: %v", err)
	}

	// Run cleanup
	if err := DeleteExpired(conns); err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}

	// Verify both sessions still exist
	found1, err := FindByID(conns, "valid-session-1")
	if err != nil {
		t.Fatalf("Error checking valid-session-1: %v", err)
	}
	if found1 == nil {
		t.Error("Expected valid-session-1 to still exist")
	}

	found2, err := FindByID(conns, "valid-session-2")
	if err != nil {
		t.Fatalf("Error checking valid-session-2: %v", err)
	}
	if found2 == nil {
		t.Error("Expected valid-session-2 to still exist")
	}
}
