package pbclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/pbclient"
)

func TestValidateToken_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/collections/users/auth-refresh" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "testtoken" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"record": map[string]any{"id": "usr_abc"}})
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	userID, err := c.ValidateToken("testtoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "usr_abc" {
		t.Errorf("got userID %q, want %q", userID, "usr_abc")
	}
}

func TestValidateToken_unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.ValidateToken("badtoken")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestGetAttempt_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/collections/attempts/records/atm_123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "testtoken" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "atm_123", "user": "usr_abc"})
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	userID, err := c.GetAttempt("testtoken", "atm_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "usr_abc" {
		t.Errorf("got %q, want %q", userID, "usr_abc")
	}
}

func TestGetAttempt_forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.GetAttempt("testtoken", "atm_other")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}
