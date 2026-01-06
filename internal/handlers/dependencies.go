package handlers

import (
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"gorm.io/gorm"
)

type Dependencies struct {
	Config      *config.Config
	DB          *gorm.DB
	RedisClient *db.RedisClient
}
