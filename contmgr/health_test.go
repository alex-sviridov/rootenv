package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func doHealthz(mgr *Contmgr, staleAfter time.Duration) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newHealthMux(mgr, staleAfter).ServeHTTP(rr, req)
	return rr
}

func decodeHealthBody(t *testing.T, rr *httptest.ResponseRecorder) healthResponse {
	t.Helper()
	var resp healthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	return resp
}

// 503 + "starting" before any poll has been recorded.
func TestHealthzStarting(t *testing.T) {
	mgr := &Contmgr{}
	rr := doHealthz(mgr, 30*time.Second)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
	resp := decodeHealthBody(t, rr)
	if resp.Status != "starting" {
		t.Errorf("want status=starting, got %q", resp.Status)
	}
	if resp.LastPollAgo != "" {
		t.Errorf("want empty last_poll_ago before first poll, got %q", resp.LastPollAgo)
	}
}

// 200 + "ok" after a recent poll with PB reachable.
func TestHealthzOK(t *testing.T) {
	mgr := &Contmgr{}
	mgr.RecordPoll(true)

	rr := doHealthz(mgr, 30*time.Second)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	resp := decodeHealthBody(t, rr)
	if resp.Status != "ok" {
		t.Errorf("want status=ok, got %q", resp.Status)
	}
	if !resp.PBConnected {
		t.Error("want pb_connected=true")
	}
	if resp.LastPollAgo == "" {
		t.Error("want non-empty last_poll_ago")
	}
}

// 200 + pb_connected=false when PB was unreachable but the poll loop is current.
func TestHealthzOKWithPBDown(t *testing.T) {
	mgr := &Contmgr{}
	mgr.RecordPoll(false)

	rr := doHealthz(mgr, 30*time.Second)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 (loop running), got %d", rr.Code)
	}
	resp := decodeHealthBody(t, rr)
	if resp.Status != "ok" {
		t.Errorf("want status=ok, got %q", resp.Status)
	}
	if resp.PBConnected {
		t.Error("want pb_connected=false")
	}
}

// 503 + "unhealthy" when the last poll is older than staleAfter.
func TestHealthzStale(t *testing.T) {
	mgr := &Contmgr{}
	mgr.lastPollAt.Store(time.Now().Add(-10 * time.Second).UnixNano())
	mgr.pbHealthy.Store(true)

	rr := doHealthz(mgr, 5*time.Second)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
	resp := decodeHealthBody(t, rr)
	if resp.Status != "unhealthy" {
		t.Errorf("want status=unhealthy, got %q", resp.Status)
	}
	if resp.LastPollAgo == "" {
		t.Error("want non-empty last_poll_ago for stale case")
	}
}

// Content-Type must be application/json on all responses.
func TestHealthzContentType(t *testing.T) {
	mgr := &Contmgr{}
	rr := doHealthz(mgr, 30*time.Second)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("want Content-Type=application/json, got %q", ct)
	}
}

// Non-GET methods must be rejected (Go 1.22 method-based routing).
func TestHealthzMethodNotAllowed(t *testing.T) {
	mgr := &Contmgr{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	newHealthMux(mgr, 30*time.Second).ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405 for POST /healthz, got %d", rr.Code)
	}
}
