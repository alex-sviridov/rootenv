package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

func TestLoadConfig_defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("POCKETBASE_URL", "")
	t.Setenv("LOG_LEVEL", "")

	cfg := loadConfig()

	if cfg.port != "8080" {
		t.Errorf("want port 8080, got %s", cfg.port)
	}
	if cfg.pocketbaseURL != "http://backend:8090" {
		t.Errorf("want default pocketbase URL, got %s", cfg.pocketbaseURL)
	}
}

func TestLoadConfig_envOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("POCKETBASE_URL", "http://pb:1234")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := loadConfig()

	if cfg.port != "9090" {
		t.Errorf("want port 9090, got %s", cfg.port)
	}
	if cfg.pocketbaseURL != "http://pb:1234" {
		t.Errorf("want http://pb:1234, got %s", cfg.pocketbaseURL)
	}
	if cfg.logLevel.String() != "DEBUG" {
		t.Errorf("want DEBUG log level, got %s", cfg.logLevel)
	}
}

func TestLoadConfig_idleTimeout(t *testing.T) {
	t.Setenv("RELAY_IDLE_TIMEOUT", "15m")
	cfg := loadConfig()
	if cfg.idleTimeout != 15*time.Minute {
		t.Errorf("want 15m, got %v", cfg.idleTimeout)
	}
}

func TestLoadConfig_idleTimeout_default(t *testing.T) {
	t.Setenv("RELAY_IDLE_TIMEOUT", "")
	cfg := loadConfig()
	if cfg.idleTimeout != 30*time.Minute {
		t.Errorf("want 30m default, got %v", cfg.idleTimeout)
	}
}

// fakeProvider implements relaybase.HealthzProvider for testing.
type fakeProvider struct {
	ready       bool
	activeConns int
}

func (f *fakeProvider) IsReady() bool          { return f.ready }
func (f *fakeProvider) ActiveConnections() int { return f.activeConns }

func TestHealthz_ready(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	relaybase.HandleHealthz(&fakeProvider{ready: true, activeConns: 3})(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}

	var resp struct {
		Status            string `json:"status"`
		Backend           string `json:"backend"`
		ActiveConnections int    `json:"active_connections"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("want status 'ok', got %q", resp.Status)
	}
	if resp.Backend != "connected" {
		t.Errorf("want backend 'connected', got %q", resp.Backend)
	}
	if resp.ActiveConnections != 3 {
		t.Errorf("want active_connections 3, got %d", resp.ActiveConnections)
	}
}

func TestHealthz_notReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	relaybase.HandleHealthz(&fakeProvider{ready: false})(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}

	var resp struct {
		Status  string `json:"status"`
		Backend string `json:"backend"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "starting" {
		t.Errorf("want status 'starting', got %q", resp.Status)
	}
}
