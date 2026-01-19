package scoreoutbox

import (
	"errors"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

// Create inserts a single outbox entry
func Create(conns *db.Connections, entry *db.ScoreUpdateOutbox) error {
	return conns.DB.Create(entry).Error
}

// CreateBatch inserts multiple outbox entries in a single transaction
func CreateBatch(conns *db.Connections, entries []db.ScoreUpdateOutbox) error {
	if len(entries) == 0 {
		return nil
	}
	return conns.DB.Create(&entries).Error
}

// FindByIdempotencyKey finds an outbox entry by its base idempotency key
// Searches for any entry whose idempotency_key starts with the provided key
// (handles both exact matches and prefix matches for composite keys like "key:patrol:index")
// Returns nil if not found
func FindByIdempotencyKey(conns *db.Connections, idempotencyKey string) (*db.ScoreUpdateOutbox, error) {
	var entry db.ScoreUpdateOutbox
	// Try exact match first (for backwards compatibility)
	err := conns.DB.Where("idempotency_key = ?", idempotencyKey).First(&entry).Error
	if err == nil {
		return &entry, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Try prefix match (for composite keys like "basekey:patrol:index")
	err = conns.DB.Where("idempotency_key LIKE ?", idempotencyKey+":%").First(&entry).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entry, nil
}

// UserPatrolKey represents a unique combination of user + section + patrol
// Used by the background worker to iterate over patrols needing sync
type UserPatrolKey struct {
	OSMUserID int
	SectionID int
	PatrolID  string
}

// ClaimPendingForUserPatrol claims all pending entries for a specific user+section+patrol
// Uses SELECT FOR UPDATE SKIP LOCKED to safely handle concurrent access
// Marks claimed entries as 'processing' and increments attempt_count
// Returns the claimed entries
func ClaimPendingForUserPatrol(conns *db.Connections, osmUserID int, sectionID int, patrolID string) ([]db.ScoreUpdateOutbox, error) {
	var entries []db.ScoreUpdateOutbox

	// Start a transaction
	err := conns.DB.Transaction(func(tx *gorm.DB) error {
		// Select pending entries with FOR UPDATE SKIP LOCKED
		if err := tx.Clauses(db.ForUpdateSkipLocked()).
			Where("osm_user_id = ? AND section_id = ? AND patrol_id = ? AND status = ?",
				osmUserID, sectionID, patrolID, "pending").
			Find(&entries).Error; err != nil {
			return err
		}

		// If no entries found, nothing to claim
		if len(entries) == 0 {
			return nil
		}

		// Extract IDs for update
		ids := make([]uint, len(entries))
		for i, entry := range entries {
			ids[i] = entry.ID
		}

		// Update status to 'processing' and increment attempt count
		if err := tx.Model(&db.ScoreUpdateOutbox{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":        "processing",
				"attempt_count": gorm.Expr("attempt_count + 1"),
			}).Error; err != nil {
			return err
		}

		// Re-fetch the updated entries to get the new values
		return tx.Where("id IN ?", ids).Find(&entries).Error
	})

	return entries, err
}

// MarkCompleted marks outbox entries as completed
func MarkCompleted(conns *db.Connections, ids []uint, processedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	updates := map[string]any{
		"status":       "completed",
		"processed_at": processedAt,
	}
	return conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Where("id IN ?", ids).
		Updates(updates).Error
}

// MarkFailed marks outbox entries as failed with error message and next retry time
func MarkFailed(conns *db.Connections, ids []uint, errorMsg string, nextRetryAt *time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	updates := map[string]any{
		"status":        "failed",
		"last_error":    errorMsg,
		"next_retry_at": nextRetryAt,
	}
	return conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Where("id IN ?", ids).
		Updates(updates).Error
}

// MarkAuthRevoked marks all pending/processing entries for a user as auth_revoked
// Called when OSM returns 401 (credentials were revoked by user)
func MarkAuthRevoked(conns *db.Connections, osmUserID int) error {
	return conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Where("osm_user_id = ? AND status IN ?", osmUserID, []string{"pending", "processing", "failed"}).
		Update("status", "auth_revoked").Error
}

// RecoverAuthRevoked resets auth_revoked entries back to pending status
// Called when user re-authenticates after previous auth revocation
func RecoverAuthRevoked(conns *db.Connections, osmUserID int) error {
	return conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Where("osm_user_id = ? AND status = ?", osmUserID, "auth_revoked").
		Update("status", "pending").Error
}

// CountPendingByUser counts pending and processing entries for a specific user
// Used by the session endpoint to show pending writes count
func CountPendingByUser(conns *db.Connections, osmUserID int) (int64, error) {
	var count int64
	err := conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Where("osm_user_id = ? AND status IN ?", osmUserID, []string{"pending", "processing"}).
		Count(&count).Error
	return count, err
}

// GetPendingDeltasBySection aggregates pending deltas by patrol for a section
// Returns a map of patrolID -> total pending delta
// Used to show pending changes on scoreboard devices
func GetPendingDeltasBySection(conns *db.Connections, sectionID int) (map[string]int, error) {
	type Result struct {
		PatrolID    string
		TotalDelta  int
	}

	var results []Result
	err := conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Select("patrol_id, SUM(points_delta) as total_delta").
		Where("section_id = ? AND status IN ?", sectionID, []string{"pending", "processing"}).
		Group("patrol_id").
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	// Convert to map
	deltas := make(map[string]int)
	for _, result := range results {
		deltas[result.PatrolID] = result.TotalDelta
	}

	return deltas, nil
}

// FindUserPatrolsWithPending finds all user+section+patrol combinations that have pending entries
// Returns list of keys for the background worker to iterate over
// Only returns entries that are ready to retry (pending status or failed with retry time passed)
func FindUserPatrolsWithPending(conns *db.Connections) ([]UserPatrolKey, error) {
	type Result struct {
		OSMUserID int
		SectionID int
		PatrolID  string
	}

	var results []Result
	now := time.Now()

	err := conns.DB.Model(&db.ScoreUpdateOutbox{}).
		Select("DISTINCT osm_user_id, section_id, patrol_id").
		Where("status = ? OR (status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?)",
			"pending", "failed", now).
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	// Convert to keys
	keys := make([]UserPatrolKey, len(results))
	for i, result := range results {
		keys[i] = UserPatrolKey{
			OSMUserID: result.OSMUserID,
			SectionID: result.SectionID,
			PatrolID:  result.PatrolID,
		}
	}

	return keys, nil
}

// DeleteExpired deletes old outbox entries based on their status and retention periods
// - completed: older than completedRetentionHours (default 24 hours)
// - failed/auth_revoked: older than failedRetentionDays (default 7 days)
func DeleteExpired(conns *db.Connections, completedRetentionHours int, failedRetentionDays int) error {
	completedCutoff := time.Now().Add(-time.Duration(completedRetentionHours) * time.Hour)
	failedCutoff := time.Now().AddDate(0, 0, -failedRetentionDays)

	// Delete completed entries older than retention period
	if err := conns.DB.Where("status = ? AND processed_at < ?", "completed", completedCutoff).
		Delete(&db.ScoreUpdateOutbox{}).Error; err != nil {
		return err
	}

	// Delete failed/auth_revoked entries older than retention period
	return conns.DB.Where("status IN ? AND created_at < ?",
		[]string{"failed", "auth_revoked"}, failedCutoff).
		Delete(&db.ScoreUpdateOutbox{}).Error
}
