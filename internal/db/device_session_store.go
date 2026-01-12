package db

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// CreateDeviceSession creates a new device session record
func CreateDeviceSession(conns *Connections, session *DeviceSession) error {
	return conns.DB.Create(session).Error
}

// FindDeviceSessionByID finds a device session by its session_id field
// Returns nil if not found or expired
func FindDeviceSessionByID(conns *Connections, sessionID string) (*DeviceSession, error) {
	var record DeviceSession
	err := conns.DB.Where("session_id = ? AND expires_at > ?", sessionID, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// DeleteExpiredDeviceSessions deletes all expired device sessions
func DeleteExpiredDeviceSessions(conns *Connections) error {
	return conns.DB.Where("expires_at < ?", time.Now()).Delete(&DeviceSession{}).Error
}

// DeleteDeviceSession deletes a device session by session ID
func DeleteDeviceSession(conns *Connections, sessionID string) error {
	return conns.DB.Where("session_id = ?", sessionID).Delete(&DeviceSession{}).Error
}
