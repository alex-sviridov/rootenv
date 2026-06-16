package pocketbase

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = fmt.Errorf("record not found")

type Client struct {
	baseURL      string
	email        string
	password     string
	httpClient   *http.Client
	streamClient *http.Client

	tokenMu sync.Mutex
	token   string
}

type AttemptRecord struct {
	ID                 string `json:"id"`
	UserId             string `json:"user"`
	UserName           string `json:"userName"`
	Lab                string `json:"lab"`
	LabName            string `json:"lab_name"`
	CurrentState       string `json:"current_state"`
	DesiredState       string `json:"desired_state"`
	ExpiresAt          string `json:"expires_at"`
	DecommissionReason string `json:"-"` // not from PocketBase; set by the controller
	Expand             struct {
		Lab struct {
			Environment json.RawMessage `json:"environment"`
		} `json:"lab"`
	} `json:"expand"`
}

func NewClient(baseURL, email, password string, tlsVerify bool) (*Client, error) {
	transport := &http.Transport{}
	if !tlsVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		email:      email,
		password:   password,
		httpClient: &http.Client{Timeout: 10 * time.Second, Transport: transport},
		// No overall timeout: the realtime SSE connection stays open
		// indefinitely, and Client.Timeout would cut off the body read.
		streamClient: &http.Client{Transport: transport},
	}
	if err := c.reauth(); err != nil {
		return nil, err
	}
	return c, nil
}

// reauth re-authenticates against PocketBase and stores the new token.
func (c *Client) reauth() error {
	body := fmt.Sprintf(`{"identity":%q,"password":%q}`, c.email, c.password)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth failed: status %d", resp.StatusCode)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Token == "" {
		return fmt.Errorf("auth returned empty token")
	}
	c.tokenMu.Lock()
	c.token = result.Token
	c.tokenMu.Unlock()
	return nil
}

func (c *Client) currentToken() string {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.token
}

func (c *Client) get(path string, out any) error {
	resp, err := c.doGet(path)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if err := c.reauth(); err != nil {
			return fmt.Errorf("GET %s: reauth: %w", path, err)
		}
		resp, err = c.doGet(path)
		if err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) doGet(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.currentToken())
	return c.httpClient.Do(req)
}

func (c *Client) ListActiveAttempts() ([]AttemptRecord, error) {
	var result struct {
		Items []AttemptRecord `json:"items"`
	}
	filter := url.QueryEscape("(current_state!=desired_state)")
	expand := url.QueryEscape("lab")
	if err := c.get("/api/collections/attempts/records?filter="+filter+"&expand="+expand, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// GetAttempt fetches a single attempt record with its lab expanded.
// Realtime subscription events don't carry expanded relations, so callers
// that need attempt.expand.lab.environment must re-fetch via this method.
func (c *Client) GetAttempt(id string) (AttemptRecord, error) {
	var rec AttemptRecord
	expand := url.QueryEscape("lab")
	if err := c.get("/api/collections/attempts/records/"+id+"?expand="+expand, &rec); err != nil {
		return AttemptRecord{}, err
	}
	return rec, nil
}
