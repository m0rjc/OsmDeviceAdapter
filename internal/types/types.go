package types

import "time"

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
	UserID            int          `json:"user_id"`
	FullName          string       `json:"full_name"`
	Email             string       `json:"email"`
	Sections          []OSMSection `json:"sections"`
	HasParentAccess   bool         `json:"has_parent_access"`
	HasSectionAccess  bool         `json:"has_section_access"`
}

type OSMProfileResponse struct {
	Status bool            `json:"status"`
	Error  *string         `json:"error"`
	Data   *OSMProfileData `json:"data"`
}
