package db

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *Connections {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Auto-migrate tables
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	return NewConnections(db, nil)
}

func TestDeleteExpiredDeviceCodes(t *testing.T) {
	conns := setupTestDB(t)

	// Create test data: mix of expired and non-expired device codes
	now := time.Now()
	expiredCode1 := &DeviceCode{
		DeviceCode: "expired-1",
		UserCode:   "EXP1",
		ClientID:   "test-client",
		Status:     "pending",
		ExpiresAt:  now.Add(-1 * time.Hour), // Expired 1 hour ago
	}
	expiredCode2 := &DeviceCode{
		DeviceCode: "expired-2",
		UserCode:   "EXP2",
		ClientID:   "test-client",
		Status:     "authorized",
		ExpiresAt:  now.Add(-24 * time.Hour), // Expired 1 day ago
	}
	validCode := &DeviceCode{
		DeviceCode: "valid-1",
		UserCode:   "VAL1",
		ClientID:   "test-client",
		Status:     "pending",
		ExpiresAt:  now.Add(1 * time.Hour), // Expires in 1 hour
	}

	// Insert test data
	if err := CreateDeviceCode(conns, expiredCode1); err != nil {
		t.Fatalf("Failed to create expired code 1: %v", err)
	}
	if err := CreateDeviceCode(conns, expiredCode2); err != nil {
		t.Fatalf("Failed to create expired code 2: %v", err)
	}
	if err := CreateDeviceCode(conns, validCode); err != nil {
		t.Fatalf("Failed to create valid code: %v", err)
	}

	// Run cleanup
	if err := DeleteExpiredDeviceCodes(conns); err != nil {
		t.Fatalf("DeleteExpiredDeviceCodes failed: %v", err)
	}

	// Verify expired codes are deleted
	found1, err := FindDeviceCodeByCode(conns, "expired-1")
	if err != nil {
		t.Fatalf("Error checking expired-1: %v", err)
	}
	if found1 != nil {
		t.Error("Expected expired-1 to be deleted")
	}

	found2, err := FindDeviceCodeByCode(conns, "expired-2")
	if err != nil {
		t.Fatalf("Error checking expired-2: %v", err)
	}
	if found2 != nil {
		t.Error("Expected expired-2 to be deleted")
	}

	// Verify valid code still exists
	found3, err := FindDeviceCodeByCode(conns, "valid-1")
	if err != nil {
		t.Fatalf("Error checking valid-1: %v", err)
	}
	if found3 == nil {
		t.Error("Expected valid-1 to still exist")
	}
}

func TestDeleteUnusedDeviceCodes(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	t.Run("deletes authorized devices not used for threshold period", func(t *testing.T) {
		// Create an authorized device that hasn't been used in 60 days
		oldDevice := &DeviceCode{
			DeviceCode: "old-device",
			UserCode:   "OLD1",
			ClientID:   "test-client",
			Status:     "authorized",
			ExpiresAt:  now.Add(24 * time.Hour),
			LastUsedAt: ptrTime(now.Add(-60 * 24 * time.Hour)), // 60 days ago
		}
		if err := CreateDeviceCode(conns, oldDevice); err != nil {
			t.Fatalf("Failed to create old device: %v", err)
		}

		// Run cleanup with 30-day threshold
		threshold := 30 * 24 * time.Hour
		if err := DeleteUnusedDeviceCodes(conns, threshold); err != nil {
			t.Fatalf("DeleteUnusedDeviceCodes failed: %v", err)
		}

		// Verify device was deleted
		found, err := FindDeviceCodeByCode(conns, "old-device")
		if err != nil {
			t.Fatalf("Error checking old-device: %v", err)
		}
		if found != nil {
			t.Error("Expected old-device to be deleted")
		}
	})

	t.Run("keeps recently used devices", func(t *testing.T) {
		// Create a recently used device
		recentDevice := &DeviceCode{
			DeviceCode: "recent-device",
			UserCode:   "REC1",
			ClientID:   "test-client",
			Status:     "authorized",
			ExpiresAt:  now.Add(24 * time.Hour),
			LastUsedAt: ptrTime(now.Add(-10 * 24 * time.Hour)), // 10 days ago
		}
		if err := CreateDeviceCode(conns, recentDevice); err != nil {
			t.Fatalf("Failed to create recent device: %v", err)
		}

		// Run cleanup with 30-day threshold
		threshold := 30 * 24 * time.Hour
		if err := DeleteUnusedDeviceCodes(conns, threshold); err != nil {
			t.Fatalf("DeleteUnusedDeviceCodes failed: %v", err)
		}

		// Verify device still exists
		found, err := FindDeviceCodeByCode(conns, "recent-device")
		if err != nil {
			t.Fatalf("Error checking recent-device: %v", err)
		}
		if found == nil {
			t.Error("Expected recent-device to still exist")
		}
	})

	t.Run("deletes devices with null last_used_at older than threshold", func(t *testing.T) {
		// Create a device that was authorized long ago but never used
		// In practice, last_used_at would be set on first use, but this tests the edge case
		neverUsedDevice := &DeviceCode{
			DeviceCode: "never-used",
			UserCode:   "NEV1",
			ClientID:   "test-client",
			Status:     "authorized",
			ExpiresAt:  now.Add(24 * time.Hour),
			LastUsedAt: nil, // Never used
			CreatedAt:  now.Add(-60 * 24 * time.Hour), // Created 60 days ago
		}
		if err := CreateDeviceCode(conns, neverUsedDevice); err != nil {
			t.Fatalf("Failed to create never-used device: %v", err)
		}

		// Run cleanup with 30-day threshold
		threshold := 30 * 24 * time.Hour
		if err := DeleteUnusedDeviceCodes(conns, threshold); err != nil {
			t.Fatalf("DeleteUnusedDeviceCodes failed: %v", err)
		}

		// Verify device was deleted (last_used_at IS NULL counts as unused)
		found, err := FindDeviceCodeByCode(conns, "never-used")
		if err != nil {
			t.Fatalf("Error checking never-used: %v", err)
		}
		if found != nil {
			t.Error("Expected never-used device to be deleted")
		}
	})

	t.Run("keeps pending devices even if old", func(t *testing.T) {
		// Create a pending device (user hasn't completed authorization yet)
		pendingDevice := &DeviceCode{
			DeviceCode: "pending-device",
			UserCode:   "PEN1",
			ClientID:   "test-client",
			Status:     "pending",
			ExpiresAt:  now.Add(1 * time.Hour),
			LastUsedAt: ptrTime(now.Add(-60 * 24 * time.Hour)), // Old but pending
		}
		if err := CreateDeviceCode(conns, pendingDevice); err != nil {
			t.Fatalf("Failed to create pending device: %v", err)
		}

		// Run cleanup
		threshold := 30 * 24 * time.Hour
		if err := DeleteUnusedDeviceCodes(conns, threshold); err != nil {
			t.Fatalf("DeleteUnusedDeviceCodes failed: %v", err)
		}

		// Verify pending device still exists (only deletes authorized/revoked)
		found, err := FindDeviceCodeByCode(conns, "pending-device")
		if err != nil {
			t.Fatalf("Error checking pending-device: %v", err)
		}
		if found == nil {
			t.Error("Expected pending-device to still exist")
		}
	})

	t.Run("deletes revoked devices older than threshold", func(t *testing.T) {
		// Create a revoked device (user revoked access)
		revokedDevice := &DeviceCode{
			DeviceCode: "revoked-device",
			UserCode:   "REV1",
			ClientID:   "test-client",
			Status:     "revoked",
			ExpiresAt:  now.Add(24 * time.Hour),
			LastUsedAt: ptrTime(now.Add(-60 * 24 * time.Hour)), // 60 days ago
		}
		if err := CreateDeviceCode(conns, revokedDevice); err != nil {
			t.Fatalf("Failed to create revoked device: %v", err)
		}

		// Run cleanup
		threshold := 30 * 24 * time.Hour
		if err := DeleteUnusedDeviceCodes(conns, threshold); err != nil {
			t.Fatalf("DeleteUnusedDeviceCodes failed: %v", err)
		}

		// Verify revoked device was deleted
		found, err := FindDeviceCodeByCode(conns, "revoked-device")
		if err != nil {
			t.Fatalf("Error checking revoked-device: %v", err)
		}
		if found != nil {
			t.Error("Expected revoked-device to be deleted")
		}
	})
}

func TestRevokeDeviceCode(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create an authorized device with tokens
	deviceCode := "test-device"
	osmToken := "osm-access-token"
	osmRefresh := "osm-refresh-token"
	device := &DeviceCode{
		DeviceCode:      deviceCode,
		UserCode:        "TEST",
		ClientID:        "test-client",
		Status:          "authorized",
		ExpiresAt:       now.Add(24 * time.Hour),
		OSMAccessToken:  &osmToken,
		OSMRefreshToken: &osmRefresh,
		OSMTokenExpiry:  ptrTime(now.Add(1 * time.Hour)),
	}
	if err := CreateDeviceCode(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Revoke the device
	if err := RevokeDeviceCode(conns, deviceCode); err != nil {
		t.Fatalf("RevokeDeviceCode failed: %v", err)
	}

	// Verify device status is revoked and tokens are cleared
	found, err := FindDeviceCodeByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found == nil {
		t.Fatal("Expected device to still exist")
	}
	if found.Status != "revoked" {
		t.Errorf("Expected status 'revoked', got '%s'", found.Status)
	}
	if found.OSMAccessToken != nil {
		t.Error("Expected OSMAccessToken to be cleared")
	}
	if found.OSMRefreshToken != nil {
		t.Error("Expected OSMRefreshToken to be cleared")
	}
	if found.OSMTokenExpiry != nil {
		t.Error("Expected OSMTokenExpiry to be cleared")
	}
}

func TestUpdateDeviceCodeLastUsed(t *testing.T) {
	conns := setupTestDB(t)
	now := time.Now()

	// Create a device without last_used_at
	deviceCode := "test-device"
	device := &DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   "TEST",
		ClientID:   "test-client",
		Status:     "authorized",
		ExpiresAt:  now.Add(24 * time.Hour),
		LastUsedAt: nil,
	}
	if err := CreateDeviceCode(conns, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Update last_used_at
	beforeUpdate := time.Now()
	if err := UpdateDeviceCodeLastUsed(conns, deviceCode); err != nil {
		t.Fatalf("UpdateDeviceCodeLastUsed failed: %v", err)
	}
	afterUpdate := time.Now()

	// Verify last_used_at was set
	found, err := FindDeviceCodeByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device: %v", err)
	}
	if found == nil {
		t.Fatal("Expected device to exist")
	}
	if found.LastUsedAt == nil {
		t.Fatal("Expected LastUsedAt to be set")
	}
	if found.LastUsedAt.Before(beforeUpdate) || found.LastUsedAt.After(afterUpdate) {
		t.Errorf("LastUsedAt should be between %v and %v, got %v",
			beforeUpdate, afterUpdate, *found.LastUsedAt)
	}

	// Update again and verify it changes
	time.Sleep(10 * time.Millisecond)
	firstUpdate := *found.LastUsedAt
	if err := UpdateDeviceCodeLastUsed(conns, deviceCode); err != nil {
		t.Fatalf("Second UpdateDeviceCodeLastUsed failed: %v", err)
	}

	found, err = FindDeviceCodeByCode(conns, deviceCode)
	if err != nil {
		t.Fatalf("Error finding device after second update: %v", err)
	}
	if found.LastUsedAt == nil {
		t.Fatal("Expected LastUsedAt to be set after second update")
	}
	if !found.LastUsedAt.After(firstUpdate) {
		t.Error("Expected LastUsedAt to be updated to a later time")
	}
}

// ptrTime is a helper to create time pointers
func ptrTime(t time.Time) *time.Time {
	return &t
}
