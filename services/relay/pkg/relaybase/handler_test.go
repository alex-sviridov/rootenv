package relaybase_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
	"github.com/coder/websocket"
)

type fakeBackend struct {
	mu        sync.Mutex
	called    bool
	assetName string
	userID    string
}

func (f *fakeBackend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	f.mu.Lock()
	f.called = true
	f.assetName = assetName
	f.userID = userID
	f.mu.Unlock()
	return nil
}

func (f *fakeBackend) wasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/workstation/", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func makeHandler(fb *fakeBackend, attemptID, ownerID string, authTimeout time.Duration) *relaybase.Handler {
	return &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   attemptID,
		OwnerID:     ownerID,
		AuthTimeout: authTimeout,
	}
}

func TestHandler_success(t *testing.T) {
	fb := &fakeBackend{}
	h := makeHandler(fb, "atm_123", "usr_abc", 2*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte("sometoken")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve was not called")
	}
	fb.mu.Lock()
	if fb.assetName != "workstation" {
		t.Errorf("assetName: got %q, want %q", fb.assetName, "workstation")
	}
	if fb.userID != "usr_abc" {
		t.Errorf("userID: got %q, want %q", fb.userID, "usr_abc")
	}
	fb.mu.Unlock()
}

func TestHandler_wrong_attempt_id(t *testing.T) {
	fb := &fakeBackend{}
	h := makeHandler(fb, "atm_123", "usr_abc", 2*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_WRONG")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if fb.wasCalled() {
		t.Error("Backend.Serve should not have been called")
	}
}

func TestHandler_missing_user_id(t *testing.T) {
	fb := &fakeBackend{}
	h := makeHandler(fb, "atm_123", "usr_abc", 2*time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if fb.wasCalled() {
		t.Error("Backend.Serve should not have been called")
	}
}

func TestHandler_auth_timeout(t *testing.T) {
	fb := &fakeBackend{}
	h := makeHandler(fb, "atm_123", "usr_abc", 50*time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()
	// send nothing — should time out
	time.Sleep(200 * time.Millisecond)
	if fb.wasCalled() {
		t.Error("Backend.Serve should not have been called on timeout")
	}
}

func TestHandler_skip_auth_calls_backend(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		SkipAuth:    true,
		AuthTimeout: 2 * time.Second,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No X-Attempt-Id or X-User-Id headers injected
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte("tok")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve was not called with SkipAuth=true")
	}
	fb.mu.Lock()
	if fb.userID != "anonymous" {
		t.Errorf("userID: got %q, want %q", fb.userID, "anonymous")
	}
	fb.mu.Unlock()
}

func TestHandler_skip_auth_ignores_wrong_attempt_id(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		SkipAuth:    true,
		AuthTimeout: 2 * time.Second,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_WRONG")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve should still be called when SkipAuth=true")
	}
}
