package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok" {
		t.Errorf("want body 'ok', got %q", body)
	}
}
