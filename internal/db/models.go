package db

import (
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
	"gorm.io/gorm"
)

// DeviceCode represents an OAuth device authorization flow instance.
// It tracks the lifecycle from initial authorization request through to
// a fully authorized device with OSM API access.
type DeviceCode struct {
	// DeviceCode is the unique identifier for this device authorization flow.
	// Generated server-side and provided to the device during authorization.
	DeviceCode string `gorm:"primaryKey;column:device_code;type:varchar(255)"`

	// UserCode is the human-readable code (e.g., "ABCD-EFGH") that the user
	// enters on the web interface to authorize this device.
	UserCode string `gorm:"uniqueIndex;column:user_code;type:varchar(255);not null"`

	// ClientID identifies the client application requesting authorization.
	// Must match one of the allowed client IDs in the configuration.
	ClientID string `gorm:"column:client_id;type:varchar(255);not null"`

	// ExpiresAt is when this device code expires and can no longer be used
	// to complete the authorization flow.
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index"`

	// CreatedAt is when this device code was initially created.
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

	// Status tracks the authorization state. Valid values:
	// - "pending": waiting for user to authorize
	// - "awaiting_section": authorized but user needs to select a section
	// - "authorized": fully authorized and ready for API access
	// - "denied": user explicitly denied authorization
	Status string `gorm:"column:status;type:varchar(50);default:'pending'"`

	// DeviceAccessToken is the token returned to and used by the device for API requests.
	// This is a server-generated token that provides security isolation from the OSM token.
	// Generated when the device is fully authorized (after section selection).
	DeviceAccessToken *string `gorm:"uniqueIndex;column:device_access_token;type:varchar(255)"`

	// OSMAccessToken is the OAuth access token from OSM API.
	// This token is kept server-side only and never exposed to the device.
	// Used internally to make OSM API calls on behalf of the authenticated user.
	OSMAccessToken *string `gorm:"column:osm_access_token;type:text"`

	// OSMRefreshToken is the OAuth refresh token from OSM API.
	// Used to obtain new OSM access tokens when the current one expires.
	// Kept server-side only and never exposed to the device.
	OSMRefreshToken *string `gorm:"column:osm_refresh_token;type:text"`

	// OSMTokenExpiry is when the OSM access token expires.
	// The server automatically refreshes tokens before they expire.
	OSMTokenExpiry *time.Time `gorm:"column:osm_token_expiry"`

	// SectionID is the scout section selected by the user during authorization.
	// Determines which section's data the device can access.
	SectionID *int `gorm:"column:section_id"`

	// OsmUserID is the OSM user ID of the authenticated user.
	// Used for rate limiting key and user context.
	OsmUserID *int `gorm:"column:osm_user_id;index:idx_device_codes_user_id"`

	// TermID is the active term ID for the section.
	// Fetched from the OSM OAuth resource endpoint and used for patrol score queries.
	TermID *int `gorm:"column:term_id"`

	// TermCheckedAt is when the term information was last fetched from OSM.
	// Used to determine when to refresh term data (24-hour expiry).
	TermCheckedAt *time.Time `gorm:"column:term_checked_at"`

	// TermEndDate is the end date of the current active term.
	// Extracted from OSM API response and used for cache invalidation.
	TermEndDate *time.Time `gorm:"column:term_end_date;index:idx_device_codes_term_end_date"`

	// DeviceRequestIP is the client IP at device code generation time.
	// Captured from CF-Connecting-IP for security auditing.
	DeviceRequestIP *string `gorm:"column:device_request_ip;type:varchar(255)"`

	// DeviceRequestCountry is the ISO country code at device code generation.
	// Captured from CF-IPCountry for geographic verification.
	DeviceRequestCountry *string `gorm:"column:device_request_country;type:varchar(10)"`

	// DeviceRequestTime is when the device initiated authorization.
	DeviceRequestTime *time.Time `gorm:"column:device_request_time"`

	// DeviceSessions are temporary web sessions used during the OAuth flow.
	// These are automatically deleted when the device code is deleted.
	DeviceSessions []DeviceSession `gorm:"foreignKey:DeviceCode;constraint:OnDelete:CASCADE"`
}

func (DeviceCode) TableName() string {
	return "device_codes"
}

// DeviceSession represents a temporary web session during the OAuth device flow.
// These sessions connect the web-based OAuth callback to the device authorization
// being processed, expiring after 15 minutes.
type DeviceSession struct {
	// SessionID is the unique identifier for this web session.
	// Also used as the OAuth state parameter to prevent CSRF attacks.
	SessionID string `gorm:"primaryKey;column:session_id;type:varchar(255)"`

	// DeviceCode links this session to the device authorization flow.
	DeviceCode string `gorm:"column:device_code;type:varchar(255)"`

	// CreatedAt is when this session was created.
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

	// ExpiresAt is when this session expires (typically 15 minutes after creation).
	ExpiresAt time.Time `gorm:"column:expires_at;not null"`
}

func (DeviceSession) TableName() string {
	return "device_sessions"
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&DeviceCode{}, &DeviceSession{})
}

// User returns the OSM user associated with this Device, or nil if this
// device does not have a user (not completed authentication)
func (c DeviceCode) User() types.User {
	if c.OSMAccessToken != nil {
		return types.NewUser(c.OsmUserID, *c.OSMAccessToken)
	}
	return nil
}
