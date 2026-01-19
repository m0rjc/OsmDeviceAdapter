package types

import (
	"context"
	"time"
)

// ContextKey is a type for context keys to avoid collisions
type ContextKey string

// Context keys used across the application
const (
	UserContextKey         ContextKey = "user"
	TokenRefreshFuncKey    ContextKey = "token_refresh_func"
)

// TokenRefreshFunc is a function that refreshes the current user's token.
// It's bound with the appropriate callbacks at authentication time and stored in context.
// Returns the new access token on success, or an error if refresh fails.
type TokenRefreshFunc func(ctx context.Context) (newAccessToken string, err error)

// User represents a user for the OSM API client.
type User interface {
	// UserID The OSM User ID if known
	UserID() *int
	// AccessToken the user's Access Token
	AccessToken() string
}

// TokenHolder represents any entity that holds OSM OAuth tokens.
// Both DeviceCode and WebSession implement this interface, allowing
// shared token refresh logic.
type TokenHolder interface {
	// GetOSMAccessToken returns the current OSM access token
	GetOSMAccessToken() string
	// GetOSMRefreshToken returns the current OSM refresh token
	GetOSMRefreshToken() string
	// GetOSMTokenExpiry returns when the access token expires
	GetOSMTokenExpiry() time.Time
	// GetIdentifier returns a unique identifier for this token holder (for logging/updates)
	GetIdentifier() string
}

type userImpl struct {
	userId      *int
	accessToken string
}

func (u *userImpl) UserID() *int {
	return u.userId
}

func (u *userImpl) AccessToken() string {
	return u.accessToken
}

func NewUser(userId *int, accessToken string) User {
	return &userImpl{userId, accessToken}
}

type PatrolScore struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}

type OSMTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type PatrolScoresResponse struct {
	Patrols   []PatrolScore `json:"patrols"`
	CachedAt  time.Time     `json:"cached_at"`
	ExpiresAt time.Time     `json:"expires_at"`
}

// OSM Profile Response Types
type OSMTerm struct {
	Name      string `json:"name"`
	StartDate string `json:"startdate"`
	EndDate   string `json:"enddate"`
	TermID    int    `json:"term_id"`
}

type OSMSection struct {
	SectionName string    `json:"section_name"`
	GroupName   string    `json:"group_name"`
	SectionID   int       `json:"section_id"`
	GroupID     int       `json:"group_id"`
	SectionType string    `json:"section_type"`
	Terms       []OSMTerm `json:"terms"`
}

type OSMProfileData struct {
	UserID           int          `json:"user_id"`
	FullName         string       `json:"full_name"`
	Email            string       `json:"email"`
	Sections         []OSMSection `json:"sections"`
	HasParentAccess  bool         `json:"has_parent_access"`
	HasSectionAccess bool         `json:"has_section_access"`
}

type OSMProfileResponse struct {
	Status bool            `json:"status"`
	Error  *string         `json:"error"`
	Data   *OSMProfileData `json:"data"`
}
