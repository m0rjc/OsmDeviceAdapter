package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrLockNotAcquired is returned when a lock cannot be acquired
	ErrLockNotAcquired = errors.New("lock not acquired")

	// ErrLockNotHeld is returned when trying to release a lock that is not held
	ErrLockNotHeld = errors.New("lock not held")
)

// RedisLock provides distributed locking using Redis.
// Implements a simple lock mechanism with TTL for safety.
type RedisLock struct {
	client *redis.Client
	key    string
	value  string
	ttl    time.Duration
}

// NewRedisLock creates a new Redis-based distributed lock.
// The lock key is formatted as: outbox:lock:{userID}:{sectionID}:{patrolID}
func NewRedisLock(client *redis.Client, userID, sectionID int, patrolID string, ttl time.Duration) *RedisLock {
	key := fmt.Sprintf("outbox:lock:%d:%d:%s", userID, sectionID, patrolID)
	// Use a unique value to ensure only the lock holder can release it
	value := fmt.Sprintf("%d-%d", time.Now().UnixNano(), userID)

	return &RedisLock{
		client: client,
		key:    key,
		value:  value,
		ttl:    ttl,
	}
}

// Acquire attempts to acquire the lock.
// Returns ErrLockNotAcquired if the lock is already held by another process.
func (l *RedisLock) Acquire(ctx context.Context) error {
	// Use SET NX (set if not exists) with expiry
	ok, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
	if err != nil {
		return fmt.Errorf("redis set failed: %w", err)
	}

	if !ok {
		return ErrLockNotAcquired
	}

	return nil
}

// Release releases the lock.
// Returns ErrLockNotHeld if the lock is not held by this instance.
// Uses Lua script to ensure atomic check-and-delete.
func (l *RedisLock) Release(ctx context.Context) error {
	// Lua script to check value matches before deleting
	// This ensures we only delete our own lock
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	if err != nil {
		return fmt.Errorf("redis eval failed: %w", err)
	}

	// Result is 1 if deleted, 0 if not found or value didn't match
	if result.(int64) == 0 {
		return ErrLockNotHeld
	}

	return nil
}

// TryAcquire attempts to acquire the lock, returning immediately.
// Returns true if acquired, false if not available (non-error case).
func (l *RedisLock) TryAcquire(ctx context.Context) (bool, error) {
	err := l.Acquire(ctx)
	if err == ErrLockNotAcquired {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Extend extends the lock TTL.
// Returns ErrLockNotHeld if the lock is not held by this instance.
func (l *RedisLock) Extend(ctx context.Context, ttl time.Duration) error {
	// Lua script to check value matches before extending
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{l.key}, l.value, ttl.Milliseconds()).Result()
	if err != nil {
		return fmt.Errorf("redis eval failed: %w", err)
	}

	if result.(int64) == 0 {
		return ErrLockNotHeld
	}

	return nil
}
