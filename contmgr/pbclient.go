package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type pbClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type Asset struct {
	ID            string          `json:"id"`
	Attempt       string          `json:"attempt"`
	Name          string          `json:"name"`
	State         string          `json:"state"`
	Platform      string          `json:"platform"`
	Configuration json.RawMessage `json:"configuration"`
}

func (a *Asset) Def() (*AssetDef, error) {
	var def AssetDef
	if err := json.Unmarshal(a.Configuration, &def); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	return &def, nil
}

type AssetDef struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	SSHUser string `json:"ssh_user"`
	CPU     any    `json:"cpu"`
	Memory  string `json:"memory"`
}

type KeysRecord struct {
	ID           string `json:"id"`
	Secret       string `json:"secret"`
	KeyEncrypted string `json:"key_encrypted"`
}

type Command struct {
	ID     string `json:"id"`
	Asset  string `json:"asset"`
	Status string `json:"status"`
}

func newPBClient(baseURL, email, password string) (*pbClient, error) {
	c := &pbClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	body := fmt.Sprintf(`{"identity":%q,"password":%q}`, email, password)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth failed: status %d", resp.StatusCode)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Token == "" {
		return nil, fmt.Errorf("auth returned empty token")
	}
	c.token = result.Token
	return c, nil
}

func (c *pbClient) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found: %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *pbClient) patch(path string, fields map[string]any) error {
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PATCH %s: status %d: %s", path, resp.StatusCode, body)
	}
	return nil
}

func (c *pbClient) ListPendingAssets() ([]Asset, error) {
	var result struct {
		Items []Asset `json:"items"`
	}
	filter := url.QueryEscape("(state='pending')")
	if err := c.get("/api/collections/assets/records?filter="+filter, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}


func (c *pbClient) GetAsset(id string) (*Asset, error) {
	var a Asset
	if err := c.get("/api/collections/assets/records/"+url.PathEscape(id), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c *pbClient) GetKeysByAsset(assetID string) (*KeysRecord, error) {
	var result struct {
		Items []KeysRecord `json:"items"`
	}
	filter := url.QueryEscape("(asset='" + assetID + "')")
	if err := c.get("/api/collections/keys/records?filter="+filter+"&perPage=1", &result); err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("no keys record for asset %s", assetID)
	}
	return &result.Items[0], nil
}

func (c *pbClient) PatchAsset(id string, fields map[string]any) error {
	return c.patch("/api/collections/assets/records/"+url.PathEscape(id), fields)
}

func (c *pbClient) PatchKeys(id string, fields map[string]any) error {
	return c.patch("/api/collections/keys/records/"+url.PathEscape(id), fields)
}

func (c *pbClient) ListPendingDecommissionCommands() ([]Command, error) {
	var result struct {
		Items []Command `json:"items"`
	}
	filter := url.QueryEscape("(command='decommission'&&status='pending')")
	if err := c.get("/api/collections/commands/records?filter="+filter, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *pbClient) PatchCommand(id string, fields map[string]any) error {
	return c.patch("/api/collections/commands/records/"+url.PathEscape(id), fields)
}
