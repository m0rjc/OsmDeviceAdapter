package osm

import (
	"context"
	"errors"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func GetPatrolScores(ctx context.Context, osm *Client, user types.User) ([]types.PatrolScore, error) {
	return nil, errors.New("GetPatrolScores is not yet implemented")
}
