package db

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// SetupTestDB creates an in-memory SQLite database for testing.
// This is exported for use by subpackage tests.
func SetupTestDB(t *testing.T) *Connections {
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
