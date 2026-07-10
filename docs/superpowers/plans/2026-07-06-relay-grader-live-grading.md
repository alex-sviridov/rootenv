# relay-grader live grading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream `relay-exec` terminal output to `relay-grader` over a network-policy-scoped internal port, so `relay-grader` can match exercise regex templates against real terminal output and push live grade updates to the frontend, while `relay-exec` remains completely unaffected if `relay-grader` is absent, down, or slow.

**Architecture:** `relay-exec`'s `Backend.Serve` gains a non-blocking `Forwarder.Send(asset, chunk)` call alongside its existing stdout→WebSocket write; a background goroutine owns the actual outbound connection and drops data if not connected. `relay-grader` gains a second TCP listener (new port) that ingests newline-delimited JSON chunks, reassembles them per-asset, strips ANSI escapes, keeps a 10-line ring buffer per asset, and re-runs each unmatched task's compiled regex on relevant buffers. Matches are sticky and trigger a broadcast of the full grade map to all frontend WebSocket clients connected to that attempt. `labenv-operator` wires the new port through both Deployments, the grader Service, and both NetworkPolicies.

**Tech Stack:** Go 1.26, `github.com/coder/websocket`, standard library `net`/`bufio`/`regexp`/`encoding/json`, Ginkgo/Gomega + envtest for `labenv-operator`, plain `testing` for `relay`.

## Global Constraints

- `relay-exec` must build, start, and serve terminal sessions correctly even when `RELAY_GRADER_ADDR` is unset or `relay-grader` is unreachable — no blocking, no error propagation into the exec/WebSocket path from any forwarder failure (spec: "Priority Constraint").
- The exec→grader link never persists dropped messages and never retries a specific message — pure fire-and-forget (spec section 1).
- `relay-grader`'s frontend-facing WS handler, URL, and auth pattern (`/relay/grade/{attemptID}/`, injected headers, first-message discard) do not change (spec section 4, "What is NOT changed").
- No new auth mechanism on the internal exec→grader link — NetworkPolicy is the sole trust boundary, matching the existing exec→apiserver pattern (spec section 5).
- Grades are sticky (`false → true` only) and never persisted across `relay-grader` restarts (spec section 3, "What is NOT changed").
- `services/relay` module path is `github.com/alexsviridov/linuxlab/relay`; run tests via `make test` in `services/relay` (Docker-wrapped `go test -v ./... && go build -v ./...`).
- `services/labenv-operator` tests run via `make test` (envtest, Ginkgo/Gomega, `KUBEBUILDER_ASSETS` auto-provisioned).

---

## File Structure

New/modified files:

- `services/relay/grader/lines.go` — ANSI-stripping + ring buffer type (`assetBuffer`), pure logic, no I/O. New file so matching/buffering logic is testable in isolation from networking.
- `services/relay/grader/lines_test.go` — tests for the above.
- `services/relay/grader/backend.go` — extend `Backend` with grading state (`regexes`, `assets`, `grades`, `clients`), `Ingest`, matching, broadcast; rewrite `Serve` to register/deregister clients and support multiple sends.
- `services/relay/grader/backend_test.go` — extend existing tests.
- `services/relay/grader/listener.go` — new internal TCP listener (`ListenAndServeInternal` or similar), NDJSON parsing, calls `Backend.Ingest`.
- `services/relay/grader/listener_test.go` — tests for the above.
- `services/relay/cmd/relay-grader/main.go` — start the internal listener alongside the existing HTTP server; new `RELAY_GRADER_INTERNAL_PORT` config.
- `services/relay/exec/forwarder.go` — new file: `Forwarder` type, `NewForwarder`, non-blocking `Send`, background reconnect/drain goroutine.
- `services/relay/exec/forwarder_test.go` — tests for the above.
- `services/relay/exec/backend.go` — add `Forwarder *Forwarder` field to `Backend`, call `Send` in the stdout loop.
- `services/relay/exec/backend_test.go` — extend existing tests.
- `services/relay/cmd/relay-exec/main.go` — construct `Forwarder` from `RELAY_GRADER_ADDR`, pass into `Backend`.
- `services/labenv-operator/internal/controller/grader.go` — add port 8081 to Service, env var + container port to Deployment, new ingress rule to grader NetworkPolicy.
- `services/labenv-operator/internal/controller/relay.go` — add new egress rule to relay-exec NetworkPolicy, add `RELAY_GRADER_ADDR` env var to relay-exec Deployment.
- `services/labenv-operator/internal/controller/labenvironment_controller_test.go` — extend existing `ensureRelayGrader` / `ensureRelayNetworkPolicy` describes.

---

## Task 1: Ring buffer + ANSI stripping (pure logic, no networking)

**Files:**
- Create: `services/relay/grader/lines.go`
- Test: `services/relay/grader/lines_test.go`

**Interfaces:**
- Produces: `type assetBuffer struct{ ... }` with methods `func (b *assetBuffer) Ingest(chunk []byte) (newLines bool)` and `func (b *assetBuffer) Joined() string`. `func stripANSI(s string) string`. These are consumed by Task 2 (`Backend.Ingest`).

- [ ] **Step 1: Write the failing test for ANSI stripping**

```go
package grader

import "testing"

func TestStripANSI_removes_color_codes(t *testing.T) {
	in := "\x1b[32mchown bob /tmp/labfile\x1b[0m\n"
	want := "chown bob /tmp/labfile\n"
	if got := stripANSI(in); got != want {
		t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
	}
}

func TestStripANSI_leaves_plain_text_untouched(t *testing.T) {
	in := "no escapes here"
	if got := stripANSI(in); got != in {
		t.Errorf("stripANSI(%q) = %q, want unchanged", in, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run TestStripANSI -v`
Expected: FAIL — `undefined: stripANSI`

- [ ] **Step 3: Implement `stripANSI`**

```go
package grader

import "regexp"

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// stripANSI removes terminal escape sequences (color, cursor movement) so
// regex templates match what a human reads, not raw PTY control bytes.
func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -run TestStripANSI -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for the ring buffer's line splitting**

```go
func TestAssetBuffer_splits_complete_lines_and_keeps_partial(t *testing.T) {
	b := &assetBuffer{}

	newLines := b.Ingest([]byte("hello wor"))
	if newLines {
		t.Error("no newline yet: want newLines=false")
	}
	if got := b.Joined(); got != "" {
		t.Errorf("Joined() = %q, want empty (no complete line yet)", got)
	}

	newLines = b.Ingest([]byte("ld\nsecond line\nthird-partial"))
	if !newLines {
		t.Error("want newLines=true after chunk containing newlines")
	}
	want := "hello world\nsecond line"
	if got := b.Joined(); got != want {
		t.Errorf("Joined() = %q, want %q", got, want)
	}
}

func TestAssetBuffer_ring_caps_at_ten_lines(t *testing.T) {
	b := &assetBuffer{}
	for i := 0; i < 15; i++ {
		b.Ingest([]byte("line" + string(rune('0'+i%10)) + "\n"))
	}
	lines := b.Joined()
	count := 1
	for _, r := range lines {
		if r == '\n' {
			count++
		}
	}
	if count != 10 {
		t.Errorf("got %d lines, want 10 (ring cap)", count)
	}
	if got := b.Joined(); got[:5] != "line5" {
		t.Errorf("Joined() = %q, want to start with the 6th line (oldest 5 dropped)", got)
	}
}

func TestAssetBuffer_strips_ansi_before_splitting(t *testing.T) {
	b := &assetBuffer{}
	b.Ingest([]byte("\x1b[32mchown bob /tmp/labfile\x1b[0m\n"))
	want := "chown bob /tmp/labfile"
	if got := b.Joined(); got != want {
		t.Errorf("Joined() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run TestAssetBuffer -v`
Expected: FAIL — `undefined: assetBuffer`

- [ ] **Step 7: Implement `assetBuffer`**

Add to `services/relay/grader/lines.go`:

```go
const ringCapacity = 10

// assetBuffer reassembles a per-asset byte stream into complete, ANSI-stripped
// lines and keeps the most recent ringCapacity of them for regex matching.
type assetBuffer struct {
	partial []byte
	lines   []string // ring, oldest first, len capped at ringCapacity
}

// Ingest appends chunk to the pending partial line, strips ANSI escapes,
// splits on '\n', and pushes every complete line into the ring buffer.
// The trailing incomplete segment (if any) is kept as the new partial.
// Returns true if at least one new complete line was added.
func (b *assetBuffer) Ingest(chunk []byte) bool {
	combined := stripANSI(string(append(b.partial, chunk...)))
	b.partial = nil

	segments := splitLines(combined)
	if len(segments) == 0 {
		return false
	}

	// last segment is incomplete unless combined ended in '\n'
	complete := segments
	if len(combined) == 0 || combined[len(combined)-1] != '\n' {
		complete = segments[:len(segments)-1]
		b.partial = []byte(segments[len(segments)-1])
	}

	if len(complete) == 0 {
		return false
	}

	for _, line := range complete {
		b.lines = append(b.lines, line)
	}
	if overflow := len(b.lines) - ringCapacity; overflow > 0 {
		b.lines = b.lines[overflow:]
	}
	return true
}

// splitLines splits s on '\n', keeping the trailing empty segment if s ends
// in '\n' (so the caller can distinguish "ended cleanly" from "mid-line").
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// Joined returns the buffered lines newline-joined, for regex matching.
func (b *assetBuffer) Joined() string {
	return joinLines(b.lines)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -run TestAssetBuffer -v`
Expected: PASS

- [ ] **Step 9: Run the full grader package test suite**

Run: `cd services/relay && go test ./grader/... -v`
Expected: PASS (all existing + new tests)

- [ ] **Step 10: Commit**

```bash
git add services/relay/grader/lines.go services/relay/grader/lines_test.go
git commit -m "feat(relay/grader): add ANSI-stripping ring buffer for per-asset lines"
```

---

## Task 2: Grading state and matching in `Backend`

**Files:**
- Modify: `services/relay/grader/backend.go`
- Test: `services/relay/grader/backend_test.go`

**Interfaces:**
- Consumes: `assetBuffer` (`Ingest([]byte) bool`, `Joined() string`) from Task 1; `Task{ID, Type, Template, Asset}` from existing `tasks.go`.
- Produces: `func NewBackend(tasks []Task, log *slog.Logger) *Backend`; `func (b *Backend) Ingest(asset string, chunk []byte)`; `func (b *Backend) Serve(ctx, conn, assetName, userID) error` (existing signature, new internal behavior — registers/deregisters `conn` for broadcast). Consumed by Task 3 (`listener.go` calls `Ingest`) and Task 4 (`main.go` calls `NewBackend`).

- [ ] **Step 1: Write the failing test for regex compilation + matching via `Ingest`**

```go
func TestBackend_Ingest_marks_task_passed_on_regex_match(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: `chown\s+bob\s+/tmp/labfile`},
	}, nil)

	b.Ingest("main", []byte("$ chown bob /tmp/labfile\n"))

	grades := b.Grades()
	if !grades["1.1"] {
		t.Errorf("grades[1.1] = %v, want true after matching input", grades["1.1"])
	}
}

func TestBackend_Ingest_asset_scoped_task_ignores_other_assets(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: `chown\s+bob`},
	}, nil)

	b.Ingest("other-asset", []byte("chown bob /tmp/labfile\n"))

	grades := b.Grades()
	if grades["1.1"] {
		t.Error("grades[1.1] = true, want false — match was on the wrong asset")
	}
}

func TestBackend_Ingest_lab_wide_task_matches_any_asset(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.2", Type: "term", Template: "echo hi"},
	}, nil)

	b.Ingest("second-asset", []byte("echo hi\n"))

	grades := b.Grades()
	if !grades["1.2"] {
		t.Error("grades[1.2] = false, want true — lab-wide task should match any asset")
	}
}

func TestBackend_Ingest_grade_is_sticky(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "PASS_MARKER"},
	}, nil)

	b.Ingest("main", []byte("PASS_MARKER\n"))
	if !b.Grades()["1.1"] {
		t.Fatal("expected 1.1 to pass")
	}

	// Feed 10+ more lines so PASS_MARKER scrolls out of the ring buffer.
	for i := 0; i < 12; i++ {
		b.Ingest("main", []byte("filler\n"))
	}

	if !b.Grades()["1.1"] {
		t.Error("grades[1.1] = false, want true — grade must stay sticky even after buffer scrolls")
	}
}

func TestBackend_Ingest_skips_already_passed_tasks(t *testing.T) {
	// A task whose regex would also match "filler" is used to prove that once
	// passed, later non-matching input doesn't flip it back, and matching
	// isn't needlessly re-run (behavioral proxy: grade stays true).
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "one"},
	}, nil)
	b.Ingest("main", []byte("one\n"))
	b.Ingest("main", []byte("two\n"))
	if !b.Grades()["1.1"] {
		t.Error("grades[1.1] should remain true")
	}
}

func TestBackend_Ingest_invalid_regex_is_skipped_not_fatal(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "bad", Type: "term", Template: "(unclosed"},
		{ID: "good", Type: "term", Template: "hello"},
	}, nil)

	// Must not panic despite the invalid regex.
	b.Ingest("main", []byte("hello\n"))

	grades := b.Grades()
	if grades["bad"] {
		t.Error("grades[bad] = true, want false — invalid regex never matches")
	}
	if !grades["good"] {
		t.Error("grades[good] = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run TestBackend_Ingest -v`
Expected: FAIL — `undefined: grader.NewBackend`, `undefined: b.Grades`, `undefined: b.Ingest`

- [ ] **Step 3: Implement grading state in `Backend`**

Replace the top of `services/relay/grader/backend.go` (keep the existing `Serve` for now — rewritten in Step 5):

```go
package grader

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"sync"

	"github.com/coder/websocket"
)

// Backend implements relaybase.Backend for the grader relay type.
// It matches each task's regex Template against real-time terminal output
// forwarded by relay-exec (via Ingest) and reports sticky pass/fail grades
// to every connected frontend WebSocket client.
type Backend struct {
	Tasks []Task
	Log   *slog.Logger // defaults to slog.Default() if nil

	initOnce sync.Once
	regexes  map[string]*regexp.Regexp // compiled once, keyed by task ID; missing key = invalid/skipped

	mu      sync.Mutex
	assets  map[string]*assetBuffer
	grades  map[string]bool
	clients map[*websocket.Conn]struct{}
}

// NewBackend constructs a Backend with regexes pre-compiled and grading maps
// initialized. Prefer this over a bare &Backend{Tasks: ...} literal so tests
// and callers don't need to know about lazy initialization.
func NewBackend(tasks []Task, log *slog.Logger) *Backend {
	b := &Backend{Tasks: tasks, Log: log}
	b.init()
	return b
}

func (b *Backend) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

func (b *Backend) init() {
	b.initOnce.Do(func() {
		b.regexes = make(map[string]*regexp.Regexp, len(b.Tasks))
		b.assets = make(map[string]*assetBuffer)
		b.grades = make(map[string]bool, len(b.Tasks))
		b.clients = make(map[*websocket.Conn]struct{})
		for _, task := range b.Tasks {
			re, err := regexp.Compile(task.Template)
			if err != nil {
				b.logger().Error("invalid task template regex, task will never pass", "task_id", task.ID, "err", err)
				continue
			}
			b.regexes[task.ID] = re
		}
	})
}

// Grades returns a snapshot copy of the current grade map.
func (b *Backend) Grades() map[string]bool {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]bool, len(b.grades))
	for id := range b.regexes {
		out[id] = b.grades[id]
	}
	// Tasks with invalid regex never appear in b.regexes; still report them
	// as false so the frontend sees an entry for every task.
	for _, task := range b.Tasks {
		if _, ok := out[task.ID]; !ok {
			out[task.ID] = false
		}
	}
	return out
}

// Ingest reassembles chunk into asset's line buffer and re-runs matching for
// every not-yet-passed task against the relevant buffer(s).
func (b *Backend) Ingest(asset string, chunk []byte) {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()

	buf, ok := b.assets[asset]
	if !ok {
		buf = &assetBuffer{}
		b.assets[asset] = buf
	}
	if !buf.Ingest(chunk) {
		return
	}

	changed := false
	for _, task := range b.Tasks {
		if b.grades[task.ID] {
			continue // sticky: already passed
		}
		re, ok := b.regexes[task.ID]
		if !ok {
			continue // invalid regex, never matches
		}
		if task.Asset != "" {
			if task.Asset != asset {
				continue
			}
			if re.MatchString(buf.Joined()) {
				b.grades[task.ID] = true
				changed = true
			}
			continue
		}
		// lab-wide: check every asset's buffer
		for _, ab := range b.assets {
			if re.MatchString(ab.Joined()) {
				b.grades[task.ID] = true
				changed = true
				break
			}
		}
	}

	if changed {
		b.broadcastLocked()
	}
}

// broadcastLocked sends the current grade map to every connected client.
// Caller must hold b.mu.
func (b *Backend) broadcastLocked() {
	payload, err := json.Marshal(b.gradesLocked())
	if err != nil {
		b.logger().Error("failed to marshal grades for broadcast", "err", err)
		return
	}
	for conn := range b.clients {
		if err := conn.Write(context.Background(), websocket.MessageText, payload); err != nil {
			delete(b.clients, conn)
		}
	}
}

// gradesLocked returns the full grade map including invalid-regex tasks as
// false. Caller must hold b.mu.
func (b *Backend) gradesLocked() map[string]bool {
	out := make(map[string]bool, len(b.Tasks))
	for _, task := range b.Tasks {
		out[task.ID] = b.grades[task.ID]
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -run TestBackend_Ingest -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for `Serve` supporting multiple broadcast messages**

This replaces the two existing tests in `backend_test.go` (`TestBackend_sends_task_grades`, `TestBackend_stays_open_until_client_closes`) with versions using `NewBackend`, plus a new multi-message test:

```go
func TestBackend_sends_task_grades(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "task1", Type: "term", Template: "echo hi"},
		{ID: "task2", Type: "term", Template: "echo bye"},
	}, nil)

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
	b := grader.NewBackend([]grader.Task{{ID: "task1", Type: "term", Template: "x"}}, nil)

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

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read initial message: %v", err)
	}

	select {
	case <-done:
		t.Fatal("Serve returned before client closed the connection")
	case <-time.After(200 * time.Millisecond):
	}

	_ = conn.CloseNow()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for Serve to return after client close")
	}
}

func TestBackend_broadcasts_grade_update_to_connected_client(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "DONE_MARKER"},
	}, nil)

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

	// consume bootstrap message
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read bootstrap: %v", err)
	}

	b.Ingest("main", []byte("DONE_MARKER\n"))

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	var grades map[string]bool
	if err := json.Unmarshal(msg, &grades); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !grades["1.1"] {
		t.Errorf("grades[1.1] = %v, want true after broadcast", grades["1.1"])
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run "TestBackend_sends_task_grades|TestBackend_stays_open|TestBackend_broadcasts" -v`
Expected: FAIL — `TestBackend_broadcasts_grade_update_to_connected_client` times out/fails (client never registered, no broadcast happens yet); other two should still compile/pass against old `Serve` since only construction changed to `NewBackend`.

- [ ] **Step 7: Rewrite `Serve` to register/deregister clients**

Replace the existing `Serve` method in `services/relay/grader/backend.go`:

```go
// Serve registers conn for broadcast, sends the current grade snapshot, then
// blocks reading (discarding) client messages until the connection closes,
// at which point conn is deregistered.
// assetName is unused — grading is attempt-scoped, not asset-scoped.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	b.init()
	log := b.logger().With("user_id", userID)

	b.mu.Lock()
	b.clients[conn] = struct{}{}
	payload, err := json.Marshal(b.gradesLocked())
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		log.Error("failed to send grade report", "err", err)
		b.mu.Lock()
		delete(b.clients, conn)
		b.mu.Unlock()
		return err
	}

	defer func() {
		b.mu.Lock()
		delete(b.clients, conn)
		b.mu.Unlock()
	}()

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return nil
		}
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -v`
Expected: PASS (all tests in the package, including the three from Step 5 and Task 1's tests)

- [ ] **Step 9: Commit**

```bash
git add services/relay/grader/backend.go services/relay/grader/backend_test.go
git commit -m "feat(relay/grader): add regex-based grading, sticky grades, and broadcast on change"
```

---

## Task 3: Internal TCP listener for exec→grader ingestion

**Files:**
- Create: `services/relay/grader/listener.go`
- Test: `services/relay/grader/listener_test.go`

**Interfaces:**
- Consumes: `Backend.Ingest(asset string, chunk []byte)` from Task 2.
- Produces: `func ListenAndServeInternal(ctx context.Context, addr string, backend *Backend, log *slog.Logger) error` — consumed by Task 4 (`cmd/relay-grader/main.go`).

- [ ] **Step 1: Write the failing test for NDJSON ingestion**

```go
package grader_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
)

func TestListenAndServeInternal_ingests_valid_lines(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "MARKER"},
	}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = grader.ServeInternalListener(ctx, ln, b, nil) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg, _ := json.Marshal(map[string]string{"asset": "main", "data": "MARKER\n"})
	if _, err := conn.Write(append(msg, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Grades()["1.1"] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("grades[1.1] never became true after valid NDJSON ingestion")
}

func TestListenAndServeInternal_skips_malformed_line_without_closing_connection(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "MARKER"},
	}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = grader.ServeInternalListener(ctx, ln, b, nil) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("not valid json\n")); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	msg, _ := json.Marshal(map[string]string{"asset": "main", "data": "MARKER\n"})
	if _, err := conn.Write(append(msg, '\n')); err != nil {
		t.Fatalf("write valid after malformed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Grades()["1.1"] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("connection should survive a malformed line and still ingest the next valid one")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run TestListenAndServeInternal -v`
Expected: FAIL — `undefined: grader.ServeInternalListener`

- [ ] **Step 3: Implement the internal listener**

```go
package grader

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
)

type forwardMsg struct {
	Asset string `json:"asset"`
	Data  string `json:"data"`
}

// ServeInternalListener accepts connections from relay-exec forwarders on ln
// and feeds every well-formed NDJSON line into backend.Ingest. Malformed
// lines are logged and skipped — never close the connection or crash the
// listener over one bad message. Returns when ctx is cancelled.
func ServeInternalListener(ctx context.Context, ln net.Listener, backend *Backend, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go handleForwarderConn(conn, backend, log)
	}
}

func handleForwarderConn(conn net.Conn, backend *Backend, log *slog.Logger) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg forwardMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Debug("skipping malformed forwarder line", "err", err)
			continue
		}
		if msg.Asset == "" {
			continue
		}
		backend.Ingest(msg.Asset, []byte(msg.Data))
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -v`
Expected: PASS (full package)

- [ ] **Step 5: Commit**

```bash
git add services/relay/grader/listener.go services/relay/grader/listener_test.go
git commit -m "feat(relay/grader): add internal NDJSON listener for relay-exec forwarding"
```

---

## Task 4: Wire the internal listener into `cmd/relay-grader`

**Files:**
- Modify: `services/relay/cmd/relay-grader/main.go`

**Interfaces:**
- Consumes: `grader.NewBackend` (Task 2), `grader.ServeInternalListener` (Task 3).

- [ ] **Step 1: Add `RELAY_GRADER_INTERNAL_PORT` to `config` and `loadConfig`**

In `services/relay/cmd/relay-grader/main.go`, add a field and default to the `config` struct and `loadConfig()`:

```go
type config struct {
	port           string
	internalPort   string
	namespace      string
	attemptID      string
	ownerID        string
	tasksFile      string
	skipAuth       bool
	allowedOrigins []string
	logLevel       slog.Level
}
```

Inside `loadConfig()`, after the existing `port` block:

```go
	internalPort := os.Getenv("RELAY_GRADER_INTERNAL_PORT")
	if internalPort == "" {
		internalPort = "8081"
	}
```

And add `internalPort: internalPort,` to the returned `config{...}` literal.

- [ ] **Step 2: Construct `Backend` via `NewBackend` and start the internal listener**

Replace:

```go
	backend := grader.Backend{
		Tasks: tasks,
		Log:   slog.Default().With("namespace", cfg.namespace),
	}
```

with:

```go
	backend := grader.NewBackend(tasks, slog.Default().With("namespace", cfg.namespace))
```

Then update every `&backend` reference below (the `relaybase.Handler{Backend: &backend, ...}` line) to `backend` (already a pointer now):

```go
	handler := &relaybase.Handler{
		Backend:        backend,
		Limiter:        limiter,
		AttemptID:      cfg.attemptID,
		OwnerID:        cfg.ownerID,
		SkipAuth:       cfg.skipAuth,
		AllowedOrigins: cfg.allowedOrigins,
		WG:             &wg,
	}
```

Before the `<-ctx.Done()` line (i.e. right after the existing `go func() { srv.ListenAndServe... }()` block), start the internal listener:

```go
	internalLn, err := net.Listen("tcp", ":"+cfg.internalPort)
	if err != nil {
		slog.Error("failed to listen on internal port", "err", err, "port", cfg.internalPort)
		os.Exit(1)
	}
	go func() {
		if err := grader.ServeInternalListener(ctx, internalLn, backend, slog.Default()); err != nil {
			slog.Error("internal listener error", "err", err)
		}
	}()
```

Add `"net"` to the import block.

- [ ] **Step 3: Update the startup log line to include the internal port**

```go
	slog.Info("relay-grader starting", "port", cfg.port, "internal_port", cfg.internalPort, "skip_auth", cfg.skipAuth, "attempt_id", cfg.attemptID, "namespace", cfg.namespace, "tasks", len(tasks))
```

- [ ] **Step 4: Build to verify it compiles**

Run: `cd services/relay && go build ./...`
Expected: success, no errors

- [ ] **Step 5: Run the full relay test suite**

Run: `cd services/relay && make test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add services/relay/cmd/relay-grader/main.go
git commit -m "feat(relay-grader): start internal listener alongside frontend-facing server"
```

---

## Task 5: `Forwarder` in relay-exec (fire-and-forget outbound client)

**Files:**
- Create: `services/relay/exec/forwarder.go`
- Test: `services/relay/exec/forwarder_test.go`

**Interfaces:**
- Produces: `func NewForwarder(addr string, log *slog.Logger) *Forwarder`; `func (f *Forwarder) Send(asset string, data []byte)`; `func (f *Forwarder) Close()`. Consumed by Task 6 (`exec.Backend`).

- [ ] **Step 1: Write the failing test for non-blocking Send with no listener present**

```go
package exec_test

import (
	"testing"
	"time"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
)

func TestForwarder_Send_never_blocks_when_unreachable(t *testing.T) {
	f := relayexec.NewForwarder("127.0.0.1:1", nil) // port 1: nothing listens there
	defer f.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			f.Send("main", []byte("line\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked for 2s while forwarder is unreachable — must be non-blocking")
	}
}

func TestForwarder_Send_noop_when_addr_empty(t *testing.T) {
	f := relayexec.NewForwarder("", nil)
	defer f.Close()

	done := make(chan struct{})
	go func() {
		f.Send("main", []byte("line\n"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Send blocked with empty addr — no-op forwarder must return immediately")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./exec/... -run TestForwarder -v`
Expected: FAIL — `undefined: relayexec.NewForwarder`

- [ ] **Step 3: Implement `Forwarder`**

```go
package exec

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"time"
)

const forwarderChanCapacity = 256

type forwardMsg struct {
	Asset string `json:"asset"`
	Data  string `json:"data"`
}

// Forwarder fire-and-forget streams terminal output chunks to relay-grader's
// internal port. If relay-grader is unreachable, down, or slow, Send drops
// the chunk instead of blocking — relay-exec's terminal sessions must never
// be affected by grader availability.
type Forwarder struct {
	addr string
	ch   chan forwardMsg
	log  *slog.Logger

	closeOnce sync.Once
	done      chan struct{}
}

// NewForwarder starts a background connection/reconnect loop to addr. If addr
// is empty, the returned Forwarder is a no-op — Send returns immediately and
// nothing is ever dialed. This lets relay-exec run fine with grading disabled
// (e.g. local dev without a relay-grader instance).
func NewForwarder(addr string, log *slog.Logger) *Forwarder {
	if log == nil {
		log = slog.Default()
	}
	f := &Forwarder{
		addr: addr,
		ch:   make(chan forwardMsg, forwarderChanCapacity),
		log:  log,
		done: make(chan struct{}),
	}
	if addr != "" {
		go f.run()
	}
	return f
}

// Send enqueues data for forwarding, tagged with asset. Never blocks: if the
// internal channel is full (forwarder disconnected or backed up), the chunk
// is dropped and logged at Debug level.
func (f *Forwarder) Send(asset string, data []byte) {
	if f.addr == "" {
		return
	}
	select {
	case f.ch <- forwardMsg{Asset: asset, Data: string(data)}:
	default:
		f.log.Debug("dropping forwarder message: channel full or disconnected", "asset", asset)
	}
}

// Close stops the background reconnect loop.
func (f *Forwarder) Close() {
	f.closeOnce.Do(func() { close(f.done) })
}

func (f *Forwarder) run() {
	backoff := 500 * time.Millisecond
	const maxBackoff = 10 * time.Second

	for {
		select {
		case <-f.done:
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", f.addr, 5*time.Second)
		if err != nil {
			f.log.Warn("forwarder: dial failed, will retry", "addr", f.addr, "err", err)
			select {
			case <-time.After(backoff):
			case <-f.done:
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = 500 * time.Millisecond
		f.drainInto(conn)
		_ = conn.Close()
	}
}

// drainInto writes queued messages to conn until a write fails or Close is
// called. A write failure returns so run() can reconnect; the message being
// written when the failure occurred is dropped (fire-and-forget, no retry).
func (f *Forwarder) drainInto(conn net.Conn) {
	w := bufio.NewWriter(conn)
	for {
		select {
		case <-f.done:
			return
		case msg := <-f.ch:
			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			payload = append(payload, '\n')
			if _, err := w.Write(payload); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./exec/... -run TestForwarder -v`
Expected: PASS

- [ ] **Step 5: Write a test proving delivery works end-to-end against a real listener**

```go
func TestForwarder_delivers_to_listening_server(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			received <- scanner.Text()
		}
	}()

	f := relayexec.NewForwarder(ln.Addr().String(), nil)
	defer f.Close()

	f.Send("main", []byte("hello\n"))

	select {
	case line := <-received:
		var msg map[string]string
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if msg["asset"] != "main" || msg["data"] != "hello\n" {
			t.Errorf("got %+v, want asset=main data=%q", msg, "hello\n")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwarder never delivered the message")
	}
}
```

Add `"bufio"` and `"encoding/json"` to the test file's imports.

- [ ] **Step 6: Run test to verify it passes**

Run: `cd services/relay && go test ./exec/... -run TestForwarder -v`
Expected: PASS (all three Forwarder tests)

- [ ] **Step 7: Commit**

```bash
git add services/relay/exec/forwarder.go services/relay/exec/forwarder_test.go
git commit -m "feat(relay/exec): add fire-and-forget Forwarder for streaming output to relay-grader"
```

---

## Task 6: Wire `Forwarder` into `exec.Backend`

**Files:**
- Modify: `services/relay/exec/backend.go`
- Test: `services/relay/exec/backend_test.go`

**Interfaces:**
- Consumes: `*Forwarder` from Task 5 (`Send(asset string, data []byte)`).

- [ ] **Step 1: Write the failing test proving `Serve` calls `Forwarder.Send` for each stdout chunk**

```go
type recordingForwarder struct {
	mu    sync.Mutex
	sends []struct {
		asset string
		data  string
	}
}

func (f *recordingForwarder) Send(asset string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, struct {
		asset string
		data  string
	}{asset, string(data)})
}

func TestBackend_forwards_stdout_chunks_to_forwarder(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	fwd := &recordingForwarder{}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
		Forwarder: fwd,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fwd.mu.Lock()
		n := len(fwd.sends)
		fwd.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fwd.mu.Lock()
	defer fwd.mu.Unlock()
	if len(fwd.sends) == 0 {
		t.Fatal("expected at least one forwarded chunk")
	}
	if fwd.sends[0].asset != "workstation" || fwd.sends[0].data != "hello from pod" {
		t.Errorf("got %+v, want asset=workstation data=%q", fwd.sends[0], "hello from pod")
	}
}

func TestBackend_nil_forwarder_does_not_panic(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
		// Forwarder intentionally nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if _, msg, err := conn.Read(ctx); err != nil || string(msg) != "hello from pod" {
		t.Fatalf("read: %v, msg=%q", err, msg)
	}
}
```

Add `"sync"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./exec/... -run "TestBackend_forwards|TestBackend_nil_forwarder" -v`
Expected: FAIL — `unknown field Forwarder in struct literal`

- [ ] **Step 3: Add a `Forwarder` interface and field to `exec.Backend`, call `Send` in the stdout loop**

In `services/relay/exec/backend.go`, add an interface (so `exec.Backend` doesn't need to import the concrete `*Forwarder` type from `cmd`, and so tests can use a fake) and a field:

```go
// forwarder abstracts Forwarder.Send so Backend can be tested without a real
// network connection. *Forwarder satisfies this interface.
type forwarder interface {
	Send(asset string, data []byte)
}

// Backend implements relaybase.Backend using kubectl exec.
// assetName == pod name — the operator ensures this invariant.
type Backend struct {
	Namespace string
	Execer    Execer
	Forwarder forwarder // optional; nil means grading is disabled for this instance
	Log       *slog.Logger // defaults to slog.Default() if nil
}
```

In the stdout→WebSocket goroutine inside `Serve`, add the forwarding call right after the existing `conn.Write`:

```go
		buf := make([]byte, 32*1024)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
				if b.Forwarder != nil {
					b.Forwarder.Send(assetName, buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
```

(This replaces the existing loop body of that goroutine — same structure, two new lines.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./exec/... -v`
Expected: PASS (full package, including pre-existing tests — `*relayexec.Forwarder` satisfies the new `forwarder` interface automatically since Go interfaces are structural)

- [ ] **Step 5: Commit**

```bash
git add services/relay/exec/backend.go services/relay/exec/backend_test.go
git commit -m "feat(relay/exec): forward stdout chunks to grader via optional Forwarder"
```

---

## Task 7: Wire `Forwarder` into `cmd/relay-exec`

**Files:**
- Modify: `services/relay/cmd/relay-exec/main.go`

**Interfaces:**
- Consumes: `exec.NewForwarder(addr string, log *slog.Logger) *exec.Forwarder` (Task 5), `exec.Backend.Forwarder` field (Task 6).

- [ ] **Step 1: Add `RELAY_GRADER_ADDR` to `config` and `loadConfig`**

```go
type config struct {
	port           string
	namespace      string
	attemptID      string
	ownerID        string
	graderAddr     string
	skipAuth       bool
	allowedOrigins []string
	logLevel       slog.Level
}
```

Inside `loadConfig()`, after the `namespace` block:

```go
	graderAddr := os.Getenv("RELAY_GRADER_ADDR") // optional — empty disables grading forwarding
```

Add `graderAddr: graderAddr,` to the returned `config{...}` literal.

- [ ] **Step 2: Construct the `Forwarder` and pass it into `Backend`**

Replace:

```go
	backend := exec.Backend{
		Namespace: cfg.namespace,
		Execer:    kubeExecer,
		Log:       slog.Default().With("namespace", cfg.namespace),
	}
```

with:

```go
	forwarder := exec.NewForwarder(cfg.graderAddr, slog.Default())
	defer forwarder.Close()

	backend := exec.Backend{
		Namespace: cfg.namespace,
		Execer:    kubeExecer,
		Forwarder: forwarder,
		Log:       slog.Default().With("namespace", cfg.namespace),
	}
```

- [ ] **Step 3: Log the grader address at startup**

```go
	slog.Info("relay-exec starting", "port", cfg.port, "skip_auth", cfg.skipAuth, "attempt_id", cfg.attemptID, "namespace", cfg.namespace, "grader_addr", cfg.graderAddr)
```

- [ ] **Step 4: Build to verify it compiles**

Run: `cd services/relay && go build ./...`
Expected: success

- [ ] **Step 5: Run the full relay test suite**

Run: `cd services/relay && make test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add services/relay/cmd/relay-exec/main.go
git commit -m "feat(relay-exec): wire optional grader Forwarder from RELAY_GRADER_ADDR"
```

---

## Task 8: `labenv-operator` — wire the new port through Deployments, Service, and NetworkPolicies

**Files:**
- Modify: `services/labenv-operator/internal/controller/grader.go`
- Modify: `services/labenv-operator/internal/controller/relay.go`
- Test: `services/labenv-operator/internal/controller/labenvironment_controller_test.go`

**Interfaces:**
- No new Go interfaces — this task only changes Kubernetes object specs the reconciler creates.

- [ ] **Step 1: Write the failing test for the grader Service's second port**

Extend the `It("creates all relay-grader resources", ...)` block in `labenvironment_controller_test.go` — add after the existing `By("Service relay-grader exists")` assertions:

```go
		By("Service relay-grader exposes the internal forwarder port")
		Expect(svc.Spec.Ports).To(ContainElement(
			corev1.ServicePort{Name: "internal", Port: 8081, TargetPort: intstr.FromInt32(8081)},
		))
```

And after the existing `By("Deployment relay-grader exists...")` env assertion:

```go
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RELAY_GRADER_INTERNAL_PORT", Value: "8081"},
		))
		Expect(deploy.Spec.Template.Spec.Containers[0].Ports).To(ContainElement(
			corev1.ContainerPort{ContainerPort: 8081},
		))
```

And after the existing `By("NetworkPolicy networkpolicy-relay-grader...")` block:

```go
		By("NetworkPolicy allows ingress from relay-exec pods on the internal port")
		var foundExecRule bool
		for _, rule := range np.Spec.Ingress {
			for _, peer := range rule.From {
				if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app"] == "relay-exec" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8081))
					foundExecRule = true
				}
			}
		}
		Expect(foundExecRule).To(BeTrue(), "expected ingress rule allowing relay-exec pods on port 8081")
```

- [ ] **Step 2: Write the failing test for relay-exec's new egress rule and env var**

Extend the `It("creates the network policy with correct shape", ...)` block under `Describe("ensureRelayNetworkPolicy", ...)` — add after the existing `By("egress contains apiserver ipBlock...")` block:

```go
		By("egress allows reaching relay-grader on port 8081")
		var foundGraderRule bool
		for _, rule := range np.Spec.Egress {
			for _, peer := range rule.To {
				if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app"] == "relay-grader" {
					Expect(rule.Ports).To(HaveLen(1))
					Expect(rule.Ports[0].Port.IntValue()).To(Equal(8081))
					foundGraderRule = true
				}
			}
		}
		Expect(foundGraderRule).To(BeTrue(), "expected egress rule allowing relay-grader on port 8081")
```

Also extend the `Describe("ensureRelayGrader", ...)` or add a new small `It` under `Describe("ensureRelayNetworkPolicy"...)`'s sibling deployment describe — since `RELAY_GRADER_ADDR` is set on the **relay-exec** Deployment, add this to whichever existing `It` covers `ensureRelayDeployment` (search first):

- [ ] **Step 3: Locate (or add) the relay-exec Deployment test and add the `RELAY_GRADER_ADDR` assertion**

Run: `grep -n "ensureRelayDeployment\|Describe(\"ensureRelay\"" services/labenv-operator/internal/controller/labenvironment_controller_test.go`

If an `It` already asserts on `relay-exec`'s Deployment env vars, add:

```go
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RELAY_GRADER_ADDR", Value: "relay-grader:8081"},
		))
```

to that block. If no such assertion exists yet, add a new `It` inside the relay-exec deployment's `Describe` block:

```go
	It("sets RELAY_GRADER_ADDR pointing at the relay-grader service", func() {
		envName := "relay-grader-addr-test"
		nsName := "rootenv-lab-" + envName
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "test-lab",
				Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
			},
		}

		Expect(os.Setenv("RELAY_EXEC_IMAGE", "relay-exec:test")).To(Succeed())
		defer func() { _ = os.Unsetenv("RELAY_EXEC_IMAGE") }()

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayDeployment(ctx, env, nsName, relayConfig{})).To(Succeed())

		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-exec"}, &deploy)).To(Succeed())
		Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
			corev1.EnvVar{Name: "RELAY_GRADER_ADDR", Value: "relay-grader:8081"},
		))
	})
```

(Place this `It` inside whichever `Describe` block currently covers `ensureRelay`/`ensureRelayDeployment` — check the grep output from this step to confirm the exact block name before inserting.)

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd services/labenv-operator && make test`
Expected: FAIL — new assertions don't find the expected env vars/ports/rules yet

- [ ] **Step 5: Implement the grader Service and Deployment changes**

In `services/labenv-operator/internal/controller/grader.go`, modify `ensureRelayGraderService`:

```go
func (r *LabEnvironmentReconciler) ensureRelayGraderService(ctx context.Context, nsName string) error {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-grader"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-grader",
			Namespace: nsName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "relay-grader"},
			Ports: []corev1.ServicePort{
				{Port: 8080, TargetPort: intstr.FromInt32(8080)},
				{Name: "internal", Port: 8081, TargetPort: intstr.FromInt32(8081)},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &svc))
}
```

Modify `ensureRelayGraderDeployment` — add the env var and container port. In the `Env` slice:

```go
								Env: []corev1.EnvVar{
									{Name: "RELAY_MY_NAMESPACE", Value: nsName},
									{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
									{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
									{Name: "RELAY_TASKS_FILE", Value: "/etc/grader/tasks.json"},
									{Name: "RELAY_GRADER_INTERNAL_PORT", Value: "8081"},
								},
```

Add a `Ports` field to the container (it has none today):

```go
								Ports: []corev1.ContainerPort{
									{ContainerPort: 8081},
								},
```

- [ ] **Step 6: Implement the grader NetworkPolicy ingress rule**

Modify `ensureRelayGraderNetworkPolicy` in `grader.go` — add a second ingress rule:

```go
func (r *LabEnvironmentReconciler) ensureRelayGraderNetworkPolicy(ctx context.Context, nsName string, cfg graderConfig) error {
	var existing networkingv1.NetworkPolicy
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "networkpolicy-relay-grader"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	tcp := corev1.ProtocolTCP
	wsPort := intstr.FromInt32(8080)
	internalPort := intstr.FromInt32(8081)

	np := networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networkpolicy-relay-grader",
			Namespace: nsName,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-grader"},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": cfg.ingressControllerNamespace,
							},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &wsPort},
					},
				},
				{
					From: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "relay-exec"},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &internalPort},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &np))
}
```

- [ ] **Step 7: Implement the relay-exec egress rule and `RELAY_GRADER_ADDR` env var**

In `services/labenv-operator/internal/controller/relay.go`, modify `ensureRelayDeployment` — add the env var:

```go
								Env: []corev1.EnvVar{
									{Name: "RELAY_MY_NAMESPACE", Value: nsName},
									{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
									{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
									{Name: "RELAY_GRADER_ADDR", Value: "relay-grader:8081"},
								},
```

Modify `ensureRelayNetworkPolicy` — add a new egress rule alongside the existing two:

```go
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// reach lab pods only (not relay-exec itself) for kubectl exec
				{
					To: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"kubernetes.io/metadata.name": nsName,
							},
						},
						PodSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{notRelayExec},
						},
					}},
				},
				// reach kube-apiserver for pods/exec API calls
				{
					To: []networkingv1.NetworkPolicyPeer{{
						IPBlock: &networkingv1.IPBlock{CIDR: apiServerCIDR},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &apiPortVal},
					},
				},
				// reach relay-grader's internal port to forward terminal output
				{
					To: []networkingv1.NetworkPolicyPeer{{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "relay-grader"},
						},
					}},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcp, Port: &graderPortVal},
					},
				},
			},
```

Add `graderPortVal := intstr.FromInt32(8081)` alongside the existing `wsPort := intstr.FromInt32(8080)` declaration near the top of `ensureRelayNetworkPolicy`.

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd services/labenv-operator && make test`
Expected: PASS

- [ ] **Step 9: Regenerate CRD/RBAC manifests if `make test`'s manifest target changed anything**

Run: `cd services/labenv-operator && make manifests && git status --short`
Expected: no unexpected diffs beyond what was already regenerated by `make test`'s dependency chain (the `test` target already depends on `manifests`/`generate`)

- [ ] **Step 10: Commit**

```bash
git add services/labenv-operator/internal/controller/grader.go services/labenv-operator/internal/controller/relay.go services/labenv-operator/internal/controller/labenvironment_controller_test.go
git commit -m "feat(labenv-operator): wire relay-exec to relay-grader's internal port via NetworkPolicy"
```

---

## Task 9: Update relay module memory

**Files:**
- Modify: `services/relay/.claude/memory/memory-architecture.md`
- Modify: `services/relay/.claude/memory/memory-decisions.md`
- Modify: `.claude/memory/memory-relay-interface.md`

**Interfaces:** None — documentation only.

- [ ] **Step 1: Add the exec→grader internal protocol to the relay interface memory**

Append a new section to `.claude/memory/memory-relay-interface.md`:

```markdown

## Internal: relay-exec → relay-grader forwarding (not client-facing)

**Port:** relay-grader listens on `RELAY_GRADER_INTERNAL_PORT` (default 8081), a plain TCP listener separate from its HTTP/WS port (8080). No auth beyond NetworkPolicy — only pods labeled `app: relay-exec` in the same namespace can reach this port.

**Protocol:** newline-delimited JSON, one message per line: `{"asset":"<assetName>","data":"<raw chunk>"}\n`. `data` is a raw, possibly mid-line, PTY output chunk — relay-exec does no line-splitting; relay-grader reassembles per-asset and splits on `\n` itself.

**Delivery guarantee:** fire-and-forget, best-effort only. relay-exec's `Forwarder.Send` never blocks and drops messages if disconnected or the internal channel (cap 256) is full. relay-exec's terminal sessions are completely unaffected by relay-grader being down, slow, or absent (`RELAY_GRADER_ADDR` unset → no-op forwarder, nothing dialed).

**Grading:** relay-grader strips ANSI escapes, keeps a 10-line ring buffer per asset, and re-runs every not-yet-passed task's compiled regex (`Task.Template`) against the relevant buffer(s) whenever new lines arrive. Asset-scoped tasks (`Task.Asset != ""`) match only that asset's buffer; lab-wide tasks match any asset's buffer. Grades are sticky (`false → true` only, never revert) and reset to all-false on relay-grader restart (in-memory only, no persistence).

**Push updates:** `/relay/grade/{attemptID}/` clients now receive more than one message per connection — an initial bootstrap snapshot, then a fresh full grade map broadcast every time any task's grade changes. Frontend's existing wholesale-replace `onmessage` handler already supports this.
```

- [ ] **Step 2: Add the architecture invariant to relay module memory**

Append to `services/relay/.claude/memory/memory-architecture.md`:

```markdown

## relay-exec Forwarder goroutine model

`exec.Backend.Serve`'s existing stdout→WS goroutine gained one more call per chunk: `b.Forwarder.Send(assetName, buf[:n])`, right after the `conn.Write` to the browser. No new goroutine in `Serve` itself.

`Forwarder` (in `exec/forwarder.go`) owns its own background goroutine (started in `NewForwarder`, only if `addr != ""`) that dials relay-grader's internal port, drains a buffered channel (cap 256) into the connection, and reconnects with exponential backoff (500ms → 10s cap) on any write/dial failure. `Send` is a non-blocking channel send with a `default: drop` case — this is the sole mechanism keeping relay-exec's hot path safe from grader unavailability.

**Critical invariant:** `Forwarder.Send` must never be changed to a blocking send or to retry synchronously — doing so would let a stuck/slow relay-grader stall real terminal sessions.
```

- [ ] **Step 3: Add the grading design decision to relay module memory**

Append to `services/relay/.claude/memory/memory-decisions.md`:

```markdown

## relay-grader — Live Grading (exec→grader forwarding)

- **Two listeners in relay-grader:** the existing HTTP mux (port 8080: frontend WS + healthz) is untouched; grading input arrives on a second, plain-TCP listener (`ServeInternalListener`, port 8081) with its own accept loop — kept separate so the frontend-facing auth/handler code (`relaybase.Handler`) needed no changes.
- **No auth on the internal link:** trust boundary is NetworkPolicy only (pods labeled `app: relay-exec` in-namespace), matching the existing exec→apiserver pattern rather than inventing a new token scheme for an internal-only hop.
- **`NewBackend` constructor added:** compiles all task regexes once (`sync.Once`-guarded `init()`), skips (logs + never matches) any task with an invalid regex instead of failing the whole grader — one bad lab definition must not take down grading for other tasks.
- **Ring buffer size fixed at 10 lines** (`ringCapacity` constant in `grader/lines.go`), per-asset, ANSI-stripped before line-splitting (stripping first avoids a rare case where an escape sequence's bytes could be mistaken for a newline boundary).
- **Sticky grades:** a task's grade only ever transitions `false → true`; matching is skipped entirely for already-passed tasks (saves recomputation, and prevents a task from "un-passing" if matching text scrolls out of the 10-line window).
- **Broadcast on change only:** `Serve` now registers/deregisters each connected client in a `map[*websocket.Conn]struct{}`; a grade change triggers a full-map broadcast to every registered client, not just the one that triggered it (multiple browser tabs on the same attempt all see updates).
```

- [ ] **Step 4: Commit**

```bash
git add .claude/memory/memory-relay-interface.md services/relay/.claude/memory/memory-architecture.md services/relay/.claude/memory/memory-decisions.md
git commit -m "docs: record relay-exec/relay-grader internal forwarding protocol and decisions"
```

---

## Final Verification

- [ ] **Step 1: Run the full relay test + build suite**

Run: `cd services/relay && make test`
Expected: PASS, all packages, `go build ./...` succeeds

- [ ] **Step 2: Run the full labenv-operator test suite**

Run: `cd services/labenv-operator && make test`
Expected: PASS

- [ ] **Step 3: Run relay lint**

Run: `cd services/relay && make lint`
Expected: no new lint errors introduced by this work (pre-existing issues, if any, are out of scope)

- [ ] **Step 4: Confirm no orphaned references to the old `&grader.Backend{...}` literal construction pattern**

Run: `grep -rn "grader.Backend{" services/relay --include=*.go`
Expected: no matches outside of the `Backend` struct's own definition in `backend.go` — all call sites use `NewBackend`
