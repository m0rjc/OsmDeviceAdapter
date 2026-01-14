package oauthclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

func New(clientId, clientSecret, redirectUri, osmDomain string) *WebFlowClient {
	return &WebFlowClient{
		clientID:     clientId,
		clientSecret: clientSecret,
		redirectURI:  redirectUri,
		osmDomain:    osmDomain,
		httpClient:   &http.Client{},
	}
}

type WebFlowClient struct {
	clientID     string
	clientSecret string
	redirectURI  string
	osmDomain    string
	httpClient   *http.Client
}

func (c *WebFlowClient) RefreshToken(ctx context.Context, refreshToken string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	// Make direct HTTP request to OSM OAuth endpoint
	tokenURL := c.osmDomain + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for 401 (user revoked access)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("OSM access unauthorized (revoked)")
	}

	// Check for other non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
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

	return fmt.Sprintf("%s/oauth/authorize?%s", c.osmDomain, params.Encode())
}

func (c *WebFlowClient) ExchangeCodeForToken(code string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", c.redirectURI)
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	// Make direct HTTP request to OSM OAuth endpoint
	tokenURL := c.osmDomain + "/oauth/token"
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}
