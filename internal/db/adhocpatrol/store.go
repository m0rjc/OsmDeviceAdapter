package adhocpatrol

import (
	"errors"
	"fmt"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// MaxPatrolsPerUser is the maximum number of ad-hoc patrols a user can create.
const MaxPatrolsPerUser = 20

// ErrMaxPatrolsReached is returned when a user tries to create more than MaxPatrolsPerUser patrols.
var ErrMaxPatrolsReached = fmt.Errorf("maximum of %d ad-hoc patrols reached", MaxPatrolsPerUser)

// ErrNotFound is returned when the requested patrol does not exist or does not belong to the user.
var ErrNotFound = errors.New("ad-hoc patrol not found")

// ListByUser returns all ad-hoc patrols for a user, ordered by position.
func ListByUser(conns *db.Connections, osmUserID int) ([]db.AdhocPatrol, error) {
	var patrols []db.AdhocPatrol
	err := conns.DB.Where("osm_user_id = ?", osmUserID).Order("position ASC").Find(&patrols).Error
	return patrols, err
}

// Create creates a new ad-hoc patrol, assigning the next position.
// Returns ErrMaxPatrolsReached if the user already has MaxPatrolsPerUser patrols.
func Create(conns *db.Connections, patrol *db.AdhocPatrol) error {
	// Count existing patrols for this user
	var count int64
	if err := conns.DB.Model(&db.AdhocPatrol{}).Where("osm_user_id = ?", patrol.OSMUserID).Count(&count).Error; err != nil {
		return err
	}
	if count >= MaxPatrolsPerUser {
		return ErrMaxPatrolsReached
	}

	// Assign next position
	var maxPos *int
	conns.DB.Model(&db.AdhocPatrol{}).Where("osm_user_id = ?", patrol.OSMUserID).Select("MAX(position)").Scan(&maxPos)
	if maxPos != nil {
		patrol.Position = *maxPos + 1
	} else {
		patrol.Position = 0
	}

	return conns.DB.Create(patrol).Error
}

// Update updates the name and color of an ad-hoc patrol, with ownership check.
// Returns ErrNotFound if the patrol does not exist or does not belong to the user.
func Update(conns *db.Connections, id int64, osmUserID int, name string, color string) error {
	result := conns.DB.Model(&db.AdhocPatrol{}).
		Where("id = ? AND osm_user_id = ?", id, osmUserID).
		Updates(map[string]interface{}{
			"name":  name,
			"color": color,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete deletes an ad-hoc patrol, with ownership check.
// Returns ErrNotFound if the patrol does not exist or does not belong to the user.
func Delete(conns *db.Connections, id int64, osmUserID int) error {
	result := conns.DB.Where("id = ? AND osm_user_id = ?", id, osmUserID).Delete(&db.AdhocPatrol{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateScore updates the score for a single ad-hoc patrol, with ownership check.
// Returns ErrNotFound if the patrol does not exist or does not belong to the user.
func UpdateScore(conns *db.Connections, id int64, osmUserID int, newScore int) error {
	result := conns.DB.Model(&db.AdhocPatrol{}).
		Where("id = ? AND osm_user_id = ?", id, osmUserID).
		Update("score", newScore)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetAllScores resets all ad-hoc patrol scores to 0 for a user.
func ResetAllScores(conns *db.Connections, osmUserID int) error {
	return conns.DB.Model(&db.AdhocPatrol{}).
		Where("osm_user_id = ?", osmUserID).
		Update("score", 0).Error
}

// FindByIDAndUser finds a single ad-hoc patrol by ID with ownership check.
// Returns ErrNotFound if the patrol does not exist or does not belong to the user.
func FindByIDAndUser(conns *db.Connections, id int64, osmUserID int) (*db.AdhocPatrol, error) {
	var patrol db.AdhocPatrol
	err := conns.DB.Where("id = ? AND osm_user_id = ?", id, osmUserID).First(&patrol).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &patrol, nil
}
