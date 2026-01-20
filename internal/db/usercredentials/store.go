package usercredentials

import (
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateOrUpdate creates or updates user credentials (upsert)
// Used on every login to keep credentials fresh
func CreateOrUpdate(conns *db.Connections, credential *db.UserCredential) error {
	return conns.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "osm_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"osm_user_name", "osm_email", "osm_access_token", "osm_refresh_token", "osm_token_expiry", "updated_at"}),
	}).Create(credential).Error
}

// Get retrieves user credentials by user ID
// Returns nil if not found
func Get(conns *db.Connections, osmUserID int) (*db.UserCredential, error) {
	var credential db.UserCredential
	err := conns.DB.Where("osm_user_id = ?", osmUserID).First(&credential).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &credential, nil
}

// UpdateTokens updates the OSM tokens after a token refresh
func UpdateTokens(conns *db.Connections, osmUserID int, accessToken string, refreshToken string, tokenExpiry time.Time) error {
	updates := map[string]any{
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
		"updated_at":        time.Now(),
	}
	return conns.DB.Model(&db.UserCredential{}).
		Where("osm_user_id = ?", osmUserID).
		Updates(updates).Error
}

// UpdateLastUsed updates the last_used_at timestamp
// Called after successfully using credentials for outbox processing
func UpdateLastUsed(conns *db.Connections, osmUserID int) error {
	return conns.DB.Model(&db.UserCredential{}).
		Where("osm_user_id = ?", osmUserID).
		Update("last_used_at", time.Now()).Error
}

// FindStaleCredentials finds credentials eligible for cleanup
// Returns credentials where:
// - No active web sessions exist for this user
// - No pending/processing outbox entries exist for this user
// - Last used more than retentionDays ago (or never used and created more than retentionDays ago)
func FindStaleCredentials(conns *db.Connections, retentionDays int) ([]db.UserCredential, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	var credentials []db.UserCredential

	// Find credentials with no active sessions and no pending writes
	// Last used (or created if never used) is older than retention period
	err := conns.DB.
		Where(`
			osm_user_id NOT IN (SELECT DISTINCT osm_user_id FROM web_sessions) AND
			osm_user_id NOT IN (SELECT DISTINCT osm_user_id FROM score_update_outbox WHERE status IN ('pending', 'processing')) AND
			COALESCE(last_used_at, created_at) < ?
		`, cutoffTime).
		Find(&credentials).Error

	return credentials, err
}

// Delete deletes user credentials by user ID
func Delete(conns *db.Connections, osmUserID int) error {
	return conns.DB.Where("osm_user_id = ?", osmUserID).Delete(&db.UserCredential{}).Error
}

// CountActive counts the total number of user credentials
func CountActive(conns *db.Connections) (int64, error) {
	var count int64
	err := conns.DB.Model(&db.UserCredential{}).Count(&count).Error
	return count, err
}
