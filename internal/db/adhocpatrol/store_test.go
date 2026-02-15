package adhocpatrol

import (
	"testing"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

func TestListByUser_Empty(t *testing.T) {
	conns := db.SetupTestDB(t)

	patrols, err := ListByUser(conns, 123)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patrols) != 0 {
		t.Fatalf("expected empty list, got %d patrols", len(patrols))
	}
}

func TestCreate_AssignsPosition(t *testing.T) {
	conns := db.SetupTestDB(t)

	p1 := &db.AdhocPatrol{OSMUserID: 1, Name: "Team A", Color: "red"}
	if err := Create(conns, p1); err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if p1.Position != 0 {
		t.Errorf("first patrol position = %d, want 0", p1.Position)
	}

	p2 := &db.AdhocPatrol{OSMUserID: 1, Name: "Team B", Color: "blue"}
	if err := Create(conns, p2); err != nil {
		t.Fatalf("create p2: %v", err)
	}
	if p2.Position != 1 {
		t.Errorf("second patrol position = %d, want 1", p2.Position)
	}
}

func TestCreate_MaxLimit(t *testing.T) {
	conns := db.SetupTestDB(t)

	for i := 0; i < MaxPatrolsPerUser; i++ {
		p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team"}
		if err := Create(conns, p); err != nil {
			t.Fatalf("create patrol %d: %v", i, err)
		}
	}

	// 21st should fail
	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Too Many"}
	err := Create(conns, p)
	if err != ErrMaxPatrolsReached {
		t.Errorf("expected ErrMaxPatrolsReached, got %v", err)
	}
}

func TestCreate_DifferentUsersIndependent(t *testing.T) {
	conns := db.SetupTestDB(t)

	p1 := &db.AdhocPatrol{OSMUserID: 1, Name: "User1 Team"}
	if err := Create(conns, p1); err != nil {
		t.Fatalf("create: %v", err)
	}

	p2 := &db.AdhocPatrol{OSMUserID: 2, Name: "User2 Team"}
	if err := Create(conns, p2); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Both should have position 0
	if p1.Position != 0 || p2.Position != 0 {
		t.Errorf("positions: user1=%d user2=%d, both should be 0", p1.Position, p2.Position)
	}

	// Each user sees only their own
	patrols1, _ := ListByUser(conns, 1)
	patrols2, _ := ListByUser(conns, 2)
	if len(patrols1) != 1 || len(patrols2) != 1 {
		t.Errorf("user1 patrols=%d, user2 patrols=%d, both should be 1", len(patrols1), len(patrols2))
	}
}

func TestListByUser_OrderedByPosition(t *testing.T) {
	conns := db.SetupTestDB(t)

	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		p := &db.AdhocPatrol{OSMUserID: 1, Name: name}
		if err := Create(conns, p); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	patrols, err := ListByUser(conns, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(patrols) != 3 {
		t.Fatalf("expected 3 patrols, got %d", len(patrols))
	}
	if patrols[0].Name != "Alpha" || patrols[1].Name != "Bravo" || patrols[2].Name != "Charlie" {
		t.Errorf("unexpected order: %s, %s, %s", patrols[0].Name, patrols[1].Name, patrols[2].Name)
	}
}

func TestUpdate_Success(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Old Name", Color: "red"}
	Create(conns, p)

	err := Update(conns, p.ID, 1, "New Name", "blue")
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	found, _ := FindByIDAndUser(conns, p.ID, 1)
	if found.Name != "New Name" || found.Color != "blue" {
		t.Errorf("after update: name=%q color=%q", found.Name, found.Color)
	}
}

func TestUpdate_WrongUser(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team"}
	Create(conns, p)

	err := Update(conns, p.ID, 999, "Hacked", "red")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete_Success(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team"}
	Create(conns, p)

	err := Delete(conns, p.ID, 1)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	patrols, _ := ListByUser(conns, 1)
	if len(patrols) != 0 {
		t.Errorf("expected 0 patrols after delete, got %d", len(patrols))
	}
}

func TestDelete_WrongUser(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team"}
	Create(conns, p)

	err := Delete(conns, p.ID, 999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Original patrol still exists
	patrols, _ := ListByUser(conns, 1)
	if len(patrols) != 1 {
		t.Errorf("patrol should still exist, got %d patrols", len(patrols))
	}
}

func TestUpdateScore(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team", Score: 10}
	Create(conns, p)

	err := UpdateScore(conns, p.ID, 1, 25)
	if err != nil {
		t.Fatalf("update score: %v", err)
	}

	found, _ := FindByIDAndUser(conns, p.ID, 1)
	if found.Score != 25 {
		t.Errorf("score = %d, want 25", found.Score)
	}
}

func TestUpdateScore_WrongUser(t *testing.T) {
	conns := db.SetupTestDB(t)

	p := &db.AdhocPatrol{OSMUserID: 1, Name: "Team", Score: 10}
	Create(conns, p)

	err := UpdateScore(conns, p.ID, 999, 100)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestResetAllScores(t *testing.T) {
	conns := db.SetupTestDB(t)

	p1 := &db.AdhocPatrol{OSMUserID: 1, Name: "Team A", Score: 10}
	p2 := &db.AdhocPatrol{OSMUserID: 1, Name: "Team B", Score: 20}
	p3 := &db.AdhocPatrol{OSMUserID: 2, Name: "Other User", Score: 30}
	Create(conns, p1)
	Create(conns, p2)
	Create(conns, p3)

	err := ResetAllScores(conns, 1)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}

	patrols, _ := ListByUser(conns, 1)
	for _, p := range patrols {
		if p.Score != 0 {
			t.Errorf("patrol %q score = %d, want 0", p.Name, p.Score)
		}
	}

	// Other user's scores unaffected
	otherPatrols, _ := ListByUser(conns, 2)
	if otherPatrols[0].Score != 30 {
		t.Errorf("other user score = %d, want 30", otherPatrols[0].Score)
	}
}

func TestFindByIDAndUser_NotFound(t *testing.T) {
	conns := db.SetupTestDB(t)

	_, err := FindByIDAndUser(conns, 999, 1)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
