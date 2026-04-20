package pbclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/relay/pkg/pbclient"
)

// authMiddleware returns 401 if Authorization header doesn't equal expectedToken.
func authMiddleware(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expectedToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newMockServer(t *testing.T, mux *http.ServeMux) (*httptest.Server, *pbclient.Client) {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := pbclient.New(ts.URL, "admin-token")
	return ts, c
}

// ---- ValidateToken ----

func TestValidateToken_valid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":  "new-token",
			"record": map[string]any{"id": "user123"},
		})
	})
	_, c := newMockServer(t, mux)

	userID, err := c.ValidateToken("Bearer valid-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "user123" {
		t.Errorf("want user123, got %s", userID)
	}
}

func TestValidateToken_unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, c := newMockServer(t, mux)

	_, err := c.ValidateToken("Bearer bad-token")
	if err != pbclient.ErrUnauthorized {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestValidateToken_forbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, c := newMockServer(t, mux)

	_, err := c.ValidateToken("Bearer bad-token")
	if err != pbclient.ErrUnauthorized {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

// ---- GetServer ----

func TestGetServer_found(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/api/collections/servers/records/srv1",
		authMiddleware("admin-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(pbclient.Server{
				ID:      "srv1",
				Attempt: "att1",
				Name:    "node1",
				State:   "provisioned",
			})
		})),
	)
	_, c := newMockServer(t, mux)

	s, err := c.GetServer("srv1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != "srv1" || s.Attempt != "att1" {
		t.Errorf("unexpected server: %+v", s)
	}
}

func TestGetServer_notFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/api/collections/servers/records/unknown",
		authMiddleware("admin-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})),
	)
	_, c := newMockServer(t, mux)

	_, err := c.GetServer("unknown")
	if err != pbclient.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ---- GetAttempt ----

func TestGetAttempt_found(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/api/collections/attempts/records/att1",
		authMiddleware("admin-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(pbclient.Attempt{
				ID:   "att1",
				User: "user123",
				Lab:  "lab1",
			})
		})),
	)
	_, c := newMockServer(t, mux)

	a, err := c.GetAttempt("att1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.User != "user123" {
		t.Errorf("want user123, got %s", a.User)
	}
}

func TestGetAttempt_notFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/api/collections/attempts/records/nope",
		authMiddleware("admin-token", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})),
	)
	_, c := newMockServer(t, mux)

	_, err := c.GetAttempt("nope")
	if err != pbclient.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ---- NewWithCredentials ----

func TestNewWithCredentials_success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Identity string `json:"identity"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Identity != "admin@example.com" || body.Password != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "admin-jwt"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c, err := pbclient.NewWithCredentials(ts.URL, "admin@example.com", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewWithCredentials_badPassword(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	_, err := pbclient.NewWithCredentials(ts.URL, "admin@example.com", "wrong")
	if err == nil {
		t.Fatal("expected error")
	}
}
