package handlers

import (
	"database/sql"

	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
)

type Dependencies struct {
	Config      *config.Config
	DB          *sql.DB
	RedisClient *db.RedisClient
}
