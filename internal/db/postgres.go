package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

func NewPostgresConnection(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}

func RunMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS device_codes (
			device_code VARCHAR(255) PRIMARY KEY,
			user_code VARCHAR(255) UNIQUE NOT NULL,
			client_id VARCHAR(255) NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			status VARCHAR(50) DEFAULT 'pending',
			osm_access_token TEXT,
			osm_refresh_token TEXT,
			osm_token_expiry TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_device_codes_user_code ON device_codes(user_code)`,
		`CREATE INDEX IF NOT EXISTS idx_device_codes_expires_at ON device_codes(expires_at)`,
		`CREATE TABLE IF NOT EXISTS device_sessions (
			session_id VARCHAR(255) PRIMARY KEY,
			device_code VARCHAR(255) REFERENCES device_codes(device_code) ON DELETE CASCADE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		)`,
	}

	for _, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
