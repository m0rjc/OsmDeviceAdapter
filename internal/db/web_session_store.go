package db

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// CreateWebSession creates a new web session record
func CreateWebSession(conns *Connections, session *WebSession) error {
	return conns.DB.Create(session).Error
}

// FindWebSessionByID finds a web session by its ID
// Returns nil if not found or expired
func FindWebSessionByID(conns *Connections, sessionID string) (*WebSession, error) {
	var record WebSession
	err := conns.DB.Where("id = ? AND expires_at > ?", sessionID, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateWebSessionActivity updates the last_activity timestamp for sliding expiration
func UpdateWebSessionActivity(conns *Connections, sessionID string) error {
	return conns.DB.Model(&WebSession{}).
		Where("id = ?", sessionID).
		Update("last_activity", time.Now()).Error
}

// UpdateWebSessionTokens updates the OSM tokens for a session
func UpdateWebSessionTokens(conns *Connections, sessionID string, accessToken string, refreshToken string, tokenExpiry time.Time) error {
	updates := map[string]interface{}{
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
	}
	return conns.DB.Model(&WebSession{}).
		Where("id = ?", sessionID).
		Updates(updates).Error
}

// UpdateWebSessionSection updates the selected section for a session
func UpdateWebSessionSection(conns *Connections, sessionID string, sectionID int) error {
	return conns.DB.Model(&WebSession{}).
		Where("id = ?", sessionID).
		Update("selected_section_id", sectionID).Error
}

// DeleteWebSession deletes a web session by ID
func DeleteWebSession(conns *Connections, sessionID string) error {
	return conns.DB.Where("id = ?", sessionID).Delete(&WebSession{}).Error
}

// DeleteExpiredWebSessions deletes all expired web sessions
func DeleteExpiredWebSessions(conns *Connections) error {
	return conns.DB.Where("expires_at < ?", time.Now()).Delete(&WebSession{}).Error
}

// DeleteWebSessionsByUserID deletes all sessions for a specific user (logout everywhere)
func DeleteWebSessionsByUserID(conns *Connections, osmUserID int) error {
	return conns.DB.Where("osm_user_id = ?", osmUserID).Delete(&WebSession{}).Error
}
