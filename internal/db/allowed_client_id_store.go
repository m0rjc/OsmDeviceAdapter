package db

import (
	"errors"

	"gorm.io/gorm"
)

// IsClientIDAllowed checks if a client ID is in the database and enabled.
// Returns (allowed bool, allowedClientID int, error).
// If allowed is false, allowedClientID will be 0.
func IsClientIDAllowed(conns *Connections, clientID string) (bool, int, error) {
	var record AllowedClientID
	err := conns.DB.Where("client_id = ? AND enabled = ?", clientID, true).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, 0, nil
		}
		return false, 0, err
	}
	return true, record.ID, nil
}

// CreateAllowedClientID creates a new allowed client ID record
func CreateAllowedClientID(conns *Connections, clientID *AllowedClientID) error {
	return conns.DB.Create(clientID).Error
}

// FindAllowedClientID finds an allowed client ID by its client_id field
func FindAllowedClientID(conns *Connections, clientID string) (*AllowedClientID, error) {
	var record AllowedClientID
	err := conns.DB.Where("client_id = ?", clientID).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateAllowedClientIDEnabled updates the enabled status of a client ID
func UpdateAllowedClientIDEnabled(conns *Connections, clientID string, enabled bool) error {
	return conns.DB.Model(&AllowedClientID{}).
		Where("client_id = ?", clientID).
		Update("enabled", enabled).Error
}

// ListAllowedClientIDs returns all allowed client IDs (enabled and disabled)
func ListAllowedClientIDs(conns *Connections) ([]AllowedClientID, error) {
	var records []AllowedClientID
	err := conns.DB.Order("created_at DESC").Find(&records).Error
	return records, err
}

// DeleteAllowedClientID deletes an allowed client ID record
func DeleteAllowedClientID(conns *Connections, clientID string) error {
	return conns.DB.Where("client_id = ?", clientID).Delete(&AllowedClientID{}).Error
}
