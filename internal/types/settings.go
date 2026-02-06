package types

// DeviceSettings contains configurable settings delivered to devices.
// These settings are configured by users via the admin UI and included
// in the /api/v1/patrols response for devices to consume.
type DeviceSettings struct {
	// PatrolColors maps patrol IDs to hex color codes (e.g., "#FF0000")
	// Colors represent the hue/theme - device firmware controls actual brightness
	PatrolColors map[string]string `json:"patrolColors,omitempty"`
}

// PatrolInfo contains basic patrol information for settings UI.
// This provides a canonical list of patrols that exist in OSM,
// allowing the UI to display patrols even if no color is set.
type PatrolInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
