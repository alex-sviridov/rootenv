# relay-grader bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a new `relay-grader` binary that accepts an attempt-scoped WebSocket connection, loads a `tasks.json` task list at startup, and replies with `{taskId: false, ...}` for every task — no grading logic yet.

**Architecture:** New `grader` package (sibling to `exec`) implements `relaybase.Backend`. `relaybase.Handler` is changed to allow routes without an `{assetName}` path segment. A new `cmd/relay-grader` binary wires config, task loading, and the handler together, mirroring `cmd/relay-exec`.

**Tech Stack:** Go 1.26, `github.com/coder/websocket`, `net/http` `ServeMux` path values, `log/slog`, distroless static Docker image.

## Global Constraints

- Module path: `github.com/alexsviridov/linuxlab/relay`
- Route: `/relay/grade/{attemptID}/` — no `{assetName}` segment
- `tasks.json` schema: array of `{id: string, type: string, template: string}`; `type` must be `"term"`, all fields required
- Response message: single JSON text WS message, object keyed by task `id` → bool, e.g. `{"task1": false}` — sent immediately after auth succeeds
- Connection stays open (idle read loop) after sending the response, until client disconnect or ctx cancellation
- Env vars: `RELAY_MY_ATTEMPT_ID`, `RELAY_MY_OWNER_ID`, `RELAY_MY_NAMESPACE`, `RELAY_TASKS_FILE`, `RELAY_LISTEN_PORT` (default `8080`), `RELAY_SKIP_AUTH` (default `false`), `RELAY_ALLOWED_ORIGINS`, `LOG_LEVEL`
- Out of scope: actual grading logic, labenv-operator wiring, skaffold.yaml wiring, frontend changes, PocketBase writes

---

### Task 1: Make `relaybase.Handler` accept routes without `{assetName}`

**Files:**
- Modify: `services/relay/pkg/relaybase/handler.go:34-39`
- Test: `services/relay/pkg/relaybase/handler_test.go`

**Interfaces:**
- Consumes: existing `relaybase.Handler` struct and `relaybase.Backend` interface (unchanged signature: `Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error`)
- Produces: `Handler.ServeHTTP` no longer 400s when the mux pattern has no `{assetName}` segment; `assetName` is passed through as `""` in that case

- [ ] **Step 1: Write the failing test**

Add to `services/relay/pkg/relaybase/handler_test.go`:

```go
func TestHandler_no_asset_name_in_route(t *testing.T) {
	fb := &fakeBackend{}
	h := makeHandler(fb, "atm_123", "usr_abc", 2*time.Second)

	mux := http.NewServeMux()
	mux.Handle("/relay/grade/{attemptID}/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_123")
		r.Header.Set("X-User-Id", "usr_abc")
		h.ServeHTTP(w, r)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/relay/grade/atm_123/", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte("sometoken")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve was not called")
	}
	fb.mu.Lock()
	if fb.assetName != "" {
		t.Errorf("assetName: got %q, want empty string", fb.assetName)
	}
	fb.mu.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./pkg/relaybase/... -run TestHandler_no_asset_name_in_route -v`
Expected: FAIL — response is 400 "missing asset name", `Backend.Serve` never called

- [ ] **Step 3: Modify the handler to stop requiring assetName**

In `services/relay/pkg/relaybase/handler.go`, replace:

```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assetName := strings.Trim(r.PathValue("assetName"), "/")
	if assetName == "" {
		http.Error(w, "missing asset name", http.StatusBadRequest)
		return
	}

	log := slog.With("asset", assetName, "remote", r.RemoteAddr)
```

with:

```go
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assetName := strings.Trim(r.PathValue("assetName"), "/")

	log := slog.With("remote", r.RemoteAddr)
	if assetName != "" {
		log = log.With("asset", assetName)
	}
```

- [ ] **Step 4: Run test to verify it passes, and confirm no regressions**

Run: `cd services/relay && go test ./pkg/relaybase/... -v`
Expected: all tests PASS, including `TestHandler_no_asset_name_in_route` and every pre-existing `TestHandler_*` case

- [ ] **Step 5: Commit**

```bash
git add services/relay/pkg/relaybase/handler.go services/relay/pkg/relaybase/handler_test.go
git commit -m "feat(relay): allow relaybase.Handler routes without assetName"
```

---

### Task 2: `grader` package — task loading

**Files:**
- Create: `services/relay/grader/tasks.go`
- Test: `services/relay/grader/tasks_test.go`

**Interfaces:**
- Produces:
  - `type Task struct { ID string; Type string; Template string }`
  - `func LoadTasks(path string) ([]Task, error)` — reads file at `path`, parses JSON array, validates `id`/`type`/`template` non-empty and `type == "term"`; returns error (not panic/exit) on any failure

- [ ] **Step 1: Write the failing tests**

Create `services/relay/grader/tasks_test.go`:

```go
package grader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexsviridov/linuxlab/relay/grader"
)

func writeTasksFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}
	return path
}

func TestLoadTasks_valid(t *testing.T) {
	path := writeTasksFile(t, `[
		{"id": "task1", "type": "term", "template": "echo hi"},
		{"id": "task2", "type": "term", "template": "echo bye"}
	]`)

	tasks, err := grader.LoadTasks(path)
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(tasks))
	}
	if tasks[0].ID != "task1" || tasks[0].Type != "term" || tasks[0].Template != "echo hi" {
		t.Errorf("unexpected task[0]: %+v", tasks[0])
	}
}

func TestLoadTasks_missing_file(t *testing.T) {
	_, err := grader.LoadTasks("/nonexistent/tasks.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadTasks_invalid_json(t *testing.T) {
	path := writeTasksFile(t, `not json`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadTasks_missing_id(t *testing.T) {
	path := writeTasksFile(t, `[{"type": "term", "template": "echo hi"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestLoadTasks_missing_template(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "term"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for missing template, got nil")
	}
}

func TestLoadTasks_invalid_type(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "gui", "template": "echo hi"}]`)
	_, err := grader.LoadTasks(path)
	if err == nil {
		t.Fatal("expected error for invalid type, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/relay && go test ./grader/... -v`
Expected: FAIL to compile — `grader` package and `LoadTasks` don't exist yet

- [ ] **Step 3: Implement `LoadTasks`**

Create `services/relay/grader/tasks.go`:

```go
package grader

import (
	"encoding/json"
	"fmt"
	"os"
)

// Task is one gradeable item loaded from tasks.json.
type Task struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Template string `json:"template"`
}

// LoadTasks reads and validates the tasks.json file at path.
func LoadTasks(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks file: %w", err)
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}

	for i, task := range tasks {
		if task.ID == "" {
			return nil, fmt.Errorf("task[%d]: missing id", i)
		}
		if task.Template == "" {
			return nil, fmt.Errorf("task[%d] %q: missing template", i, task.ID)
		}
		if task.Type != "term" {
			return nil, fmt.Errorf("task[%d] %q: unsupported type %q (only \"term\" is supported)", i, task.ID, task.Type)
		}
	}

	return tasks, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/relay && go test ./grader/... -v`
Expected: all `TestLoadTasks_*` PASS

- [ ] **Step 5: Commit**

```bash
git add services/relay/grader/tasks.go services/relay/grader/tasks_test.go
git commit -m "feat(relay): add grader task loading from tasks.json"
```

---

### Task 3: `grader.Backend` — WebSocket response

**Files:**
- Create: `services/relay/grader/backend.go`
- Test: `services/relay/grader/backend_test.go`

**Interfaces:**
- Consumes: `grader.Task` from Task 2 (`ID string`, `Type string`, `Template string`); `relaybase.Backend` interface: `Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error`
- Produces: `type Backend struct { Tasks []Task; Log *slog.Logger }` implementing `Serve` — sends `{taskId: false, ...}` JSON, then idles reading until disconnect/ctx-cancel

- [ ] **Step 1: Write the failing tests**

Create `services/relay/grader/backend_test.go`:

```go
package grader_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
	"github.com/coder/websocket"
)

func TestBackend_sends_task_grades(t *testing.T) {
	b := &grader.Backend{
		Tasks: []grader.Task{
			{ID: "task1", Type: "term", Template: "echo hi"},
			{ID: "task2", Type: "term", Template: "echo bye"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var grades map[string]bool
	if err := json.Unmarshal(msg, &grades); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	want := map[string]bool{"task1": false, "task2": false}
	if len(grades) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(grades), len(want), grades)
	}
	for id, grade := range want {
		if got, ok := grades[id]; !ok || got != grade {
			t.Errorf("grades[%q] = %v, %v; want %v, true", id, got, ok, grade)
		}
	}
}

func TestBackend_stays_open_until_client_closes(t *testing.T) {
	b := &grader.Backend{Tasks: []grader.Task{{ID: "task1", Type: "term", Template: "x"}}}

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "", "usr_abc")
		close(done)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// consume the initial grade message
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read initial message: %v", err)
	}

	// Serve must not have returned yet — connection should still be open.
	select {
	case <-done:
		t.Fatal("Serve returned before client closed the connection")
	case <-time.After(200 * time.Millisecond):
		// expected: still open
	}

	_ = conn.CloseNow()

	select {
	case <-done:
		// expected: Serve returns after client disconnects
	case <-ctx.Done():
		t.Fatal("timeout waiting for Serve to return after client close")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/relay && go test ./grader/... -run TestBackend -v`
Expected: FAIL to compile — `grader.Backend` doesn't exist yet

- [ ] **Step 3: Implement `Backend`**

Create `services/relay/grader/backend.go`:

```go
package grader

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/coder/websocket"
)

// Backend implements relaybase.Backend for the grader relay type.
// It reports the current grade (always false in this bootstrap) for every
// loaded task, then holds the connection open until the client disconnects.
type Backend struct {
	Tasks []Task
	Log   *slog.Logger // defaults to slog.Default() if nil
}

func (b *Backend) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// Serve sends the initial grade report, then idles until the connection closes.
// assetName is unused — grading is attempt-scoped, not asset-scoped.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	log := b.logger().With("user_id", userID)

	grades := make(map[string]bool, len(b.Tasks))
	for _, task := range b.Tasks {
		grades[task.ID] = false
	}

	payload, err := json.Marshal(grades)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		log.Error("failed to send grade report", "err", err)
		return err
	}

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return nil
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/relay && go test ./grader/... -v`
Expected: all tests in `services/relay/grader/` PASS

- [ ] **Step 5: Commit**

```bash
git add services/relay/grader/backend.go services/relay/grader/backend_test.go
git commit -m "feat(relay): add grader.Backend WebSocket grade response"
```

---

### Task 4: `cmd/relay-grader` binary

**Files:**
- Create: `services/relay/cmd/relay-grader/main.go`

**Interfaces:**
- Consumes: `grader.LoadTasks(path string) ([]Task, error)` and `grader.Backend{Tasks []Task, Log *slog.Logger}` from Tasks 2–3; `relaybase.Handler{Backend, Limiter, AttemptID, OwnerID, SkipAuth, AllowedOrigins, WG}` and `relaybase.NewConnLimiter(n int) *ConnLimiter` (existing)
- Produces: a runnable binary at `./cmd/relay-grader` serving `/healthz` and `/relay/grade/{attemptID}/`

- [ ] **Step 1: Write `main.go`**

Create `services/relay/cmd/relay-grader/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

type config struct {
	port           string
	namespace      string
	attemptID      string
	ownerID        string
	tasksFile      string
	skipAuth       bool
	allowedOrigins []string
	logLevel       slog.Level
}

func loadConfig() (config, bool) {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}

	skipAuth := os.Getenv("RELAY_SKIP_AUTH") == "true"

	attemptID := os.Getenv("RELAY_MY_ATTEMPT_ID")
	if attemptID == "" && !skipAuth {
		slog.Error("RELAY_MY_ATTEMPT_ID is required (set RELAY_SKIP_AUTH=true to skip)")
		return config{}, false
	}
	ownerID := os.Getenv("RELAY_MY_OWNER_ID")
	if ownerID == "" && !skipAuth {
		slog.Error("RELAY_MY_OWNER_ID is required (set RELAY_SKIP_AUTH=true to skip)")
		return config{}, false
	}
	namespace := os.Getenv("RELAY_MY_NAMESPACE")
	if namespace == "" {
		slog.Error("RELAY_MY_NAMESPACE is required")
		return config{}, false
	}
	tasksFile := os.Getenv("RELAY_TASKS_FILE")
	if tasksFile == "" {
		slog.Error("RELAY_TASKS_FILE is required")
		return config{}, false
	}

	port := os.Getenv("RELAY_LISTEN_PORT")
	if port == "" {
		port = "8080"
	}

	var origins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}

	return config{
		port:           port,
		namespace:      namespace,
		attemptID:      attemptID,
		ownerID:        ownerID,
		tasksFile:      tasksFile,
		skipAuth:       skipAuth,
		allowedOrigins: origins,
		logLevel:       level,
	}, true
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		r, err := http.Get("http://localhost:8080/healthz")
		if err != nil || r.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, ok := loadConfig()
	if !ok {
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel})))

	tasks, err := grader.LoadTasks(cfg.tasksFile)
	if err != nil {
		slog.Error("failed to load tasks", "err", err, "path", cfg.tasksFile)
		os.Exit(1)
	}

	backend := grader.Backend{
		Tasks: tasks,
		Log:   slog.Default().With("namespace", cfg.namespace),
	}
	limiter := relaybase.NewConnLimiter(16)

	var wg sync.WaitGroup
	handler := &relaybase.Handler{
		Backend:        &backend,
		Limiter:        limiter,
		AttemptID:      cfg.attemptID,
		OwnerID:        cfg.ownerID,
		SkipAuth:       cfg.skipAuth,
		AllowedOrigins: cfg.allowedOrigins,
		WG:             &wg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/relay/grade/{attemptID}/", handler)

	srv := &http.Server{
		Addr:    ":" + cfg.port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("relay-grader starting", "port", cfg.port, "skip_auth", cfg.skipAuth, "attempt_id", cfg.attemptID, "namespace", cfg.namespace, "tasks", len(tasks))
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all sessions drained")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timeout: sessions still active")
	}
}
```

- [ ] **Step 2: Build the binary**

Run: `cd services/relay && go build -o /tmp/relay-grader ./cmd/relay-grader`
Expected: builds with no errors

- [ ] **Step 3: Smoke-test the binary manually**

```bash
cd services/relay
cat > /tmp/tasks.json <<'EOF'
[{"id": "task1", "type": "term", "template": "echo hi"}]
EOF
RELAY_SKIP_AUTH=true RELAY_MY_NAMESPACE=test RELAY_TASKS_FILE=/tmp/tasks.json RELAY_LISTEN_PORT=8099 /tmp/relay-grader &
sleep 1
curl -s http://localhost:8099/healthz
kill %1
```
Expected: `{"status":"ok"}` printed; process starts without exiting

- [ ] **Step 4: Run full test suite for the module**

Run: `cd services/relay && go test ./... -v`
Expected: all tests PASS across `exec`, `grader`, `pkg/relaybase`

- [ ] **Step 5: Commit**

```bash
git add services/relay/cmd/relay-grader/main.go
git commit -m "feat(relay): add relay-grader binary entry point"
```

---

### Task 5: Dockerfile for relay-grader

**Files:**
- Create: `services/relay/grader/Dockerfile`

**Interfaces:**
- Consumes: `services/relay/cmd/relay-grader` (Task 4), `services/relay/go.mod`/`go.sum`
- Produces: buildable container image for relay-grader

- [ ] **Step 1: Write the Dockerfile**

Create `services/relay/grader/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o relay-grader ./cmd/relay-grader

FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot
COPY --from=builder /app/relay-grader /relay-grader
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/relay-grader", "--healthcheck"]
ENTRYPOINT ["/relay-grader"]
```

- [ ] **Step 2: Build the image**

Run: `cd services/relay && docker build -f grader/Dockerfile -t relay-grader:test .`
Expected: image builds successfully

- [ ] **Step 3: Smoke-test the container**

```bash
cat > /tmp/tasks.json <<'EOF'
[{"id": "task1", "type": "term", "template": "echo hi"}]
EOF
docker run --rm -d --name relay-grader-test \
  -e RELAY_SKIP_AUTH=true -e RELAY_MY_NAMESPACE=test -e RELAY_TASKS_FILE=/tasks.json \
  -v /tmp/tasks.json:/tasks.json:ro \
  -p 8099:8080 relay-grader:test
sleep 1
curl -s http://localhost:8099/healthz
docker stop relay-grader-test
```
Expected: `{"status":"ok"}` printed

- [ ] **Step 4: Commit**

```bash
git add services/relay/grader/Dockerfile
git commit -m "feat(relay): add relay-grader Dockerfile"
```

---

## Self-Review Notes

- **Spec coverage:** `relaybase.Handler` optional assetName (Task 1); `tasks.json` loading + validation (Task 2); WS protocol — grade response + idle-until-close (Task 3); binary/config/healthz/graceful shutdown (Task 4); Dockerfile (Task 5). All design sections covered. labenv-operator wiring and skaffold.yaml wiring are explicitly out of scope per the design doc and are not tasks here.
- **Type consistency:** `grader.Task{ID, Type, Template}` (Task 2) used identically in `grader.Backend{Tasks []Task}` (Task 3) and `grader.LoadTasks` return type consumed in `cmd/relay-grader/main.go` (Task 4). `relaybase.Backend.Serve` signature matches existing interface unchanged.
- **No placeholders:** every step has complete, runnable code and exact commands.
