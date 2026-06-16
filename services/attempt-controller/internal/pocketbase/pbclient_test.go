package pocketbase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewPBClientAuthSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q", c.token)
	}
}

func TestNewPBClientTLSVerifyRejectsSelfSigned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()

	if _, err := NewClient(ts.URL, "svc@x.local", "pass", true); err == nil {
		t.Error("expected TLS verification error for self-signed cert")
	}
}

func TestNewPBClientTLSVerifyDisabledAcceptsSelfSigned(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "tok123" {
		t.Errorf("token = %q", c.token)
	}
}

func TestNewPBClientAuthFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	if _, err := NewClient(ts.URL, "svc@x.local", "wrong", true); err == nil {
		t.Error("expected error")
	}
}

func TestGetReauthsOn401(t *testing.T) {
	var authCalls int
	var recordCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		token := fmt.Sprintf("tok%d", authCalls)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token})
	})
	mux.HandleFunc("/api/collections/attempts/records/a1", func(w http.ResponseWriter, r *http.Request) {
		recordCalls++
		if r.Header.Get("Authorization") != "tok2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(AttemptRecord{ID: "a1"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.token != "tok1" {
		t.Fatalf("token = %q, want tok1", c.token)
	}

	rec, err := c.GetAttempt(context.Background(), "a1")
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if rec.ID != "a1" {
		t.Errorf("rec.ID = %q", rec.ID)
	}
	if c.token != "tok2" {
		t.Errorf("token after reauth = %q, want tok2", c.token)
	}
	if authCalls != 2 {
		t.Errorf("authCalls = %d, want 2", authCalls)
	}
	if recordCalls != 2 {
		t.Errorf("recordCalls = %d, want 2", recordCalls)
	}
}

func TestListActiveAttempts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	mux.HandleFunc("/api/collections/attempts/records", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "tok123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter != "(current_state!=desired_state)" {
			t.Errorf("filter = %q", filter)
		}
		if expand := r.URL.Query().Get("expand"); expand != "lab" {
			t.Errorf("expand = %q", expand)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []AttemptRecord{
				{ID: "a1", UserId: "u1", Lab: "rhcsa-lab1", LabName: "RHCSA Lab 1", CurrentState: "provisioned", DesiredState: "provisioned", ExpiresAt: "2026-06-15T12:00:00Z"},
				{ID: "a2", UserId: "u2", Lab: "rhcsa-lab2", LabName: "RHCSA Lab 2", CurrentState: "new", DesiredState: "provisioned", ExpiresAt: "2026-06-15T13:00:00Z"},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	attempts, err := c.ListActiveAttempts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveAttempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("len(attempts) = %d", len(attempts))
	}
	if attempts[0].ID != "a1" || attempts[1].ID != "a2" {
		t.Errorf("attempts = %+v", attempts)
	}
}

func TestGetAttemptReturnsErrNotFoundOn404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	mux.HandleFunc("/api/collections/attempts/records/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.GetAttempt(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListActiveAttemptsIncludesLabEnvironment(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	mux.HandleFunc("/api/collections/attempts/records", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":            "a1",
					"user":          "u1",
					"lab":           "rhcsa-lab1",
					"lab_name":      "RHCSA Lab 1",
					"current_state": "provisioned",
					"desired_state": "provisioned",
					"expires_at":    "2026-06-15T12:00:00Z",
					"expand": map[string]any{
						"lab": map[string]any{
							"environment": map[string]any{
								"duration": 30,
								"assets":   []map[string]any{{"name": "server-0"}},
							},
						},
					},
				},
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	attempts, err := c.ListActiveAttempts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("len(attempts) = %d", len(attempts))
	}
	if attempts[0].Environment.Duration != 30 {
		t.Errorf("environment duration = %v, want 30", attempts[0].Environment.Duration)
	}
}
