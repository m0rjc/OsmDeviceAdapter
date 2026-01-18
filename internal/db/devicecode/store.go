package devicecode

import (
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// Create creates a new device code record
func Create(conns *db.Connections, deviceCode *db.DeviceCode) error {
	return conns.DB.Create(deviceCode).Error
}

// FindByCode finds a device code by its device_code field
func FindByCode(conns *db.Connections, deviceCode string) (*db.DeviceCode, error) {
	var record db.DeviceCode
	err := conns.DB.Where("device_code = ?", deviceCode).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// FindByUserCode finds a device code by its user_code field
// Returns nil if not found or expired
func FindByUserCode(conns *db.Connections, userCode string) (*db.DeviceCode, error) {
	var record db.DeviceCode
	err := conns.DB.Where("user_code = ? AND expires_at > ?", userCode, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateStatus updates the status field of a device code
func UpdateStatus(conns *db.Connections, deviceCode string, status string) error {
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Update("status", status).Error
}

// UpdateWithTokens updates a device code with OSM tokens, user ID, and expiry
func UpdateWithTokens(conns *db.Connections, deviceCode string, status string, accessToken string, refreshToken string, tokenExpiry time.Time, userID int) error {
	updates := map[string]interface{}{
		"status":            status,
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
		"osm_user_id":       userID,
	}
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// UpdateWithSection updates a device code with section ID, device access token, and status
func UpdateWithSection(conns *db.Connections, deviceCode string, status string, sectionID int, deviceAccessToken string) error {
	updates := map[string]interface{}{
		"status":              status,
		"section_id":          sectionID,
		"device_access_token": deviceAccessToken,
	}
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// FindByAccessToken finds a device code by its OSM access token
// Returns nil if not found or not authorized
// DEPRECATED: Use FindByDeviceAccessToken instead for client authentication
func FindByAccessToken(conns *db.Connections, accessToken string) (*db.DeviceCode, error) {
	var record db.DeviceCode
	err := conns.DB.Where("osm_access_token = ? AND status = ?", accessToken, "authorized").First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// FindByDeviceAccessToken finds a device code by its device access token
// This is used for authenticating device API requests
// Returns nil if not found, not authorized, or revoked
func FindByDeviceAccessToken(conns *db.Connections, deviceAccessToken string) (*db.DeviceCode, error) {
	var record db.DeviceCode
	err := conns.DB.Where("device_access_token = ? AND status = ?", deviceAccessToken, "authorized").First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateTokensOnly updates just the OSM tokens and expiry (not status)
func UpdateTokensOnly(conns *db.Connections, deviceCode string, accessToken string, refreshToken string, tokenExpiry time.Time) error {
	updates := map[string]interface{}{
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
	}
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// DeleteExpired deletes expired device codes that were never fully authorized.
// Authorized and revoked devices are not deleted here - they are handled by DeleteUnused
// based on last_used_at timestamp instead.
func DeleteExpired(conns *db.Connections) error {
	return conns.DB.Where("expires_at < ? AND status NOT IN (?, ?)", time.Now(), "authorized", "revoked").Delete(&db.DeviceCode{}).Error
}

// UpdateTermInfo updates a device code with term information
func UpdateTermInfo(conns *db.Connections, deviceCode string, userID int, termID int, termCheckedAt time.Time, termEndDate time.Time) error {
	updates := map[string]interface{}{
		"osm_user_id":     userID,
		"term_id":         termID,
		"term_checked_at": termCheckedAt,
		"term_end_date":   termEndDate,
	}
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// UpdateLastUsed updates the last_used_at timestamp for a device
func UpdateLastUsed(conns *db.Connections, deviceCode string) error {
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Update("last_used_at", time.Now()).Error
}

// Revoke marks a device as revoked and clears its OSM tokens.
// This is called when OSM token refresh fails with a 401, indicating the user revoked access.
func Revoke(conns *db.Connections, deviceCode string) error {
	updates := map[string]interface{}{
		"status":            "revoked",
		"osm_access_token":  nil,
		"osm_refresh_token": nil,
		"osm_token_expiry":  nil,
	}
	return conns.DB.Model(&db.DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// DeleteUnused deletes device codes that haven't been used within the threshold duration
// and are in authorized or revoked status (to avoid deleting pending authorization flows)
func DeleteUnused(conns *db.Connections, unusedThreshold time.Duration) error {
	cutoffTime := time.Now().Add(-unusedThreshold)
	return conns.DB.Where("status IN (?, ?) AND (last_used_at IS NULL OR last_used_at < ?)", "authorized", "revoked", cutoffTime).
		Delete(&db.DeviceCode{}).Error
}
