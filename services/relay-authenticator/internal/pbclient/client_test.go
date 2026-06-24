package pbclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/pbclient"
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
		_ = json.NewEncoder(w).Encode(map[string]any{"record": map[string]any{"id": "usr_abc"}})
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

func TestValidateToken_forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.ValidateToken("badtoken")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestValidateToken_unexpected_status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.ValidateToken("tok")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestValidateToken_empty_user_id(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// record.id is missing/empty
		_ = json.NewEncoder(w).Encode(map[string]any{"record": map[string]any{"id": ""}})
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.ValidateToken("tok")
	if err == nil {
		t.Fatal("expected error for empty user id, got nil")
	}
}

func TestValidateToken_malformed_json(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.ValidateToken("tok")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
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
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "atm_123", "user": "usr_abc"})
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

func TestGetAttempt_path_escaped(t *testing.T) {
	// Attempt IDs with special characters must be path-escaped.
	// Go's r.URL.Path decodes percent-encoding, so we check r.URL.RawPath
	// (set by the stdlib when encoding differs from decoded form).
	var gotRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"user": "usr_abc"})
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, _ = c.GetAttempt("tok", "atm/evil")
	// url.PathEscape("atm/evil") → "atm%2Fevil"; the server must see the encoded form
	want := "/api/collections/attempts/records/atm%2Fevil"
	if gotRawPath != want {
		t.Errorf("attempt ID not correctly escaped: got RawPath %q, want %q", gotRawPath, want)
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

func TestGetAttempt_not_found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.GetAttempt("testtoken", "atm_gone")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestGetAttempt_unexpected_status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.GetAttempt("tok", "atm_123")
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestGetAttempt_malformed_json(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := pbclient.New(srv.URL, true)
	_, err := c.GetAttempt("tok", "atm_123")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestNew_base_url_trailing_slash_stripped(t *testing.T) {
	c := pbclient.New("http://pocketbase.example.com/", true)
	if c.BaseURL() != "http://pocketbase.example.com" {
		t.Errorf("trailing slash not stripped: %q", c.BaseURL())
	}
}
