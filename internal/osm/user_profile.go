package osm

import (
	"context"
	"net/http"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func (c *Client) FetchOSMProfile(user types.User) (*types.OSMProfileResponse, error) {
	var profileResp types.OSMProfileResponse
	_, err := c.Request(context.Background(), http.MethodGet, &profileResp,
		WithPath("/oauth/resource"),
		WithUser(user),
	)
	if err != nil {
		return nil, err
	}

	return &profileResp, nil
}
