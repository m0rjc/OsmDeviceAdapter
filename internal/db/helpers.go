package db

import (
	"gorm.io/gorm/clause"
)

// ForUpdateSkipLocked returns a GORM clause for SELECT ... FOR UPDATE SKIP LOCKED
// This is used for safe concurrent access to rows that need processing
func ForUpdateSkipLocked() clause.Locking {
	return clause.Locking{
		Strength: "UPDATE",
		Options:  "SKIP LOCKED",
	}
}
