package allowedclient

import (
	"errors"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// IsAllowed checks if a client ID is in the database and enabled.
// Returns (allowed bool, allowedClientID int, error).
// If allowed is false, allowedClientID will be 0.
func IsAllowed(conns *db.Connections, clientID string) (bool, int, error) {
	var record db.AllowedClientID
	err := conns.DB.Where("client_id = ? AND enabled = ?", clientID, true).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, 0, nil
		}
		return false, 0, err
	}
	return true, record.ID, nil
}

// Create creates a new allowed client ID record
func Create(conns *db.Connections, clientID *db.AllowedClientID) error {
	return conns.DB.Create(clientID).Error
}

// Find finds an allowed client ID by its client_id field
func Find(conns *db.Connections, clientID string) (*db.AllowedClientID, error) {
	var record db.AllowedClientID
	err := conns.DB.Where("client_id = ?", clientID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateEnabled updates the enabled status of a client ID
func UpdateEnabled(conns *db.Connections, clientID string, enabled bool) error {
	return conns.DB.Model(&db.AllowedClientID{}).
		Where("client_id = ?", clientID).
		Update("enabled", enabled).Error
}

// List returns all allowed client IDs (enabled and disabled)
func List(conns *db.Connections) ([]db.AllowedClientID, error) {
	var records []db.AllowedClientID
	err := conns.DB.Order("created_at DESC").Find(&records).Error
	return records, err
}

// Delete deletes an allowed client ID record
func Delete(conns *db.Connections, clientID string) error {
	return conns.DB.Where("client_id = ?", clientID).Delete(&db.AllowedClientID{}).Error
}
