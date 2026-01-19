package scoreoutbox

import (
	"testing"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

func TestCreate(t *testing.T) {
	conns := db.SetupTestDB(t)

	entry := &db.ScoreUpdateOutbox{
		IdempotencyKey: "test-key-1",
		OSMUserID:      123,
		SectionID:      456,
		PatrolID:       "patrol-1",
		PointsDelta:    5,
		Status:         "pending",
	}

	if err := Create(conns, entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify created
	found, err := FindByIdempotencyKey(conns, "test-key-1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if found == nil {
		t.Fatal("Expected entry to be created")
	}
	if found.OSMUserID != 123 || found.PointsDelta != 5 {
		t.Errorf("Entry fields not matching: got userID=%d delta=%d, want userID=123 delta=5",
			found.OSMUserID, found.PointsDelta)
	}
}

func TestCreateBatch(t *testing.T) {
	conns := db.SetupTestDB(t)

	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "batch-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "batch-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "pending",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Verify both created
	for _, key := range []string{"batch-1", "batch-2"} {
		found, err := FindByIdempotencyKey(conns, key)
		if err != nil {
			t.Fatalf("FindByIdempotencyKey(%s) failed: %v", key, err)
		}
		if found == nil {
			t.Errorf("Expected entry %s to be created", key)
		}
	}
}

func TestFindByIdempotencyKey(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Test not found
	found, err := FindByIdempotencyKey(conns, "nonexistent")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if found != nil {
		t.Error("Expected nil for nonexistent key")
	}

	// Create entry
	entry := &db.ScoreUpdateOutbox{
		IdempotencyKey: "find-test",
		OSMUserID:      123,
		SectionID:      456,
		PatrolID:       "patrol-1",
		PointsDelta:    5,
		Status:         "pending",
	}
	if err := Create(conns, entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test found
	found, err = FindByIdempotencyKey(conns, "find-test")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if found == nil {
		t.Fatal("Expected entry to be found")
	}
	if found.IdempotencyKey != "find-test" {
		t.Errorf("Wrong entry found: got key=%s, want key=find-test", found.IdempotencyKey)
	}
}

func TestClaimPendingForUserPatrol(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create test entries for same user+section+patrol
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "claim-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "claim-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    3,
			Status:         "pending",
		},
		{
			IdempotencyKey: "claim-other",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2", // Different patrol
			PointsDelta:    2,
			Status:         "pending",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Claim pending for patrol-1
	claimed, err := ClaimPendingForUserPatrol(conns, 123, 456, "patrol-1")
	if err != nil {
		t.Fatalf("ClaimPendingForUserPatrol failed: %v", err)
	}

	// Should claim 2 entries
	if len(claimed) != 2 {
		t.Fatalf("Expected 2 claimed entries, got %d", len(claimed))
	}

	// Verify entries are marked as processing
	for _, entry := range claimed {
		if entry.Status != "processing" {
			t.Errorf("Expected status=processing, got %s", entry.Status)
		}
		if entry.AttemptCount != 1 {
			t.Errorf("Expected attempt_count=1, got %d", entry.AttemptCount)
		}
	}

	// Verify patrol-2 entry is still pending
	other, err := FindByIdempotencyKey(conns, "claim-other")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if other.Status != "pending" {
		t.Errorf("Expected patrol-2 entry to remain pending, got %s", other.Status)
	}
}

func TestMarkCompleted(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create entries
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "complete-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "processing",
		},
		{
			IdempotencyKey: "complete-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    3,
			Status:         "processing",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Get IDs
	ids := []uint{entries[0].ID, entries[1].ID}
	processedAt := time.Now()

	// Mark completed
	if err := MarkCompleted(conns, ids, processedAt); err != nil {
		t.Fatalf("MarkCompleted failed: %v", err)
	}

	// Verify status
	for _, key := range []string{"complete-1", "complete-2"} {
		found, err := FindByIdempotencyKey(conns, key)
		if err != nil {
			t.Fatalf("FindByIdempotencyKey failed: %v", err)
		}
		if found.Status != "completed" {
			t.Errorf("Expected status=completed, got %s", found.Status)
		}
		if found.ProcessedAt == nil {
			t.Error("Expected ProcessedAt to be set")
		}
	}
}

func TestMarkFailed(t *testing.T) {
	conns := db.SetupTestDB(t)

	entry := &db.ScoreUpdateOutbox{
		IdempotencyKey: "fail-1",
		OSMUserID:      123,
		SectionID:      456,
		PatrolID:       "patrol-1",
		PointsDelta:    5,
		Status:         "processing",
	}

	if err := Create(conns, entry); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Mark failed with retry time
	nextRetry := time.Now().Add(5 * time.Minute)
	if err := MarkFailed(conns, []uint{entry.ID}, "Rate limited", &nextRetry); err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	// Verify
	found, err := FindByIdempotencyKey(conns, "fail-1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if found.Status != "failed" {
		t.Errorf("Expected status=failed, got %s", found.Status)
	}
	if found.LastError != "Rate limited" {
		t.Errorf("Expected error message, got %s", found.LastError)
	}
	if found.NextRetryAt == nil {
		t.Error("Expected NextRetryAt to be set")
	}
}

func TestMarkAuthRevoked(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create entries for user 123
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "revoke-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "revoke-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "processing",
		},
		{
			IdempotencyKey: "revoke-other",
			OSMUserID:      999, // Different user
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    2,
			Status:         "pending",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Mark user 123's entries as auth revoked
	if err := MarkAuthRevoked(conns, 123); err != nil {
		t.Fatalf("MarkAuthRevoked failed: %v", err)
	}

	// Verify user 123's entries are marked
	for _, key := range []string{"revoke-1", "revoke-2"} {
		found, err := FindByIdempotencyKey(conns, key)
		if err != nil {
			t.Fatalf("FindByIdempotencyKey failed: %v", err)
		}
		if found.Status != "auth_revoked" {
			t.Errorf("Expected status=auth_revoked for %s, got %s", key, found.Status)
		}
	}

	// Verify other user's entry is unaffected
	other, err := FindByIdempotencyKey(conns, "revoke-other")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey failed: %v", err)
	}
	if other.Status != "pending" {
		t.Errorf("Expected other user entry to remain pending, got %s", other.Status)
	}
}

func TestRecoverAuthRevoked(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create auth_revoked entries
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "recover-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "auth_revoked",
		},
		{
			IdempotencyKey: "recover-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "auth_revoked",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Recover entries
	if err := RecoverAuthRevoked(conns, 123); err != nil {
		t.Fatalf("RecoverAuthRevoked failed: %v", err)
	}

	// Verify entries are now pending
	for _, key := range []string{"recover-1", "recover-2"} {
		found, err := FindByIdempotencyKey(conns, key)
		if err != nil {
			t.Fatalf("FindByIdempotencyKey failed: %v", err)
		}
		if found.Status != "pending" {
			t.Errorf("Expected status=pending for %s, got %s", key, found.Status)
		}
	}
}

func TestCountPendingByUser(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create entries with different statuses
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "count-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "count-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "processing",
		},
		{
			IdempotencyKey: "count-3",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-3",
			PointsDelta:    2,
			Status:         "completed",
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Count pending (should include pending + processing)
	count, err := CountPendingByUser(conns, 123)
	if err != nil {
		t.Fatalf("CountPendingByUser failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count=2, got %d", count)
	}
}

func TestGetPendingDeltasBySection(t *testing.T) {
	conns := db.SetupTestDB(t)

	// Create entries for same section but different patrols
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "delta-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "delta-2",
			OSMUserID:      124,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    3, // Same patrol, should sum to 8
			Status:         "pending",
		},
		{
			IdempotencyKey: "delta-3",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    7,
			Status:         "processing",
		},
		{
			IdempotencyKey: "delta-4",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-3",
			PointsDelta:    2,
			Status:         "completed", // Should not be included
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Get deltas
	deltas, err := GetPendingDeltasBySection(conns, 456)
	if err != nil {
		t.Fatalf("GetPendingDeltasBySection failed: %v", err)
	}

	// Verify
	if len(deltas) != 2 {
		t.Fatalf("Expected 2 patrol entries, got %d", len(deltas))
	}
	if deltas["patrol-1"] != 8 {
		t.Errorf("Expected patrol-1 delta=8, got %d", deltas["patrol-1"])
	}
	if deltas["patrol-2"] != 7 {
		t.Errorf("Expected patrol-2 delta=7, got %d", deltas["patrol-2"])
	}
	if _, exists := deltas["patrol-3"]; exists {
		t.Error("Completed entry should not be included")
	}
}

func TestFindUserPatrolsWithPending(t *testing.T) {
	conns := db.SetupTestDB(t)

	now := time.Now()
	pastRetry := now.Add(-5 * time.Minute)
	futureRetry := now.Add(5 * time.Minute)

	// Create entries
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "find-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "pending",
		},
		{
			IdempotencyKey: "find-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "failed",
			NextRetryAt:    &pastRetry, // Ready to retry
		},
		{
			IdempotencyKey: "find-3",
			OSMUserID:      124,
			SectionID:      789,
			PatrolID:       "patrol-1",
			PointsDelta:    2,
			Status:         "failed",
			NextRetryAt:    &futureRetry, // Not ready yet
		},
		{
			IdempotencyKey: "find-4",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-3",
			PointsDelta:    1,
			Status:         "completed", // Should not be included
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Find patrols with pending
	keys, err := FindUserPatrolsWithPending(conns)
	if err != nil {
		t.Fatalf("FindUserPatrolsWithPending failed: %v", err)
	}

	// Should find 2: (123,456,patrol-1) and (123,456,patrol-2)
	if len(keys) != 2 {
		t.Fatalf("Expected 2 patrol keys, got %d", len(keys))
	}

	// Verify keys
	foundKeys := make(map[string]bool)
	for _, key := range keys {
		keyStr := key.PatrolID // Just use patrol ID for simple check
		foundKeys[keyStr] = true
	}

	if !foundKeys["patrol-1"] {
		t.Error("Expected to find patrol-1")
	}
	if !foundKeys["patrol-2"] {
		t.Error("Expected to find patrol-2")
	}
}

func TestDeleteExpired(t *testing.T) {
	conns := db.SetupTestDB(t)

	now := time.Now()
	oldCompleted := now.Add(-48 * time.Hour)
	recentCompleted := now.Add(-12 * time.Hour)
	oldFailed := now.Add(-10 * 24 * time.Hour)

	// Create entries
	entries := []db.ScoreUpdateOutbox{
		{
			IdempotencyKey: "expire-1",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-1",
			PointsDelta:    5,
			Status:         "completed",
			ProcessedAt:    &oldCompleted, // Older than 24 hours
			CreatedAt:      oldCompleted,
		},
		{
			IdempotencyKey: "expire-2",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-2",
			PointsDelta:    3,
			Status:         "completed",
			ProcessedAt:    &recentCompleted, // Recent, should not delete
			CreatedAt:      recentCompleted,
		},
		{
			IdempotencyKey: "expire-3",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-3",
			PointsDelta:    2,
			Status:         "failed",
			CreatedAt:      oldFailed, // Older than 7 days
		},
		{
			IdempotencyKey: "expire-4",
			OSMUserID:      123,
			SectionID:      456,
			PatrolID:       "patrol-4",
			PointsDelta:    1,
			Status:         "pending", // Should not delete
			CreatedAt:      oldCompleted,
		},
	}

	if err := CreateBatch(conns, entries); err != nil {
		t.Fatalf("CreateBatch failed: %v", err)
	}

	// Delete expired (24 hours for completed, 7 days for failed)
	if err := DeleteExpired(conns, 24, 7); err != nil {
		t.Fatalf("DeleteExpired failed: %v", err)
	}

	// Verify old completed is deleted
	found, _ := FindByIdempotencyKey(conns, "expire-1")
	if found != nil {
		t.Error("Expected old completed entry to be deleted")
	}

	// Verify recent completed still exists
	found, _ = FindByIdempotencyKey(conns, "expire-2")
	if found == nil {
		t.Error("Expected recent completed entry to remain")
	}

	// Verify old failed is deleted
	found, _ = FindByIdempotencyKey(conns, "expire-3")
	if found != nil {
		t.Error("Expected old failed entry to be deleted")
	}

	// Verify pending still exists
	found, _ = FindByIdempotencyKey(conns, "expire-4")
	if found == nil {
		t.Error("Expected pending entry to remain")
	}
}
