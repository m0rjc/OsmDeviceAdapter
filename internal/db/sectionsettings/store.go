package sectionsettings

import (
	"encoding/json"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm/clause"
)

// SettingsJSON represents the JSON structure stored in the settings column
type SettingsJSON struct {
	PatrolColors map[string]string `json:"patrolColors,omitempty"`
}

// Get retrieves section settings for a user+section combination.
// Returns nil, nil if no settings exist.
func Get(conns *db.Connections, osmUserID, sectionID int) (*db.SectionSettings, error) {
	var settings db.SectionSettings
	result := conns.DB.Where("osm_user_id = ? AND section_id = ?", osmUserID, sectionID).First(&settings)
	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			return nil, nil
		}
		return nil, result.Error
	}
	return &settings, nil
}

// GetParsed retrieves and parses the settings JSON for a user+section combination.
// Returns empty SettingsJSON if no settings exist.
func GetParsed(conns *db.Connections, osmUserID, sectionID int) (*SettingsJSON, error) {
	settings, err := Get(conns, osmUserID, sectionID)
	if err != nil {
		return nil, err
	}

	parsed := &SettingsJSON{
		PatrolColors: make(map[string]string),
	}

	if settings == nil || len(settings.Settings) == 0 {
		return parsed, nil
	}

	if err := json.Unmarshal(settings.Settings, parsed); err != nil {
		return nil, err
	}

	// Ensure map is initialized even if JSON had null
	if parsed.PatrolColors == nil {
		parsed.PatrolColors = make(map[string]string)
	}

	return parsed, nil
}

// Upsert creates or updates section settings for a user+section combination.
// Uses PostgreSQL ON CONFLICT for atomic upsert.
func Upsert(conns *db.Connections, settings *db.SectionSettings) error {
	return conns.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "osm_user_id"}, {Name: "section_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"settings", "updated_at"}),
	}).Create(settings).Error
}

// UpsertPatrolColors updates only the patrol colors portion of settings.
// Creates the record if it doesn't exist.
func UpsertPatrolColors(conns *db.Connections, osmUserID, sectionID int, patrolColors map[string]string) error {
	// Get existing settings to preserve other fields
	existing, err := GetParsed(conns, osmUserID, sectionID)
	if err != nil {
		return err
	}

	// Update patrol colors
	existing.PatrolColors = patrolColors

	// Serialize back to JSON
	settingsBytes, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	// Upsert the record
	return Upsert(conns, &db.SectionSettings{
		OSMUserID: osmUserID,
		SectionID: sectionID,
		Settings:  settingsBytes,
	})
}

// Delete removes section settings for a user+section combination.
func Delete(conns *db.Connections, osmUserID, sectionID int) error {
	return conns.DB.Where("osm_user_id = ? AND section_id = ?", osmUserID, sectionID).Delete(&db.SectionSettings{}).Error
}
