package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreaudit"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db/scoreoutbox"
	"github.com/m0rjc/OsmDeviceAdapter/internal/metrics"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/redis/go-redis/v9"
)

// PatrolSyncService handles synchronization of pending score updates to OSM.
// Implements the core business logic for processing the outbox pattern.
type PatrolSyncService struct {
	conns            *db.Connections
	osmClient        *osm.Client
	credentialMgr    *CredentialManager
	lockTTL          time.Duration
	redisClient      *redis.Client
}

// NewPatrolSyncService creates a new patrol sync service
func NewPatrolSyncService(conns *db.Connections, osmClient *osm.Client, credentialMgr *CredentialManager, redisClient *redis.Client) *PatrolSyncService {
	return &PatrolSyncService{
		conns:         conns,
		osmClient:     osmClient,
		credentialMgr: credentialMgr,
		lockTTL:       60 * time.Second, // 60-second lock TTL as per spec
		redisClient:   redisClient,
	}
}

// SyncPatrol synchronizes pending score updates for a specific user+section+patrol combination.
// This is the main entry point for processing outbox entries.
//
// Algorithm:
//  1. Acquire Redis distributed lock for user+section+patrol
//  2. Claim pending entries using SELECT FOR UPDATE SKIP LOCKED
//  3. Get user credentials and refresh tokens if needed
//  4. Fetch current score from OSM
//  5. Calculate net delta from all claimed entries
//  6. Apply single OSM update with new score
//  7. Mark entries as completed and create audit log
//  8. Update user credentials last_used_at timestamp
//  9. Release lock
//
// Returns nil on success, error on failure.
func (s *PatrolSyncService) SyncPatrol(ctx context.Context, osmUserID int, sectionID int, patrolID string) error {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ScoreOutboxSyncDuration.Observe(duration)
	}()

	logger := slog.With(
		"component", "worker.patrol_sync",
		"osm_user_id", osmUserID,
		"section_id", sectionID,
		"patrol_id", patrolID,
	)

	// Step 1: Acquire distributed lock
	lock := NewRedisLock(s.redisClient, osmUserID, sectionID, patrolID, s.lockTTL)
	acquired, err := lock.TryAcquire(ctx)
	if err != nil {
		logger.Error("worker.patrol_sync.lock_error",
			"event", "sync.lock_error",
			"error", err,
		)
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		// Another worker is already processing this patrol, skip
		logger.Debug("worker.patrol_sync.already_locked",
			"event", "sync.already_locked",
		)
		return nil
	}
	defer func() {
		if err := lock.Release(ctx); err != nil {
			logger.Error("worker.patrol_sync.lock_release_error",
				"event", "sync.lock_release_error",
				"error", err,
			)
		}
	}()

	// Step 2: Claim pending entries
	entries, err := scoreoutbox.ClaimPendingForUserPatrol(s.conns, osmUserID, sectionID, patrolID)
	if err != nil {
		logger.Error("worker.patrol_sync.claim_failed",
			"event", "sync.claim_error",
			"error", err,
		)
		return fmt.Errorf("failed to claim entries: %w", err)
	}

	if len(entries) == 0 {
		// No entries to process (may have been claimed by another worker before lock)
		logger.Debug("worker.patrol_sync.no_entries",
			"event", "sync.no_entries",
		)
		return nil
	}

	logger.Info("worker.patrol_sync.claimed",
		"event", "sync.entries_claimed",
		"entry_count", len(entries),
	)

	// Step 3: Get user credentials and refresh tokens if needed
	accessToken, err := s.credentialMgr.GetCredentials(ctx, osmUserID)
	if err != nil {
		logger.Error("worker.patrol_sync.credentials_failed",
			"event", "sync.credentials_error",
			"error", err,
		)

		// Mark entries as failed (auth may have been revoked)
		entryIDs := extractIDs(entries)
		nextRetry := calculateNextRetry(entries[0].AttemptCount)
		if markErr := scoreoutbox.MarkFailed(s.conns, entryIDs, err.Error(), nextRetry); markErr != nil {
			logger.Error("worker.patrol_sync.mark_failed_error",
				"event", "sync.mark_failed_error",
				"error", markErr,
			)
		}
		return fmt.Errorf("failed to get credentials: %w", err)
	}

	// Create a user context for OSM API calls
	user := &userContext{
		osmUserID:   osmUserID,
		accessToken: accessToken,
	}

	// Step 4: Fetch current score from OSM
	// First get the active term
	termInfo, err := s.osmClient.FetchActiveTermForSection(ctx, user, sectionID)
	if err != nil {
		logger.Error("worker.patrol_sync.term_fetch_failed",
			"event", "sync.term_error",
			"error", err,
		)
		entryIDs := extractIDs(entries)
		nextRetry := calculateNextRetry(entries[0].AttemptCount)
		if markErr := scoreoutbox.MarkFailed(s.conns, entryIDs, err.Error(), nextRetry); markErr != nil {
			logger.Error("worker.patrol_sync.mark_failed_error",
				"event", "sync.mark_failed_error",
				"error", markErr,
			)
		}
		return fmt.Errorf("failed to fetch term: %w", err)
	}

	// Fetch patrol scores
	currentScores, _, err := s.osmClient.FetchPatrolScores(ctx, user, sectionID, termInfo.TermID)
	if err != nil {
		logger.Error("worker.patrol_sync.scores_fetch_failed",
			"event", "sync.scores_error",
			"error", err,
		)
		entryIDs := extractIDs(entries)
		nextRetry := calculateNextRetry(entries[0].AttemptCount)
		if markErr := scoreoutbox.MarkFailed(s.conns, entryIDs, err.Error(), nextRetry); markErr != nil {
			logger.Error("worker.patrol_sync.mark_failed_error",
				"event", "sync.mark_failed_error",
				"error", markErr,
			)
		}
		return fmt.Errorf("failed to fetch scores: %w", err)
	}

	// Find the patrol in current scores
	var currentScore int
	var patrolName string
	found := false
	for _, p := range currentScores {
		if p.ID == patrolID {
			currentScore = p.Score
			patrolName = p.Name
			found = true
			break
		}
	}

	if !found {
		logger.Error("worker.patrol_sync.patrol_not_found",
			"event", "sync.patrol_not_found",
		)
		// Patrol doesn't exist in OSM - use patrol ID as fallback name for error message
		patrolName = patrolID
		// Mark as failed - patrol doesn't exist in OSM
		entryIDs := extractIDs(entries)
		errMsg := fmt.Sprintf("patrol %s not found in section %d", patrolID, sectionID)
		if markErr := scoreoutbox.MarkFailed(s.conns, entryIDs, errMsg, nil); markErr != nil {
			logger.Error("worker.patrol_sync.mark_failed_error",
				"event", "sync.mark_failed_error",
				"error", markErr,
			)
		}
		return fmt.Errorf("patrol not found")
	}

	// Step 5: Calculate net delta from all claimed entries
	netDelta := 0
	for _, entry := range entries {
		netDelta += entry.PointsDelta
	}

	newScore := currentScore + netDelta

	logger.Info("worker.patrol_sync.applying_update",
		"event", "sync.applying_update",
		"current_score", currentScore,
		"net_delta", netDelta,
		"new_score", newScore,
	)

	// Step 6: Apply single OSM update
	if err := s.osmClient.UpdatePatrolScore(ctx, user, sectionID, patrolID, newScore); err != nil {
		logger.Error("worker.patrol_sync.update_failed",
			"event", "sync.update_error",
			"error", err,
		)
		entryIDs := extractIDs(entries)
		nextRetry := calculateNextRetry(entries[0].AttemptCount)
		if markErr := scoreoutbox.MarkFailed(s.conns, entryIDs, err.Error(), nextRetry); markErr != nil {
			logger.Error("worker.patrol_sync.mark_failed_error",
				"event", "sync.mark_failed_error",
				"error", markErr,
			)
		}
		return fmt.Errorf("failed to update score: %w", err)
	}

	// Step 7: Mark entries as completed and create audit log
	processedAt := time.Now()
	entryIDs := extractIDs(entries)

	if err := scoreoutbox.MarkCompleted(s.conns, entryIDs, processedAt); err != nil {
		logger.Error("worker.patrol_sync.mark_completed_failed",
			"event", "sync.mark_completed_error",
			"error", err,
		)
		// Don't return error - the OSM update succeeded
	} else {
		// Record metrics for successfully processed entries
		metrics.ScoreOutboxEntriesProcessed.WithLabelValues("completed").Add(float64(len(entries)))
	}

	// Create audit log entry (one entry for the aggregated update)
	auditLog := db.ScoreAuditLog{
		OSMUserID:     osmUserID,
		SectionID:     sectionID,
		PatrolID:      patrolID,
		PatrolName:    patrolName,
		PreviousScore: currentScore,
		NewScore:      newScore,
		PointsAdded:   netDelta,
	}

	if err := scoreaudit.CreateBatch(s.conns, []db.ScoreAuditLog{auditLog}); err != nil {
		logger.Error("worker.patrol_sync.audit_log_failed",
			"event", "sync.audit_error",
			"error", err,
		)
		// Don't return error - the update succeeded
	}

	// Step 8: Update user credentials last_used_at timestamp
	if err := s.credentialMgr.UpdateLastUsed(ctx, osmUserID); err != nil {
		logger.Error("worker.patrol_sync.last_used_update_failed",
			"event", "sync.last_used_error",
			"error", err,
		)
		// Don't return error - the update succeeded
	}

	logger.Info("worker.patrol_sync.success",
		"event", "sync.success",
		"entries_processed", len(entries),
		"net_delta", netDelta,
	)

	return nil
}

// userContext implements types.User for OSM API calls
type userContext struct {
	osmUserID   int
	accessToken string
}

func (u *userContext) UserID() *int {
	return &u.osmUserID
}

func (u *userContext) AccessToken() string {
	return u.accessToken
}

// extractIDs extracts the IDs from a slice of outbox entries
func extractIDs(entries []db.ScoreUpdateOutbox) []uint {
	ids := make([]uint, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}

// calculateNextRetry calculates the next retry time using exponential backoff
// Returns nil for permanent failures (max attempts reached)
func calculateNextRetry(attemptCount int) *time.Time {
	const maxAttempts = 10

	if attemptCount >= maxAttempts {
		return nil // Permanent failure
	}

	// Exponential backoff: 1min, 2min, 4min, 8min, 16min, 32min, 1h, 2h, 4h, 8h
	backoffMinutes := 1 << attemptCount // 2^attemptCount
	if backoffMinutes > 480 {           // Cap at 8 hours
		backoffMinutes = 480
	}

	nextRetry := time.Now().Add(time.Duration(backoffMinutes) * time.Minute)
	return &nextRetry
}
