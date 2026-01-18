package devicesession

import (
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// Create creates a new device session record
func Create(conns *db.Connections, session *db.DeviceSession) error {
	return conns.DB.Create(session).Error
}

// FindByID finds a device session by its session_id field
// Returns nil if not found or expired
func FindByID(conns *db.Connections, sessionID string) (*db.DeviceSession, error) {
	var record db.DeviceSession
	err := conns.DB.Where("session_id = ? AND expires_at > ?", sessionID, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// DeleteExpired deletes all expired device sessions
func DeleteExpired(conns *db.Connections) error {
	return conns.DB.Where("expires_at < ?", time.Now()).Delete(&db.DeviceSession{}).Error
}

// Delete deletes a device session by session ID
func Delete(conns *db.Connections, sessionID string) error {
	return conns.DB.Where("session_id = ?", sessionID).Delete(&db.DeviceSession{}).Error
}
