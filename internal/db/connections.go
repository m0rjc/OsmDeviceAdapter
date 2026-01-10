package db

import "gorm.io/gorm"

// Connections holds database and cache connections
type Connections struct {
	DB    *gorm.DB
	Redis *RedisClient
}

// NewConnections creates a new Connections instance
func NewConnections(db *gorm.DB, redis *RedisClient) *Connections {
	return &Connections{
		DB:    db,
		Redis: redis,
	}
}
