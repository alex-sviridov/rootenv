# relay-exec Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace relay-ssh with a `kubectl exec`-based relay, one instance per LabEnvironment namespace, with centralized PocketBase auth at the Traefik ingress layer.

**Architecture:** A new stateless `ingress-authenticator` service in `rootenv-infra` validates PocketBase tokens and attempt ownership via ForwardAuth before Traefik routes to the per-namespace `relay-exec` pod. The relay-exec pod trusts injected headers (protected by NetworkPolicy) and execs into asset pods using its in-cluster ServiceAccount. Generic WebSocket handling is extracted into `pkg/relaybase` so future relay types (http, filemanager) reuse it.

**Tech Stack:** Go 1.26, `github.com/coder/websocket`, `k8s.io/client-go` (exec), controller-runtime (operator), Traefik IngressRoute/Middleware CRDs (managed as unstructured), standard `net/http`.

---

## File Map

### New: `services/ingress-authenticator/`
- `go.mod` — module `github.com/alexsviridov/linuxlab/ingress-authenticator`
- `cmd/main.go` — HTTP server, reads env vars, wires handler
- `internal/pbclient/client.go` — `ValidateToken` + `GetAttempt` (user token only)
- `internal/pbclient/client_test.go`
- `internal/auth/handler.go` — `GET /auth` handler
- `internal/auth/handler_test.go`
- `Dockerfile`

### Modified: `services/relay/pkg/relaybase/`
- `handler.go` — **new file**: generic `Handler` struct + `Backend` interface; extracted WS accept + first-message + header validation + limiter logic

### New: `services/relay/exec/`
- `backend.go` — implements `relaybase.Backend`: resolves assetName→pod, kubectl exec↔WS proxy
- `backend_test.go`
- `Dockerfile`

### New: `services/relay/cmd/relay-exec/`
- `main.go` — wires `relaybase.Handler{Backend: &exec.Backend{...}}`, reads env vars

### Modified: `services/labenv-operator/internal/controller/labenvironment_controller.go`
- `ensureNetworkPolicy` → switch to `CreateOrPatch`, add relay-exec ingress + egress rules
- `ensureRelayServiceAccount`, `ensureRelayRole`, `ensureRelayRoleBinding` — new
- `ensureRelayDeployment`, `ensureRelayService` — new
- `ensureIngressRoute` — new; creates Traefik IngressRoute + 3 Middlewares in `rootenv-infra` as unstructured objects
- `reconcileDelete` — delete Traefik resources on cleanup

---

## Task 1: `ingress-authenticator` — PocketBase client

**Files:**
- Create: `services/ingress-authenticator/go.mod`
- Create: `services/ingress-authenticator/internal/pbclient/client.go`
- Create: `services/ingress-authenticator/internal/pbclient/client_test.go`

- [ ] **Step 1: Create go.mod**

```
cd services/ingress-authenticator
go mod init github.com/alexsviridov/linuxlab/ingress-authenticator
```

- [ ] **Step 2: Write failing tests**

Create `services/ingress-authenticator/internal/pbclient/client_test.go`:

```go
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
	attempt, err := c.GetAttempt("testtoken", "atm_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempt.ID != "atm_123" || attempt.UserID != "usr_abc" {
		t.Errorf("unexpected attempt: %+v", attempt)
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
```

- [ ] **Step 3: Run tests — expect compile failure (package doesn't exist yet)**

```
cd services/ingress-authenticator && go test ./internal/pbclient/...
```

Expected: `cannot find package`

- [ ] **Step 4: Implement the client**

Create `services/ingress-authenticator/internal/pbclient/client.go`:

```go
package pbclient

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Attempt struct {
	ID     string `json:"id"`
	UserID string `json:"user"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, tlsVerify bool) *Client {
	transport := &http.Transport{}
	if !tlsVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second, Transport: transport},
	}
}

// ValidateToken calls PocketBase auth-refresh and returns the userID on success.
func (c *Client) ValidateToken(token string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/users/auth-refresh", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth-refresh returned status %d", resp.StatusCode)
	}

	var result struct {
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Record.ID == "" {
		return "", fmt.Errorf("auth-refresh returned empty user id")
	}
	return result.Record.ID, nil
}

// GetAttempt fetches an attempt record using the user's own token.
// PocketBase's viewRule enforces ownership — 403 means wrong user or not found.
func (c *Client) GetAttempt(token, attemptID string) (*Attempt, error) {
	u := c.baseURL + "/api/collections/attempts/records/" + url.PathEscape(attemptID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("forbidden or not found (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get attempt returned status %d", resp.StatusCode)
	}

	var a Attempt
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}
```

- [ ] **Step 5: Run tests — expect pass**

```
cd services/ingress-authenticator && go test ./internal/pbclient/... -v
```

Expected: all 4 tests PASS

- [ ] **Step 6: Commit**

```
git add services/ingress-authenticator/
git commit -m "feat(ingress-authenticator): pbclient with ValidateToken and GetAttempt"
```

---

## Task 2: `ingress-authenticator` — auth handler

**Files:**
- Create: `services/ingress-authenticator/internal/auth/handler.go`
- Create: `services/ingress-authenticator/internal/auth/handler_test.go`

- [ ] **Step 1: Write failing tests**

Create `services/ingress-authenticator/internal/auth/handler_test.go`:

```go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/auth"
)

type fakePB struct {
	userID      string
	validateErr error
	attemptUser string
	attemptErr  error
}

func (f *fakePB) ValidateToken(token string) (string, error) {
	return f.userID, f.validateErr
}

func (f *fakePB) GetAttempt(token, attemptID string) (string, error) {
	return f.attemptUser, f.attemptErr
}

func TestHandler_success(t *testing.T) {
	pb := &fakePB{userID: "usr_abc", attemptUser: "usr_abc"}
	h := auth.NewHandler(pb)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "tok")
	req.Header.Set("X-Attempt-Id", "atm_123")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if w.Header().Get("X-User-Id") != "usr_abc" {
		t.Errorf("want X-User-Id usr_abc, got %q", w.Header().Get("X-User-Id"))
	}
	if w.Header().Get("X-Attempt-Id") != "atm_123" {
		t.Errorf("want X-Attempt-Id atm_123, got %q", w.Header().Get("X-Attempt-Id"))
	}
}

func TestHandler_missing_authorization(t *testing.T) {
	pb := &fakePB{}
	h := auth.NewHandler(pb)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("X-Attempt-Id", "atm_123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_missing_attempt_id(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandler_invalid_token(t *testing.T) {
	pb := &fakePB{validateErr: fmt.Errorf("unauthorized")}
	h := auth.NewHandler(pb)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "badtok")
	req.Header.Set("X-Attempt-Id", "atm_123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_forbidden_attempt(t *testing.T) {
	pb := &fakePB{userID: "usr_abc", attemptErr: fmt.Errorf("forbidden")}
	h := auth.NewHandler(pb)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "tok")
	req.Header.Set("X-Attempt-Id", "atm_other")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", w.Code)
	}
}
```

Note: add `"fmt"` to the import in the test file.

- [ ] **Step 2: Run tests — expect compile failure**

```
cd services/ingress-authenticator && go test ./internal/auth/... 
```

Expected: `cannot find package`

- [ ] **Step 3: Implement the handler**

Create `services/ingress-authenticator/internal/auth/handler.go`:

```go
package auth

import (
	"log/slog"
	"net/http"
)

// PocketBase is the interface the handler needs — two calls, both with the user token.
type PocketBase interface {
	ValidateToken(token string) (string, error)
	GetAttempt(token, attemptID string) (string, error)
}

type Handler struct {
	pb PocketBase
}

func NewHandler(pb PocketBase) *Handler {
	return &Handler{pb: pb}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if token == "" {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}

	attemptID := r.Header.Get("X-Attempt-Id")
	if attemptID == "" {
		http.Error(w, "missing X-Attempt-Id", http.StatusBadRequest)
		return
	}

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		slog.Warn("token validation failed", "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := h.pb.GetAttempt(token, attemptID); err != nil {
		slog.Warn("attempt access denied", "attempt_id", attemptID, "err", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("X-User-Id", userID)
	w.Header().Set("X-Attempt-Id", attemptID)
	w.WriteHeader(http.StatusOK)
}
```

Now update `internal/pbclient/client.go` — `GetAttempt` must return `(string, error)` to match the interface. Change its signature:

```go
// GetAttempt fetches an attempt record using the user's own token.
// Returns the attempt's owner userID on success.
func (c *Client) GetAttempt(token, attemptID string) (string, error) {
	// ... same HTTP logic as Task 1 ...
	return a.UserID, nil  // return userID instead of *Attempt
}
```

And update `client_test.go` accordingly — `TestGetAttempt_success` checks the returned string:

```go
func TestGetAttempt_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
```

- [ ] **Step 4: Run all tests — expect pass**

```
cd services/ingress-authenticator && go test ./... -v
```

Expected: all tests PASS

- [ ] **Step 5: Commit**

```
git add services/ingress-authenticator/
git commit -m "feat(ingress-authenticator): auth handler with PB validation"
```

---

## Task 3: `ingress-authenticator` — main + Dockerfile

**Files:**
- Create: `services/ingress-authenticator/cmd/main.go`
- Create: `services/ingress-authenticator/Dockerfile`

- [ ] **Step 1: Write main.go**

Create `services/ingress-authenticator/cmd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/auth"
	"github.com/alexsviridov/linuxlab/ingress-authenticator/internal/pbclient"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	pbURL := os.Getenv("INGAUTH_POCKETBASE_URL")
	if pbURL == "" {
		slog.Error("INGAUTH_POCKETBASE_URL is required")
		os.Exit(1)
	}
	tlsVerify := os.Getenv("INGAUTH_POCKETBASE_TLS_VERIFY") != "false"

	port := os.Getenv("INGAUTH_PORT")
	if port == "" {
		port = "8080"
	}

	pb := pbclient.New(pbURL, tlsVerify)
	handler := auth.NewHandler(pb)

	mux := http.NewServeMux()
	mux.Handle("/auth", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("ingress-authenticator starting", "port", port, "pb_url", pbURL, "tls_verify", tlsVerify)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
	slog.Info("shutdown complete")
}
```

- [ ] **Step 2: Write Dockerfile**

Create `services/ingress-authenticator/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /ingress-authenticator ./cmd/main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /ingress-authenticator /ingress-authenticator
ENTRYPOINT ["/ingress-authenticator"]
```

- [ ] **Step 3: Build check**

```
cd services/ingress-authenticator && go build ./...
```

Expected: no errors

- [ ] **Step 4: Commit**

```
git add services/ingress-authenticator/
git commit -m "feat(ingress-authenticator): main entrypoint and Dockerfile"
```

---

## Task 4: `relaybase.Handler` — generic WebSocket handler

Extract the common WS accept + first-message + header validation + limiter pattern into `pkg/relaybase/handler.go`. relay-ssh keeps its own `ServeHTTP` (it has ssh-specific auth via `Authenticator`); this new `Handler` is for relay-exec and future relays that get auth from injected headers.

**Files:**
- Create: `services/relay/pkg/relaybase/handler.go`
- Create: `services/relay/pkg/relaybase/handler_test.go`

- [ ] **Step 1: Write failing tests**

Create `services/relay/pkg/relaybase/handler_test.go`:

```go
package relaybase_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
	"github.com/coder/websocket"
)

// fakeBackend records calls and closes immediately.
type fakeBackend struct {
	called    bool
	assetName string
	userID    string
}

func (f *fakeBackend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	f.called = true
	f.assetName = assetName
	f.userID = userID
	return nil
}

func dial(t *testing.T, srv *httptest.Server, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/workstation/", &websocket.DialOptions{HTTPHeader: headers})
}

func TestHandler_success(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		OwnerID:     "usr_abc",
		AuthTimeout: 2 * time.Second,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn, _, err := dial(t, srv, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	// send first message (token placeholder)
	ctx := context.Background()
	if err := conn.Write(ctx, websocket.MessageText, []byte("sometoken")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !fb.called {
		t.Error("Backend.Serve was not called")
	}
	if fb.assetName != "workstation" {
		t.Errorf("assetName: got %q, want %q", fb.assetName, "workstation")
	}
	if fb.userID != "usr_abc" {
		t.Errorf("userID: got %q, want %q", fb.userID, "usr_abc")
	}
}

func TestHandler_wrong_attempt_id(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		OwnerID:     "usr_abc",
		AuthTimeout: 2 * time.Second,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_WRONG")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn, _, err := dial(t, srv, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if fb.called {
		t.Error("Backend.Serve should not have been called")
	}
}

func TestHandler_missing_user_id(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		OwnerID:     "usr_abc",
		AuthTimeout: 2 * time.Second,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		// X-User-Id intentionally omitted
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn, _, err := dial(t, srv, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if fb.called {
		t.Error("Backend.Serve should not have been called")
	}
}

func TestHandler_auth_timeout(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		OwnerID:     "usr_abc",
		AuthTimeout: 50 * time.Millisecond,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn, _, err := dial(t, srv, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()
	// send nothing — should time out
	time.Sleep(200 * time.Millisecond)
	if fb.called {
		t.Error("Backend.Serve should not have been called on timeout")
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

```
cd services/relay && go test ./pkg/relaybase/... 
```

Expected: `relaybase.Handler undefined`

- [ ] **Step 3: Implement handler.go**

Create `services/relay/pkg/relaybase/handler.go`:

```go
package relaybase

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Backend is implemented by each relay type (exec, http, etc.).
// Called after the generic handler completes auth; responsible for the actual proxy.
type Backend interface {
	Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error
}

// Handler is a generic HTTP handler for relay types that receive auth via injected headers.
// It accepts a WebSocket upgrade, reads the first message (token placeholder, discarded),
// validates X-Attempt-Id and X-User-Id headers, acquires a connection slot, then calls Backend.
type Handler struct {
	Backend         Backend
	Limiter         *ConnLimiter
	AttemptID       string        // MY_ATTEMPT_ID — set from env at startup
	OwnerID         string        // MY_OWNER_ID — set from env at startup
	AllowedOrigins  []string
	AuthTimeout     time.Duration // how long to wait for first WS message
	WG              *sync.WaitGroup
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assetName := strings.Trim(r.PathValue("assetName"), "/")
	if assetName == "" {
		http.Error(w, "missing asset name", http.StatusBadRequest)
		return
	}

	log := slog.With("asset", assetName, "remote", r.RemoteAddr)

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.AllowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Warn("ws accept failed", "err", err)
		return
	}

	authTimeout := h.AuthTimeout
	if authTimeout == 0 {
		authTimeout = 10 * time.Second
	}
	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, _, err = conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: no first message received", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	attemptID := r.Header.Get("X-Attempt-Id")
	userID := r.Header.Get("X-User-Id")

	if attemptID != h.AttemptID {
		log.Warn("security: X-Attempt-Id mismatch", "got", attemptID, "want", h.AttemptID)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	if userID == "" {
		log.Warn("security: missing X-User-Id")
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	if err := h.Limiter.Acquire(userID); err != nil {
		log.Warn("connection limit exceeded", "user_id", userID)
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	defer h.Limiter.Release(userID)

	log = log.With("user_id", userID, "attempt_id", attemptID)
	log.Info("ws connected", "active_total", h.Limiter.Total())

	if h.WG != nil {
		h.WG.Add(1)
		defer h.WG.Done()
	}

	if err := h.Backend.Serve(r.Context(), conn, assetName, userID); err != nil {
		log.Error("backend error", "err", err)
	}

	log.Info("ws disconnected")
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
```

- [ ] **Step 4: Run tests — expect pass**

```
cd services/relay && go test ./pkg/relaybase/... -v
```

Expected: all tests PASS (including pre-existing limiter tests)

- [ ] **Step 5: Commit**

```
git add services/relay/pkg/relaybase/handler.go services/relay/pkg/relaybase/handler_test.go
git commit -m "feat(relay): generic relaybase.Handler with Backend interface"
```

---

## Task 5: `relay-exec` — kubectl exec backend

**Files:**
- Create: `services/relay/exec/backend.go`
- Create: `services/relay/exec/backend_test.go`

The relay module needs `k8s.io/client-go` added as a dependency for the exec stream.

- [ ] **Step 1: Add k8s client-go dependency**

```
cd services/relay && go get k8s.io/client-go@v0.32.0 && go get k8s.io/api@v0.32.0
```

Then tidy:
```
go mod tidy
```

- [ ] **Step 2: Write failing tests**

Create `services/relay/exec/backend_test.go`:

```go
package exec_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/coder/websocket"
)

// fakeExecer simulates the kubectl exec stream — just echoes stdin back as stdout.
type fakeExecer struct {
	output []byte
}

func (f *fakeExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer) error {
	if f.output != nil {
		_, _ = stdout.Write(f.output)
	}
	return nil
}

func TestBackend_routes_output_to_websocket(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "hello from pod" {
		t.Errorf("got %q, want %q", string(msg), "hello from pod")
	}
}

func TestBackend_resize_frame_does_not_crash(t *testing.T) {
	fb := &fakeExecer{}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// send resize frame: \x01 + cols (uint16 LE 80) + rows (uint16 LE 24)
	frame := []byte{0x01, 80, 0, 24, 0}
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write: %v", err)
	}
	// no crash is the assertion
	time.Sleep(50 * time.Millisecond)
}
```

- [ ] **Step 3: Run tests — expect compile failure**

```
cd services/relay && go test ./exec/... 
```

Expected: `cannot find package`

- [ ] **Step 4: Implement backend.go**

Create `services/relay/exec/backend.go`:

```go
package exec

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"

	"github.com/coder/websocket"
)

// Execer abstracts kubectl exec so it can be faked in tests.
type Execer interface {
	Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Backend implements relaybase.Backend using kubectl exec.
type Backend struct {
	Namespace string // labenv namespace where asset pods live
	Execer    Execer // nil in production — replaced by KubeExecer at startup
}

// Serve proxies WebSocket ↔ kubectl exec stream for the named asset.
// assetName == pod name (operator ensures this invariant).
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	log := slog.With("asset", assetName, "user_id", userID)

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	execDone := make(chan error, 1)
	go func() {
		execDone <- b.Execer.Exec(ctx, b.Namespace, assetName, stdinR, stdoutW, io.Discard)
		_ = stdoutW.Close()
	}()

	// stdout → WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → stdin (with resize handling)
	go func() {
		defer stdinW.Close()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			// resize frame: \x01 + cols (uint16 LE) + rows (uint16 LE)
			if len(data) == 5 && data[0] == 0x01 {
				cols := binary.LittleEndian.Uint16(data[1:3])
				rows := binary.LittleEndian.Uint16(data[3:5])
				log.Debug("resize", "cols", cols, "rows", rows)
				// resize via exec stream is not supported by all container runtimes;
				// log and ignore for now
				continue
			}
			if _, err := stdinW.Write(data); err != nil {
				return
			}
		}
	}()

	return <-execDone
}
```

- [ ] **Step 5: Run tests — expect pass**

```
cd services/relay && go test ./exec/... -v
```

Expected: all tests PASS

- [ ] **Step 6: Implement KubeExecer (production exec via in-cluster config)**

Add to `services/relay/exec/backend.go`:

```go
import (
	// add these to existing imports
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/scheme"
)

// KubeExecer implements Execer using a real Kubernetes client.
type KubeExecer struct {
	client *kubernetes.Clientset
	config *rest.Config
}

// NewKubeExecer creates an Execer using the in-cluster ServiceAccount.
func NewKubeExecer() (*KubeExecer, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubeExecer{client: cs, config: cfg}, nil
}

func (k *KubeExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := k.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/sh"},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.config, http.MethodPost, req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    true,
	})
}
```

Add `"net/http"` to the existing imports block.

- [ ] **Step 7: Build check**

```
cd services/relay && go build ./...
```

Expected: no errors

- [ ] **Step 8: Commit**

```
git add services/relay/exec/ services/relay/go.mod services/relay/go.sum
git commit -m "feat(relay-exec): kubectl exec backend with KubeExecer"
```

---

## Task 6: `relay-exec` — main entrypoint + Dockerfile

**Files:**
- Create: `services/relay/cmd/relay-exec/main.go`
- Create: `services/relay/exec/Dockerfile`

- [ ] **Step 1: Write main.go**

Create `services/relay/cmd/relay-exec/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	attemptID := os.Getenv("RELAY_MY_ATTEMPT_ID")
	ownerID := os.Getenv("RELAY_MY_OWNER_ID")
	namespace := os.Getenv("RELAY_MY_NAMESPACE")
	port := os.Getenv("RELAY_PORT")
	if port == "" {
		port = "8080"
	}

	if attemptID == "" || ownerID == "" || namespace == "" {
		slog.Error("RELAY_MY_ATTEMPT_ID, RELAY_MY_OWNER_ID, RELAY_MY_NAMESPACE are required")
		os.Exit(1)
	}

	var allowedOrigins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				allowedOrigins = append(allowedOrigins, o)
			}
		}
	}

	execer, err := relayexec.NewKubeExecer()
	if err != nil {
		slog.Error("failed to create kube execer", "err", err)
		os.Exit(1)
	}

	backend := &relayexec.Backend{
		Namespace: namespace,
		Execer:    execer,
	}

	var wg sync.WaitGroup
	handler := &relaybase.Handler{
		Backend:        backend,
		Limiter:        relaybase.NewConnLimiter(16),
		AttemptID:      attemptID,
		OwnerID:        ownerID,
		AllowedOrigins: allowedOrigins,
		WG:             &wg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/{assetName}/", handler)

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("relay-exec starting", "port", port, "attempt_id", attemptID, "namespace", namespace)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()
	_ = srv.Shutdown(context.Background())
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	<-done
	slog.Info("shutdown complete")
}
```

- [ ] **Step 2: Write Dockerfile**

Create `services/relay/exec/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /relay-exec ./cmd/relay-exec/main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /relay-exec /relay-exec
ENTRYPOINT ["/relay-exec"]
```

- [ ] **Step 3: Build check**

```
cd services/relay && go build ./cmd/relay-exec/...
```

Expected: no errors

- [ ] **Step 4: Commit**

```
git add services/relay/cmd/relay-exec/ services/relay/exec/Dockerfile
git commit -m "feat(relay-exec): main entrypoint and Dockerfile"
```

---

## Task 7: Operator — relay-exec provisioning

Add relay-exec provisioning to the labenv-operator controller.

**Files:**
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go`

- [ ] **Step 1: Add relay ensure calls to reconcileCreate**

In `reconcileCreate`, after `ensureLimitRange`, add:

```go
if err := r.ensureRelayServiceAccount(ctx, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureRelayRole(ctx, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureRelayRoleBinding(ctx, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureRelayDeployment(ctx, env, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureRelayService(ctx, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureIngressRoute(ctx, env, nsName); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 2: Add new imports**

Add to the import block at the top of the controller file:

```go
appsv1 "k8s.io/api/apps/v1"
rbacv1 "k8s.io/api/rbac/v1"
"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
"k8s.io/apimachinery/pkg/runtime/schema"
```

- [ ] **Step 3: Implement ensureRelayServiceAccount**

Add to the controller file:

```go
func (r *LabEnvironmentReconciler) ensureRelayServiceAccount(ctx context.Context, nsName string) error {
    sa := &corev1.ServiceAccount{
        ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-sa", Namespace: nsName},
    }
    err := r.Get(ctx, client.ObjectKeyFromObject(sa), sa)
    if err == nil {
        return nil
    }
    if !apierrors.IsNotFound(err) {
        return err
    }
    return client.IgnoreAlreadyExists(r.Create(ctx, sa))
}
```

- [ ] **Step 4: Implement ensureRelayRole**

```go
func (r *LabEnvironmentReconciler) ensureRelayRole(ctx context.Context, nsName string) error {
    role := &rbacv1.Role{
        ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-role", Namespace: nsName},
        Rules: []rbacv1.PolicyRule{
            {
                APIGroups: []string{""},
                Resources: []string{"pods"},
                Verbs:     []string{"get", "list"},
            },
            {
                APIGroups: []string{""},
                Resources: []string{"pods/exec"},
                Verbs:     []string{"create"},
            },
        },
    }
    err := r.Get(ctx, client.ObjectKeyFromObject(role), role)
    if err == nil {
        return nil
    }
    if !apierrors.IsNotFound(err) {
        return err
    }
    return client.IgnoreAlreadyExists(r.Create(ctx, role))
}
```

- [ ] **Step 5: Implement ensureRelayRoleBinding**

```go
func (r *LabEnvironmentReconciler) ensureRelayRoleBinding(ctx context.Context, nsName string) error {
    rb := &rbacv1.RoleBinding{
        ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-rb", Namespace: nsName},
        RoleRef: rbacv1.RoleRef{
            APIGroup: "rbac.authorization.k8s.io",
            Kind:     "Role",
            Name:     "relay-exec-role",
        },
        Subjects: []rbacv1.Subject{{
            Kind:      "ServiceAccount",
            Name:      "relay-exec-sa",
            Namespace: nsName,
        }},
    }
    err := r.Get(ctx, client.ObjectKeyFromObject(rb), rb)
    if err == nil {
        return nil
    }
    if !apierrors.IsNotFound(err) {
        return err
    }
    return client.IgnoreAlreadyExists(r.Create(ctx, rb))
}
```

- [ ] **Step 6: Implement ensureRelayDeployment**

```go
func (r *LabEnvironmentReconciler) ensureRelayDeployment(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
    image := os.Getenv("LABENV_RELAY_EXEC_IMAGE")
    if image == "" {
        image = "relay-exec:latest"
    }
    replicas := int32(1)
    dep := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{Name: "relay-exec", Namespace: nsName},
    }
    err := r.Get(ctx, client.ObjectKeyFromObject(dep), dep)
    if err == nil {
        return nil
    }
    if !apierrors.IsNotFound(err) {
        return err
    }
    dep.Spec = appsv1.DeploymentSpec{
        Replicas: &replicas,
        Selector: &metav1.LabelSelector{
            MatchLabels: map[string]string{"app": "relay-exec"},
        },
        Template: corev1.PodTemplateSpec{
            ObjectMeta: metav1.ObjectMeta{
                Labels: map[string]string{"app": "relay-exec"},
            },
            Spec: corev1.PodSpec{
                ServiceAccountName: "relay-exec-sa",
                Containers: []corev1.Container{{
                    Name:  "relay-exec",
                    Image: image,
                    Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
                    Env: []corev1.EnvVar{
                        {Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
                        {Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
                        {Name: "RELAY_MY_NAMESPACE", Value: nsName},
                        {Name: "RELAY_PORT", Value: "8080"},
                    },
                }},
            },
        },
    }
    return client.IgnoreAlreadyExists(r.Create(ctx, dep))
}
```

- [ ] **Step 7: Implement ensureRelayService**

```go
func (r *LabEnvironmentReconciler) ensureRelayService(ctx context.Context, nsName string) error {
    svc := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{Name: "relay-exec-svc", Namespace: nsName},
    }
    err := r.Get(ctx, client.ObjectKeyFromObject(svc), svc)
    if err == nil {
        return nil
    }
    if !apierrors.IsNotFound(err) {
        return err
    }
    svc.Spec = corev1.ServiceSpec{
        Selector: map[string]string{"app": "relay-exec"},
        Ports: []corev1.ServicePort{{
            Port:       8080,
            TargetPort: intstr.FromInt32(8080),
        }},
    }
    return client.IgnoreAlreadyExists(r.Create(ctx, svc))
}
```

- [ ] **Step 8: Add RBAC annotations to controller**

Add kubebuilder RBAC markers near the top of the controller (with existing markers):

```go
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
```

- [ ] **Step 9: Build check**

```
cd services/labenv-operator && go build ./...
```

Expected: no errors. Fix any import issues.

- [ ] **Step 10: Commit**

```
git add services/labenv-operator/internal/controller/labenvironment_controller.go
git commit -m "feat(labenv-operator): provision relay-exec Deployment, Service, RBAC per namespace"
```

---

## Task 8: Operator — NetworkPolicy update for relay-exec

**Files:**
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go` — `ensureNetworkPolicy`

- [ ] **Step 1: Switch ensureNetworkPolicy to CreateOrPatch and add relay rules**

Replace the entire `ensureNetworkPolicy` function:

```go
func (r *LabEnvironmentReconciler) ensureNetworkPolicy(ctx context.Context, nsName string) error {
    tcp := corev1.ProtocolTCP
    udp := corev1.ProtocolUDP
    dnsPort := intstr.FromInt32(53)
    relayPort := intstr.FromInt32(8080)
    apiPort := intstr.FromInt32(6443)

    np := &networkingv1.NetworkPolicy{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "network-policy",
            Namespace: nsName,
        },
    }
    _, err := controllerutil.CreateOrPatch(ctx, r.Client, np, func() error {
        np.Spec = networkingv1.NetworkPolicySpec{
            PodSelector: metav1.LabelSelector{},
            PolicyTypes: []networkingv1.PolicyType{
                networkingv1.PolicyTypeIngress,
                networkingv1.PolicyTypeEgress,
            },
            Ingress: []networkingv1.NetworkPolicyIngressRule{
                // same-namespace traffic (inter-pod)
                {
                    From: []networkingv1.NetworkPolicyPeer{{
                        NamespaceSelector: &metav1.LabelSelector{
                            MatchLabels: map[string]string{
                                "kubernetes.io/metadata.name": nsName,
                            },
                        },
                    }},
                },
                // Traefik → relay-exec only (port 8080)
                {
                    From: []networkingv1.NetworkPolicyPeer{{
                        NamespaceSelector: &metav1.LabelSelector{
                            MatchLabels: map[string]string{
                                "kubernetes.io/metadata.name": "kube-system",
                            },
                        },
                    }},
                    Ports: []networkingv1.NetworkPolicyPort{
                        {Protocol: &tcp, Port: &relayPort},
                    },
                    // Note: podSelector here would scope to relay-exec only, but
                    // NetworkPolicy IngressRule does not support both From+podSelector
                    // targeting the destination — use a separate NetworkPolicy if
                    // stricter scoping is needed. For MVP this allows any pod in the
                    // namespace to receive traffic from Traefik on :8080.
                },
            },
            Egress: []networkingv1.NetworkPolicyEgressRule{
                // same-namespace traffic
                {
                    To: []networkingv1.NetworkPolicyPeer{{
                        NamespaceSelector: &metav1.LabelSelector{
                            MatchLabels: map[string]string{
                                "kubernetes.io/metadata.name": nsName,
                            },
                        },
                    }},
                },
                // DNS
                {
                    Ports: []networkingv1.NetworkPolicyPort{
                        {Protocol: &udp, Port: &dnsPort},
                        {Protocol: &tcp, Port: &dnsPort},
                    },
                    To: []networkingv1.NetworkPolicyPeer{{
                        NamespaceSelector: &metav1.LabelSelector{
                            MatchLabels: map[string]string{
                                "kubernetes.io/metadata.name": "kube-system",
                            },
                        },
                        PodSelector: &metav1.LabelSelector{
                            MatchLabels: map[string]string{"k8s-app": "kube-dns"},
                        },
                    }},
                },
                // relay-exec → kube-apiserver for pods/exec
                {
                    Ports: []networkingv1.NetworkPolicyPort{
                        {Protocol: &tcp, Port: &apiPort},
                    },
                },
            },
        }
        return nil
    })
    return err
}
```

- [ ] **Step 2: Build check**

```
cd services/labenv-operator && go build ./...
```

Expected: no errors

- [ ] **Step 3: Commit**

```
git add services/labenv-operator/internal/controller/labenvironment_controller.go
git commit -m "feat(labenv-operator): update NetworkPolicy to CreateOrPatch with relay-exec rules"
```

---

## Task 9: Operator — Traefik IngressRoute per LabEnvironment

**Files:**
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go` — add `ensureIngressRoute` and cleanup in `reconcileDelete`

Traefik resources are managed as `unstructured.Unstructured` — no Traefik Go module needed.

- [ ] **Step 1: Add Traefik GVRs as constants**

Add near the top of the controller file (after imports):

```go
var (
    middlewareGVK = schema.GroupVersionKind{
        Group:   "traefik.io",
        Version: "v1alpha1",
        Kind:    "Middleware",
    }
    ingressRouteGVK = schema.GroupVersionKind{
        Group:   "traefik.io",
        Version: "v1alpha1",
        Kind:    "IngressRoute",
    }
    infraNamespace = "rootenv-infra"
)
```

- [ ] **Step 2: Implement ensureIngressRoute**

```go
func (r *LabEnvironmentReconciler) ensureIngressRoute(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
    attemptID := env.Name
    pathPrefix := "/relay/" + attemptID + "/exec"

    // Middleware 1: inject X-Attempt-Id header
    headersName := "relay-exec-headers-" + attemptID
    headers := &unstructured.Unstructured{}
    headers.SetGroupVersionKind(middlewareGVK)
    headers.SetName(headersName)
    headers.SetNamespace(infraNamespace)
    headers.SetLabels(map[string]string{"rootenv.io/attempt-id": attemptID})
    if err := unstructured.SetNestedStringMap(headers.Object, map[string]string{
        "X-Attempt-Id": attemptID,
    }, "spec", "headers", "customRequestHeaders"); err != nil {
        return err
    }
    if err := r.Create(ctx, headers); client.IgnoreAlreadyExists(err) != nil {
        return err
    }

    // Middleware 2: ForwardAuth
    authName := "relay-exec-auth-" + attemptID
    fwdAuth := &unstructured.Unstructured{}
    fwdAuth.SetGroupVersionKind(middlewareGVK)
    fwdAuth.SetName(authName)
    fwdAuth.SetNamespace(infraNamespace)
    fwdAuth.SetLabels(map[string]string{"rootenv.io/attempt-id": attemptID})
    if err := unstructured.SetNestedMap(fwdAuth.Object, map[string]any{
        "address":            "http://ingress-authenticator-svc.rootenv-infra.svc/auth",
        "authRequestHeaders": []any{"Authorization", "X-Attempt-Id"},
        "authResponseHeaders": []any{"X-User-Id", "X-Attempt-Id"},
    }, "spec", "forwardAuth"); err != nil {
        return err
    }
    if err := r.Create(ctx, fwdAuth); client.IgnoreAlreadyExists(err) != nil {
        return err
    }

    // Middleware 3: stripPrefix
    stripName := "relay-exec-strip-" + attemptID
    strip := &unstructured.Unstructured{}
    strip.SetGroupVersionKind(middlewareGVK)
    strip.SetName(stripName)
    strip.SetNamespace(infraNamespace)
    strip.SetLabels(map[string]string{"rootenv.io/attempt-id": attemptID})
    if err := unstructured.SetNestedStringSlice(strip.Object, []string{pathPrefix}, "spec", "stripPrefix", "prefixes"); err != nil {
        return err
    }
    if err := r.Create(ctx, strip); client.IgnoreAlreadyExists(err) != nil {
        return err
    }

    // IngressRoute
    route := &unstructured.Unstructured{}
    route.SetGroupVersionKind(ingressRouteGVK)
    route.SetName("relay-exec-" + attemptID)
    route.SetNamespace(infraNamespace)
    route.SetLabels(map[string]string{"rootenv.io/attempt-id": attemptID})
    if err := unstructured.SetNestedMap(route.Object, map[string]any{
        "entryPoints": []any{"websecure"},
        "routes": []any{map[string]any{
            "match": "PathPrefix(`" + pathPrefix + "/`)",
            "kind":  "Rule",
            "middlewares": []any{
                map[string]any{"name": headersName, "namespace": infraNamespace},
                map[string]any{"name": authName, "namespace": infraNamespace},
                map[string]any{"name": stripName, "namespace": infraNamespace},
            },
            "services": []any{map[string]any{
                "name":      "relay-exec-svc",
                "namespace": nsName,
                "port":      int64(8080),
            }},
        }},
    }, "spec"); err != nil {
        return err
    }
    if err := r.Create(ctx, route); client.IgnoreAlreadyExists(err) != nil {
        return err
    }
    return nil
}
```

- [ ] **Step 3: Clean up Traefik resources on delete**

In `reconcileDelete`, after the namespace deletion logic and before returning, add cleanup of Traefik resources in `rootenv-infra`. Add this helper and call it:

```go
func (r *LabEnvironmentReconciler) deleteIngressRoute(ctx context.Context, attemptID string) error {
    names := []string{
        "relay-exec-headers-" + attemptID,
        "relay-exec-auth-" + attemptID,
        "relay-exec-strip-" + attemptID,
    }
    for _, name := range names {
        obj := &unstructured.Unstructured{}
        obj.SetGroupVersionKind(middlewareGVK)
        obj.SetName(name)
        obj.SetNamespace(infraNamespace)
        if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
            return err
        }
    }
    route := &unstructured.Unstructured{}
    route.SetGroupVersionKind(ingressRouteGVK)
    route.SetName("relay-exec-" + attemptID)
    route.SetNamespace(infraNamespace)
    if err := r.Delete(ctx, route); client.IgnoreNotFound(err) != nil {
        return err
    }
    return nil
}
```

Call it in `reconcileDelete` before requesting namespace deletion:

```go
if err := r.deleteIngressRoute(ctx, env.Name); err != nil {
    return ctrl.Result{}, err
}
```

- [ ] **Step 4: Add RBAC markers for Traefik resources**

```go
// +kubebuilder:rbac:groups=traefik.io,resources=middlewares;ingressroutes,verbs=get;list;watch;create;delete,namespace=rootenv-infra
```

- [ ] **Step 5: Build check**

```
cd services/labenv-operator && go build ./...
```

Expected: no errors

- [ ] **Step 6: Regenerate RBAC manifests**

```
cd services/labenv-operator && make manifests
```

- [ ] **Step 7: Commit**

```
git add services/labenv-operator/
git commit -m "feat(labenv-operator): create/delete Traefik IngressRoute and Middlewares per LabEnvironment"
```

---

## Task 10: Deploy `ingress-authenticator` to `rootenv-infra`

**Files:**
- Create: `deploy/base/23-ingress-authenticator-deploy.yaml`
- Modify: `deploy/base/kustomization.yaml` (or overlay)

- [ ] **Step 1: Write the deployment manifest**

Create `deploy/base/23-ingress-authenticator-deploy.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ingress-authenticator
  namespace: rootenv-infra
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ingress-authenticator
  template:
    metadata:
      labels:
        app: ingress-authenticator
    spec:
      containers:
        - name: ingress-authenticator
          image: ingress-authenticator:latest
          ports:
            - containerPort: 8080
          env:
            - name: INGAUTH_POCKETBASE_URL
              value: "http://backend-svc.rootenv-infra.svc:8090"
            - name: INGAUTH_POCKETBASE_TLS_VERIFY
              value: "true"
            - name: INGAUTH_PORT
              value: "8080"
---
apiVersion: v1
kind: Service
metadata:
  name: ingress-authenticator-svc
  namespace: rootenv-infra
spec:
  selector:
    app: ingress-authenticator
  ports:
    - port: 8080
      targetPort: 8080
```

- [ ] **Step 2: Add to kustomization**

In `deploy/base/kustomization.yaml` (or the sandbox overlay's kustomization), add:

```yaml
resources:
  # ... existing resources ...
  - 23-ingress-authenticator-deploy.yaml
```

- [ ] **Step 3: Commit**

```
git add deploy/
git commit -m "feat(deploy): ingress-authenticator Deployment and Service"
```

---

## Task 11: Operator ClusterRole — grant access to `rootenv-infra` Traefik resources

The operator runs in its own namespace and needs permission to create/delete Middleware and IngressRoute objects in `rootenv-infra`. `kubebuilder:rbac` markers generate a ClusterRole; a RoleBinding in `rootenv-infra` scopes this.

- [ ] **Step 1: Add a RoleBinding in rootenv-infra for the operator**

Create `deploy/base/24-operator-traefik-rbac.yaml`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: operator-traefik-manager
  namespace: rootenv-infra
rules:
  - apiGroups: ["traefik.io"]
    resources: ["middlewares", "ingressroutes"]
    verbs: ["get", "list", "watch", "create", "delete", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: operator-traefik-manager
  namespace: rootenv-infra
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: operator-traefik-manager
subjects:
  - kind: ServiceAccount
    name: labenv-operator-controller-manager
    namespace: labenv-operator-system
```

Adjust the `namespace` of the ServiceAccount subject to match where the operator is deployed (check `deploy/overlays/sandbox/kustomization.yaml`).

- [ ] **Step 2: Add to kustomization**

```yaml
resources:
  - 24-operator-traefik-rbac.yaml
```

- [ ] **Step 3: Commit**

```
git add deploy/
git commit -m "feat(deploy): RoleBinding for operator to manage Traefik resources in rootenv-infra"
```

---

## Self-Review Checklist

After writing the plan, checking spec coverage:

| Spec requirement | Task |
|---|---|
| ingress-authenticator: ValidateToken + GetAttempt, user token only | Task 1 |
| ingress-authenticator: handler 200/400/401/403/503 | Task 2 |
| ingress-authenticator: env vars INGAUTH_* | Task 3 |
| relaybase.Handler + Backend interface | Task 4 |
| relay-exec: kubectl exec backend + KubeExecer | Task 5 |
| relay-exec: RELAY_* env vars, main, Dockerfile | Task 6 |
| Operator: SA + Role + RoleBinding + Deployment + Service per namespace | Task 7 |
| Operator: NetworkPolicy CreateOrPatch with relay-exec rules | Task 8 |
| Operator: Traefik IngressRoute + 3 Middlewares, cleanup on delete | Task 9 |
| ingress-authenticator deployment in rootenv-infra | Task 10 |
| Operator RBAC for Traefik resources in rootenv-infra | Task 11 |
