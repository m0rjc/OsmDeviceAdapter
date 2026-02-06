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
	// This is stored for reference but validation happens via CreatedByID.
	ClientID string `gorm:"column:client_id;type:varchar(255);not null"`

	// CreatedByID references the allowed_client_ids.id that created this device code.
	// Using surrogate key allows client ID rotation without breaking this reference.
	CreatedByID *int `gorm:"column:created_by_id;index:idx_device_codes_created_by"`

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
	// - "revoked": OSM access was revoked by user (token refresh failed with 401)
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

	// LastUsedAt is when the device last made an API request.
	// Used to identify and clean up unused devices after a configurable period.
	LastUsedAt *time.Time `gorm:"column:last_used_at;index:idx_device_codes_last_used"`

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

// AllowedClientID represents a client application that is allowed to use the device flow.
// Client IDs can be enabled/disabled, rotated, and include contact information for management.
// Uses a surrogate primary key to allow client ID rotation without breaking foreign keys.
type AllowedClientID struct {
	// ID is the surrogate primary key for this record.
	// Used in foreign key relationships to allow client ID rotation.
	ID int `gorm:"primaryKey;autoIncrement;column:id"`

	// ClientID is the current client identifier checked during authorization.
	// Can be rotated by updating this field (foreign keys remain stable via ID).
	ClientID string `gorm:"uniqueIndex;column:client_id;type:varchar(255);not null"`

	// Comment is a description of the client application or deployment.
	// e.g., "Production Scoreboard v1.0" or "Test deployment"
	Comment string `gorm:"column:comment;type:text;not null"`

	// ContactEmail is the email address for the client owner or maintainer.
	// Used for communication about service changes or issues.
	ContactEmail string `gorm:"column:contact_email;type:varchar(255);not null"`

	// Enabled indicates whether this client ID is currently allowed to authorize devices.
	// Set to false to temporarily disable a client without deleting the record.
	Enabled bool `gorm:"column:enabled;not null;default:true;index:idx_allowed_client_ids_enabled"`

	// CreatedAt is when this client ID was added to the system.
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

	// UpdatedAt is when this record was last modified.
	UpdatedAt time.Time `gorm:"column:updated_at;default:CURRENT_TIMESTAMP"`
}

func (AllowedClientID) TableName() string {
	return "allowed_client_ids"
}

// WebSession represents an authenticated admin web session.
// Created when a user completes the OAuth web flow for the admin UI.
// Session data is stored server-side; only the session ID is in the cookie.
type WebSession struct {
	// ID is a UUID identifying this session (stored in cookie)
	ID string `gorm:"primaryKey;column:id;type:varchar(36)"`

	// OSMUserID is the OSM user ID of the authenticated user
	OSMUserID int `gorm:"column:osm_user_id;not null;index:idx_web_sessions_user"`

	// OSMAccessToken is the OAuth access token from OSM API
	OSMAccessToken string `gorm:"column:osm_access_token;type:text;not null"`

	// OSMRefreshToken is the OAuth refresh token from OSM API
	OSMRefreshToken string `gorm:"column:osm_refresh_token;type:text;not null"`

	// OSMTokenExpiry is when the OSM access token expires
	OSMTokenExpiry time.Time `gorm:"column:osm_token_expiry;not null"`

	// CSRFToken is used to protect against CSRF attacks
	CSRFToken string `gorm:"column:csrf_token;type:varchar(64);not null"`

	// SelectedSectionID is the currently selected section (nullable)
	SelectedSectionID *int `gorm:"column:selected_section_id"`

	// CreatedAt is when this session was created
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

	// LastActivity is updated on each request for sliding expiration
	LastActivity time.Time `gorm:"column:last_activity;default:CURRENT_TIMESTAMP"`

	// ExpiresAt is the absolute session expiry time
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index:idx_web_sessions_expiry"`
}

func (WebSession) TableName() string {
	return "web_sessions"
}

// ScoreAuditLog records score changes made via the admin UI.
// Used for accountability and debugging score discrepancies.
type ScoreAuditLog struct {
	// ID is an auto-incrementing primary key
	ID int64 `gorm:"primaryKey;autoIncrement;column:id"`

	// OSMUserID is the user who made the change
	OSMUserID int `gorm:"column:osm_user_id;not null;index:idx_score_audit_user"`

	// SectionID is the section containing the patrol
	SectionID int `gorm:"column:section_id;not null;index:idx_score_audit_section"`

	// PatrolID is the patrol whose score was changed
	PatrolID string `gorm:"column:patrol_id;type:varchar(255);not null"`

	// PatrolName is the patrol name at the time of the change
	PatrolName string `gorm:"column:patrol_name;type:varchar(255);not null"`

	// PreviousScore is the score before the change
	PreviousScore int `gorm:"column:previous_score;not null"`

	// NewScore is the score after the change
	NewScore int `gorm:"column:new_score;not null"`

	// PointsAdded is the delta (can be negative)
	PointsAdded int `gorm:"column:points_added;not null"`

	// CreatedAt is when the change was made
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP;index:idx_score_audit_created"`
}

func (ScoreAuditLog) TableName() string {
	return "score_audit_log"
}

// SectionSettings stores user-configurable settings for a section.
// Settings are scoped per OSM user + section combination.
type SectionSettings struct {
	// OSMUserID is the OSM user who owns these settings
	OSMUserID int `gorm:"primaryKey;column:osm_user_id;not null"`

	// SectionID is the section these settings apply to
	SectionID int `gorm:"primaryKey;column:section_id;not null"`

	// Settings is a JSONB column storing all settings as a flexible JSON object.
	// Current schema:
	// {
	//   "patrolColors": {
	//     "<patrolId>": "#RRGGBB",
	//     ...
	//   }
	// }
	Settings []byte `gorm:"column:settings;type:jsonb;not null;default:'{}'"`

	// CreatedAt is when this record was created
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP"`

	// UpdatedAt is when this record was last modified
	UpdatedAt time.Time `gorm:"column:updated_at;default:CURRENT_TIMESTAMP"`
}

func (SectionSettings) TableName() string {
	return "section_settings"
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&DeviceCode{}, &DeviceSession{}, &AllowedClientID{}, &WebSession{}, &ScoreAuditLog{}, &SectionSettings{})
}

// User returns the OSM user associated with this Device, or nil if this
// device does not have a user (not completed authentication)
func (c DeviceCode) User() types.User {
	if c.OSMAccessToken != nil {
		return types.NewUser(c.OsmUserID, *c.OSMAccessToken)
	}
	return nil
}

// TokenHolder interface implementation for DeviceCode

// GetOSMAccessToken returns the current OSM access token
func (c *DeviceCode) GetOSMAccessToken() string {
	if c.OSMAccessToken == nil {
		return ""
	}
	return *c.OSMAccessToken
}

// GetOSMRefreshToken returns the current OSM refresh token
func (c *DeviceCode) GetOSMRefreshToken() string {
	if c.OSMRefreshToken == nil {
		return ""
	}
	return *c.OSMRefreshToken
}

// GetOSMTokenExpiry returns when the access token expires
func (c *DeviceCode) GetOSMTokenExpiry() time.Time {
	if c.OSMTokenExpiry == nil {
		return time.Time{}
	}
	return *c.OSMTokenExpiry
}

// GetIdentifier returns the device code as a unique identifier
func (c *DeviceCode) GetIdentifier() string {
	return c.DeviceCode
}

// TokenHolder interface implementation for WebSession

// GetOSMAccessToken returns the current OSM access token
func (s *WebSession) GetOSMAccessToken() string {
	return s.OSMAccessToken
}

// GetOSMRefreshToken returns the current OSM refresh token
func (s *WebSession) GetOSMRefreshToken() string {
	return s.OSMRefreshToken
}

// GetOSMTokenExpiry returns when the access token expires
func (s *WebSession) GetOSMTokenExpiry() time.Time {
	return s.OSMTokenExpiry
}

// GetIdentifier returns the session ID as a unique identifier
func (s *WebSession) GetIdentifier() string {
	return s.ID
}

// User returns the OSM user associated with this web session
func (s *WebSession) User() types.User {
	return types.NewUser(&s.OSMUserID, s.OSMAccessToken)
}
