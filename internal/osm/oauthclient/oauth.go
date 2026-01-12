package oauthclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/m0rjc/OsmDeviceAdapter/internal/osm"
	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func New(clientId, clientSecret, redirectUri string, osm *osm.Client) *WebFlowClient {
	return &WebFlowClient{
		clientID:     clientId,
		clientSecret: clientSecret,
		redirectURI:  redirectUri,
		osm:          osm,
	}
}

type WebFlowClient struct {
	clientID     string
	clientSecret string
	redirectURI  string
	osm          *osm.Client
}

func (c *WebFlowClient) RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	var tokenResp types.OSMTokenResponse
	_, err := c.osm.Request(ctx, http.MethodPost, &tokenResp,
		osm.WithPath("/oauth/token"),
		osm.WithUrlEncodedBody(&data),
	)
	if err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func (c *WebFlowClient) BuildAuthURL(scope, state string) string {
	if scope == "" {
		// Fallback until I work out who's responsible for this
		scope = "section:member:read"
	}
	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("redirect_uri", c.redirectURI)
	params.Set("response_type", "code")
	params.Set("state", state)
	params.Set("scope", scope)

	return fmt.Sprintf("%s/oauth/authorize?%s", c.osm.OSMDomain(), params.Encode())
}

func (c *WebFlowClient) ExchangeCodeForToken(code string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", c.redirectURI)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	var tokenResp types.OSMTokenResponse
	_, err := c.osm.Request(context.Background(), http.MethodPost, &tokenResp,
		osm.WithPath("/oauth/token"),
		osm.WithPostBody(strings.NewReader(data.Encode())),
		osm.WithContentType("application/x-www-form-urlencoded"),
		osm.WithSensitive(),
	)
	if err != nil {
		return nil, err
	}

	return &tokenResp, nil
}
