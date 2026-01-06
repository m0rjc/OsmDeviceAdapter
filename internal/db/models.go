package db

import (
	"time"

	"gorm.io/gorm"
)

type DeviceCode struct {
	DeviceCode       string         `gorm:"primaryKey;column:device_code;type:varchar(255)"`
	UserCode         string         `gorm:"uniqueIndex;column:user_code;type:varchar(255);not null"`
	ClientID         string         `gorm:"column:client_id;type:varchar(255);not null"`
	ExpiresAt        time.Time      `gorm:"column:expires_at;not null;index"`
	CreatedAt        time.Time      `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`
	Status           string         `gorm:"column:status;type:varchar(50);default:'pending'"`
	OSMAccessToken   *string        `gorm:"column:osm_access_token;type:text"`
	OSMRefreshToken  *string        `gorm:"column:osm_refresh_token;type:text"`
	OSMTokenExpiry   *time.Time     `gorm:"column:osm_token_expiry"`
	DeviceSessions   []DeviceSession `gorm:"foreignKey:DeviceCode;constraint:OnDelete:CASCADE"`
}

func (DeviceCode) TableName() string {
	return "device_codes"
}

type DeviceSession struct {
	SessionID  string    `gorm:"primaryKey;column:session_id;type:varchar(255)"`
	DeviceCode string    `gorm:"column:device_code;type:varchar(255)"`
	CreatedAt  time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`
	ExpiresAt  time.Time `gorm:"column:expires_at;not null"`
}

func (DeviceSession) TableName() string {
	return "device_sessions"
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&DeviceCode{}, &DeviceSession{})
}
