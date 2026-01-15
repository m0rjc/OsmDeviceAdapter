package db

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// CreateDeviceCode creates a new device code record
func CreateDeviceCode(conns *Connections, deviceCode *DeviceCode) error {
	return conns.DB.Create(deviceCode).Error
}

// FindDeviceCodeByCode finds a device code by its device_code field
func FindDeviceCodeByCode(conns *Connections, deviceCode string) (*DeviceCode, error) {
	var record DeviceCode
	err := conns.DB.Where("device_code = ?", deviceCode).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// FindDeviceCodeByUserCode finds a device code by its user_code field
// Returns nil if not found or expired
func FindDeviceCodeByUserCode(conns *Connections, userCode string) (*DeviceCode, error) {
	var record DeviceCode
	err := conns.DB.Where("user_code = ? AND expires_at > ?", userCode, time.Now()).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateDeviceCodeStatus updates the status field of a device code
func UpdateDeviceCodeStatus(conns *Connections, deviceCode string, status string) error {
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Update("status", status).Error
}

// UpdateDeviceCodeWithTokens updates a device code with OSM tokens, user ID, and expiry
func UpdateDeviceCodeWithTokens(conns *Connections, deviceCode string, status string, accessToken string, refreshToken string, tokenExpiry time.Time, userID int) error {
	updates := map[string]interface{}{
		"status":            status,
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
		"osm_user_id":       userID,
	}
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// UpdateDeviceCodeWithSection updates a device code with section ID, device access token, and status
func UpdateDeviceCodeWithSection(conns *Connections, deviceCode string, status string, sectionID int, deviceAccessToken string) error {
	updates := map[string]interface{}{
		"status":              status,
		"section_id":          sectionID,
		"device_access_token": deviceAccessToken,
	}
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// FindDeviceCodeByAccessToken finds a device code by its OSM access token
// Returns nil if not found or not authorized
// DEPRECATED: Use FindDeviceCodeByDeviceAccessToken instead for client authentication
func FindDeviceCodeByAccessToken(conns *Connections, accessToken string) (*DeviceCode, error) {
	var record DeviceCode
	err := conns.DB.Where("osm_access_token = ? AND status = ?", accessToken, "authorized").First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// FindDeviceCodeByDeviceAccessToken finds a device code by its device access token
// This is used for authenticating device API requests
// Returns nil if not found, not authorized, or revoked
func FindDeviceCodeByDeviceAccessToken(conns *Connections, deviceAccessToken string) (*DeviceCode, error) {
	var record DeviceCode
	err := conns.DB.Where("device_access_token = ? AND status = ?", deviceAccessToken, "authorized").First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// UpdateDeviceCodeTokensOnly updates just the OSM tokens and expiry (not status)
func UpdateDeviceCodeTokensOnly(conns *Connections, deviceCode string, accessToken string, refreshToken string, tokenExpiry time.Time) error {
	updates := map[string]interface{}{
		"osm_access_token":  accessToken,
		"osm_refresh_token": refreshToken,
		"osm_token_expiry":  tokenExpiry,
	}
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// DeleteExpiredDeviceCodes deletes expired device codes that were never fully authorized.
// Authorized and revoked devices are not deleted here - they are handled by DeleteUnusedDeviceCodes
// based on last_used_at timestamp instead.
func DeleteExpiredDeviceCodes(conns *Connections) error {
	return conns.DB.Where("expires_at < ? AND status NOT IN (?, ?)", time.Now(), "authorized", "revoked").Delete(&DeviceCode{}).Error
}

// UpdateDeviceCodeTermInfo updates a device code with term information
func UpdateDeviceCodeTermInfo(conns *Connections, deviceCode string, userID int, termID int, termCheckedAt time.Time, termEndDate time.Time) error {
	updates := map[string]interface{}{
		"osm_user_id":     userID,
		"term_id":         termID,
		"term_checked_at": termCheckedAt,
		"term_end_date":   termEndDate,
	}
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// UpdateDeviceCodeLastUsed updates the last_used_at timestamp for a device
func UpdateDeviceCodeLastUsed(conns *Connections, deviceCode string) error {
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Update("last_used_at", time.Now()).Error
}

// RevokeDeviceCode marks a device as revoked and clears its OSM tokens.
// This is called when OSM token refresh fails with a 401, indicating the user revoked access.
func RevokeDeviceCode(conns *Connections, deviceCode string) error {
	updates := map[string]interface{}{
		"status":            "revoked",
		"osm_access_token":  nil,
		"osm_refresh_token": nil,
		"osm_token_expiry":  nil,
	}
	return conns.DB.Model(&DeviceCode{}).
		Where("device_code = ?", deviceCode).
		Updates(updates).Error
}

// DeleteUnusedDeviceCodes deletes device codes that haven't been used within the threshold duration
// and are in authorized or revoked status (to avoid deleting pending authorization flows)
func DeleteUnusedDeviceCodes(conns *Connections, unusedThreshold time.Duration) error {
	cutoffTime := time.Now().Add(-unusedThreshold)
	return conns.DB.Where("status IN (?, ?) AND (last_used_at IS NULL OR last_used_at < ?)", "authorized", "revoked", cutoffTime).
		Delete(&DeviceCode{}).Error
}
