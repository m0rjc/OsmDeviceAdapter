package handlers

import (
	"github.com/m0rjc/OsmDeviceAdapter/internal/config"
	"github.com/m0rjc/OsmDeviceAdapter/internal/db"
	"github.com/m0rjc/OsmDeviceAdapter/internal/deviceauth"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/osm/oauthclient"
	"github.com/m0rjc/OsmDeviceAdapter/internal/webauth"
)

type Dependencies struct {
	Config     *config.Config
	Conns      *db.Connections
	OSM        *osm.Client
	OSMAuth    *oauthclient.WebFlowClient
	DeviceAuth *deviceauth.Service
	WebAuth    *webauth.Service
}
