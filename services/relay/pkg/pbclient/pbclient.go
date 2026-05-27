package pbclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	adminToken string
}

type Server struct {
	ID      string `json:"id"`
	Attempt string `json:"attempt"`
	User    string `json:"user"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Status  string `json:"status"`
}

type ServerConfig struct {
	ID         string          `json:"id"`
	Connection json.RawMessage `json:"connection"`
}

type Attempt struct {
	ID   string `json:"id"`
	User string `json:"user"`
	Lab  string `json:"lab"`
}

func New(baseURL, adminToken string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		adminToken: adminToken,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWithCredentials creates a Client that authenticates as an admin user.
func NewWithCredentials(baseURL, username, password string) (*Client, error) {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	token, err := c.authenticate(username, password)
	if err != nil {
		return nil, err
	}
	c.adminToken = token
	return c, nil
}

// NewUnauthenticated creates a Client without performing authentication.
// Call Reconnect before using the client for authorized requests.
func NewUnauthenticated(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Reconnect re-authenticates the client with new credentials, updating the admin token.
func (c *Client) Reconnect(username, password string) error {
	token, err := c.authenticate(username, password)
	if err != nil {
		return err
	}
	c.adminToken = token
	return nil
}

func (c *Client) authenticate(username, password string) (string, error) {
	body := fmt.Sprintf(`{"identity":%q,"password":%q}`, username, password)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("svc auth failed: status %d", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("svc auth returned empty token")
	}
	return result.Token, nil
}

// ValidateToken verifies a user token against PocketBase and returns the userID.
func (c *Client) ValidateToken(token string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-refresh", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token validation returned status %d", resp.StatusCode)
	}

	var result struct {
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Record.ID == "" {
		return "", fmt.Errorf("token validation returned empty user id")
	}
	return result.Record.ID, nil
}

// GetServer fetches a server record by ID using admin credentials.
func (c *Client) GetServer(serverID string) (*Server, error) {
	u := c.baseURL + "/api/collections/assets/records/" + url.PathEscape(serverID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.adminToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get server returned status %d", resp.StatusCode)
	}

	var s Server
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetServerConfig fetches the assets_configs record for the given asset ID.
func (c *Client) GetServerConfig(serverID string) (*ServerConfig, error) {
	u := c.baseURL + "/api/collections/assets_configs/records?filter=" + url.QueryEscape("(asset='"+serverID+"')")+"&perPage=1"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.adminToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get server config returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []ServerConfig `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, ErrNotFound
	}
	return &result.Items[0], nil
}

type KeysRecord struct {
	ID           string `json:"id"`
	KeyEncrypted string `json:"key_encrypted"`
}

// GetKeysByAsset fetches the keys record for the given asset ID.
func (c *Client) GetKeysByAsset(assetID string) (*KeysRecord, error) {
	u := c.baseURL + "/api/collections/keys/records?filter=" + url.QueryEscape("(asset='"+assetID+"')")+"&perPage=1"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.adminToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get keys returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []KeysRecord `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, ErrNotFound
	}
	return &result.Items[0], nil
}

// GetAttempt fetches an attempt record by ID using admin credentials.
func (c *Client) GetAttempt(attemptID string) (*Attempt, error) {
	u := c.baseURL + "/api/collections/attempts/records/" + url.PathEscape(attemptID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.adminToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get attempt returned status %d", resp.StatusCode)
	}

	var a Attempt
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}
