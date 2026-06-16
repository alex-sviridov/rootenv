package pbclient

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, tlsVerify bool) *Client {
	transport := &http.Transport{}
	if !tlsVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second, Transport: transport},
	}
}

// ValidateToken calls PocketBase auth-refresh and returns the userID on success.
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
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth-refresh returned status %d", resp.StatusCode)
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
		return "", fmt.Errorf("auth-refresh returned empty user id")
	}
	return result.Record.ID, nil
}

// GetAttempt fetches an attempt record using the user's own token.
// PocketBase's viewRule enforces ownership — 403 means wrong user or not found.
// Returns the attempt owner's userID on success.
func (c *Client) GetAttempt(token, attemptID string) (string, error) {
	u := c.baseURL + "/api/collections/attempts/records/" + url.PathEscape(attemptID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("forbidden or not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get attempt returned status %d", resp.StatusCode)
	}

	var a struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return "", err
	}
	return a.User, nil
}
