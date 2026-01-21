package scoreupdateservice

import (
	"context"
	"fmt"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

type PatrolLockSet struct {
	client    *db.RedisClient
	ttl       time.Duration
	lockValue string
	held      map[string]bool
}

func NewPatrolLockSet(client *db.RedisClient, userId int, ttl time.Duration) *PatrolLockSet {
	return &PatrolLockSet{
		client:    client,
		ttl:       ttl,
		lockValue: fmt.Sprintf("%d-%d", time.Now().UnixNano(), userId),
		held:      make(map[string]bool),
	}
}

func (l *PatrolLockSet) AddPatrol(sectionId int, patrolId string) {
	key := l.internalKey(sectionId, patrolId)
	// Do not allow a held lock to be marked released.
	_, keyKnown := l.held[key]
	if !keyKnown {
		l.held[key] = false
	}
}

func (l *PatrolLockSet) Acquire(ctx context.Context) error {
	for key, alreadyHeld := range l.held {
		if !alreadyHeld {

			// Use SET NX (set if not exists) with expiry
			ok, err := l.client.SetNX(ctx, key, l.lockValue, l.ttl).Result()
			if err != nil {
				return fmt.Errorf("redis set failed: %w", err)
			}

			if ok {
				l.held[key] = true
			}
		}
	}
	return nil
}

func (l *PatrolLockSet) IsHeld(sectionId int, patrolId string) bool {
	key := l.internalKey(sectionId, patrolId)
	state, known := l.held[key]
	return known && state
}

func (l *PatrolLockSet) internalKey(sectionId int, patrolId string) string {
	return fmt.Sprintf("patrol:lock:%d:%s", sectionId, patrolId)
}

func (l *PatrolLockSet) Release(ctx context.Context) error {
	// Lua script to check value matches before deleting
	// This ensures we only delete our own lock
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
	for key, isHeld := range l.held {
		if isHeld {
			result, err := l.client.Eval(ctx, script, []string{key}, l.lockValue).Result()
			if err != nil {
				return fmt.Errorf("redis eval failed: %w", err)
			}

			// Result is 1 if deleted, 0 if not found or value didn't match
			if result.(int64) == 1 {
				l.held[key] = false
			}
		}
	}
	return nil
}
