package osm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/m0rjc/OsmDeviceAdapter/internal/types"
)

type Client struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

type Patrol struct {
	PatrolID   string `json:"patrol_id"`
	PatrolName string `json:"patrol_name"`
	Points     int    `json:"points"`
}

func NewClient(baseURL, accessToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) GetPatrolScores(ctx context.Context) ([]types.PatrolScore, error) {
	// Note: This is a placeholder implementation
	// You'll need to adjust this based on OSM's actual API endpoints
	// OSM API documentation: https://www.onlinescoutmanager.co.uk/api/

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/api.php", c.baseURL),
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Add authorization header
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.accessToken))

	// Add OSM-specific parameters
	q := req.URL.Query()
	q.Set("action", "getPatrolScores") // Adjust based on actual OSM API
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OSM API error: %s - %s", resp.Status, string(body))
	}

	var patrols []Patrol
	if err := json.NewDecoder(resp.Body).Decode(&patrols); err != nil {
		return nil, err
	}

	// Convert to our response format
	result := make([]types.PatrolScore, len(patrols))
	for i, p := range patrols {
		result[i] = types.PatrolScore{
			ID:    p.PatrolID,
			Name:  p.PatrolName,
			Score: p.Points,
		}
	}

	return result, nil
}

func (c *Client) RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*types.OSMTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/oauth/token", c.baseURL),
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp types.OSMTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}
