package websession

import (
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// Create creates a new web session record
func Create(conns *db.Connections, session *db.WebSession) error {
	return conns.DB.Create(session).Error
}

// FindByID finds a web session by its ID
// Returns nil if not found or expired
func FindByID(conns *db.Connections, sessionID string) (*db.WebSession, error) {
	var record db.WebSession
	err := conns.DB.Where("id = ? AND expires_at > ?", sessionID, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateActivity updates the last_activity timestamp for sliding expiration
func UpdateActivity(conns *db.Connections, sessionID string) error {
	return conns.DB.Model(&db.WebSession{}).
		Where("id = ?", sessionID).
		Update("last_activity", time.Now()).Error
}

// UpdateTokens updates the OSM tokens for a session
func UpdateTokens(conns *db.Connections, sessionID string, accessToken string, refreshToken string, tokenExpiry time.Time) error {
	updates := map[string]interface{}{
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
	}
	return conns.DB.Model(&db.WebSession{}).
		Where("id = ?", sessionID).
		Updates(updates).Error
}

// UpdateSection updates the selected section for a session
func UpdateSection(conns *db.Connections, sessionID string, sectionID int) error {
	return conns.DB.Model(&db.WebSession{}).
		Where("id = ?", sessionID).
		Update("selected_section_id", sectionID).Error
}

// Delete deletes a web session by ID
func Delete(conns *db.Connections, sessionID string) error {
	return conns.DB.Where("id = ?", sessionID).Delete(&db.WebSession{}).Error
}

// DeleteExpired deletes all expired web sessions
func DeleteExpired(conns *db.Connections) error {
	return conns.DB.Where("expires_at < ?", time.Now()).Delete(&db.WebSession{}).Error
}

// DeleteByUserID deletes all sessions for a specific user (logout everywhere)
func DeleteByUserID(conns *db.Connections, osmUserID int) error {
	return conns.DB.Where("osm_user_id = ?", osmUserID).Delete(&db.WebSession{}).Error
}
