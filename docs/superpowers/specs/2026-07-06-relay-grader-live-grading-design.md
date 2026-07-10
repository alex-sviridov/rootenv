# relay-grader live grading design

**Date:** 2026-07-06
**Branch:** feat/relay-grader
**Scope:** `services/relay/exec`, `services/relay/grader`, `services/relay/cmd/relay-exec`, `services/relay/cmd/relay-grader`, `services/labenv-operator/internal/controller`

## Problem

`relay-grader` currently loads `tasks.json` and, on every frontend connection, reports every task as `false` — there is no grading logic (`services/relay/grader/backend.go`). Real grading requires the actual terminal output the user produces via `relay-exec`, but the two are separate pods today with no data path between them, and `relay-grader`'s NetworkPolicy has zero egress / ingress-only-from-ingress-controller.

This design wires `relay-exec` to stream terminal output to `relay-grader`, which matches each task's `template` regex against a rolling per-asset buffer and pushes updated grades to connected frontend clients.

**Out of scope:**
- Any change to the browser↔relay-exec WebSocket protocol or terminal proxying behavior.
- Persisting grades across `relay-grader` restarts (in-memory only, matches existing bootstrap model).
- Any change to `labs-sync`, exercise placeholder parsing, or the frontend grader wiring already built (`useGraderConnection.js`).
- Grading types other than `"term"` (only supported type today).

## Priority Constraint

`relay-exec` must work identically whether `relay-grader` is up, down, or slow. The exec→grader link is strictly fire-and-forget: no blocking, no backpressure onto the exec/WebSocket hot path, silent drop under any failure.

## Architecture

```
relay-exec pod                              relay-grader pod
┌─────────────────────────┐                 ┌──────────────────────────────┐
│ Backend.Serve (per       │                 │ internal listener :8081      │
│ session, per assetName)  │                 │  (forwarder connections)     │
│                          │   TCP :8081     │                              │
│ stdout chunk ──┬─► WS ───┼───────not this──┤                              │
│                │         │                 │                              │
│                └─► forwarder.Send(         │  per-asset:                  │
│                     asset, chunk)          │   strip ANSI → split \n      │
│                     (non-blocking,         │   → ring buffer (10 lines)   │
│                      bounded chan)         │   → re-run unpassed task     │
│                          │                 │     regexes against buffer   │
│  forwarder goroutine:    │   NDJSON        │   → sticky grades map        │
│   dial/reconnect loop,   ├────────────────►│   → on change: broadcast     │
│   drains chan, writes    │   {"asset":..,  │     full grade map to all    │
│   {"asset","data"}\n     │    "data":..}\n │     connected frontend WS    │
│   drops on error/full    │                 │     clients (existing        │
└─────────────────────────┘                 │     /relay/grade/{id}/ WS)   │
                                             └──────────────────────────────┘
```

## 1. `relay-exec` → `relay-grader` forwarding

New file: `services/relay/exec/forwarder.go`

```go
type Forwarder struct {
    addr string        // RELAY_GRADER_ADDR, e.g. "relay-grader:8081"
    ch   chan forwardMsg // bounded, e.g. cap 256
    log  *slog.Logger
}

type forwardMsg struct {
    Asset string `json:"asset"`
    Data  string `json:"data"` // raw chunk, string(buf[:n])
}
```

- `NewForwarder(addr string, log *slog.Logger) *Forwarder` — starts one background goroutine (`run`) that owns a single persistent TCP connection to `addr`, reconnecting with backoff (reuse `relaybase.BackoffReconnector` if its interface fits; otherwise a small local backoff loop — simple exponential, capped, matching existing style) when the connection drops.
- `Send(asset string, data []byte)` — **non-blocking**: `select { case ch <- forwardMsg{...}: default: /* drop, log at Debug */ }`. Never called from a path that can be slowed by this.
- When connected, `run` drains `ch`, marshals each message to JSON, writes `+ "\n"` to the socket. Any write error closes the connection and re-enters the reconnect loop; in-flight message is dropped (fire-and-forget, no retry of the same message).
- If `addr` is empty (env var unset), `NewForwarder` returns a no-op `Forwarder` whose `Send` is a no-op — grading is optional infrastructure, relay-exec must start and run fine without it (e.g. local dev via `RELAY_SKIP_AUTH=true` with no grader present).

**Wiring into `Backend`:** `exec.Backend` gains a `Forwarder *Forwarder` field (nil-safe: `Serve` checks `if b.Forwarder != nil { b.Forwarder.Send(assetName, buf[:n]) }` right next to the existing `conn.Write` call in the stdout→WS loop in `backend.go`). No new goroutines added to `Serve` — reuses the existing stdout-reading goroutine, just adds one extra non-blocking send.

**main.go (`cmd/relay-exec`):** reads `RELAY_GRADER_ADDR` (optional — empty means no-op forwarder). Constructs `exec.NewForwarder(addr, log)` once at startup, passes into `exec.Backend`.

## 2. `relay-grader` internal forwarder listener

New file: `services/relay/grader/forwarder_listener.go` (or add to `backend.go` — small enough to co-locate, final call made during implementation).

- A second `http.Server`-free raw TCP listener on a new port (`RELAY_GRADER_INTERNAL_PORT`, default `8081`), separate from the existing `:8080` HTTP mux (frontend WS + healthz stay on 8080).
- Accepts connections from `relay-exec` pods only (enforced by NetworkPolicy, not application auth — see below). Multiple relay-exec pods could theoretically connect; each connection is handled independently, all writing into the same `Backend` state.
- Per connection: `bufio.Scanner` (or `Reader.ReadString('\n')`) reading NDJSON lines, unmarshal into `forwardMsg{Asset, Data}`, call `Backend.Ingest(asset, []byte(data))`. Malformed lines are skipped (never crash the listener — same defensive posture as the existing "ignore malformed grader WS frames" fix on the frontend side).
- Connection drop/EOF: listener goroutine exits cleanly, no impact on other connections or on the frontend-facing side.

## 3. Grading state in `grader.Backend`

Extend `services/relay/grader/backend.go`:

```go
type Backend struct {
    Tasks []Task
    Log   *slog.Logger

    mu      sync.Mutex
    regexes map[string]*regexp.Regexp   // compiled once, keyed by task ID
    assets  map[string]*assetBuffer     // keyed by assetName
    grades  map[string]bool             // sticky, keyed by task ID
    clients map[*websocket.Conn]struct{} // connected frontend clients for broadcast
}

type assetBuffer struct {
    partial []byte      // incomplete trailing line from last chunk
    lines   []string    // ring buffer, cap 10, most-recent last
}
```

- **Compile once:** at construction (or lazily on first `Ingest`/guarded by `sync.Once`), compile every task's `Template` via `regexp.Compile`. A task with an invalid regex is logged and skipped (never matches) — must not crash the grader over one bad lab definition.
- **`Ingest(asset string, chunk []byte)`:**
  1. Lock, fetch/create `assetBuffer` for `asset`.
  2. Append `chunk` to `partial`; strip ANSI escapes (`\x1b\[[0-9;]*[a-zA-Z]` compiled once at package level) from the combined buffer before splitting — stripping before line-splitting avoids an escape sequence itself containing a literal newline byte confusing the split (rare but possible with some cursor-save sequences; stripping first is simplest and matches "what a human reads").
  3. Split combined buffer on `\n`; all complete segments become new lines pushed into the ring (cap 10, drop oldest); the final incomplete segment becomes the new `partial`.
  4. If at least one new line was added, re-run matching (step below).
  5. Unlock.
- **Matching:** for every task where `grades[task.ID] != true`:
  - `task.Asset == asset` (asset-scoped, matches this buffer) OR `task.Asset == ""` (lab-wide — check against every asset's buffer, not just the one that just updated, since a lab-wide task could match on an asset that isn't currently being typed into): join that asset's `lines` with `\n`, run the compiled regex, `MatchString`.
  - On first match: `grades[task.ID] = true`, mark `changed = true`.
  - After the loop, if `changed`, broadcast (below).
- **Broadcast:** marshal the full `grades` map (same shape as today's bootstrap message), write to every conn in `clients` (best-effort — write errors just drop that client from the map, matching existing `Serve`'s "onclose: log only" client-side posture; a broadcast write failure must not affect other clients or `Ingest` callers).

## 4. Frontend-facing `Serve` changes

`Backend.Serve` (the existing `/relay/grade/{attemptID}/` WS handler) changes from "send once, then idle-read-until-close" to:

1. Register `conn` in `b.clients` (locked).
2. Send current `grades` snapshot immediately (bootstrap behavior preserved for a freshly-connecting client).
3. Block on `conn.Read` loop as before (still just waiting for close — no incoming messages expected from frontend).
4. On exit (any reason): deregister `conn` from `b.clients` (locked), return.

This is the only behavior change visible to the frontend: it may now receive **more than one** grade message over the connection's lifetime. The existing frontend code already handles this correctly per the wiring design (`onmessage: grades.value = JSON.parse(e.data)` — replaces the whole map each time) — no frontend changes needed.

## 5. Config & wiring (`labenv-operator`)

`services/labenv-operator/internal/controller/relay.go` (`ensureRelayDeployment`):
- Add env var `RELAY_GRADER_ADDR` = `"relay-grader:8081"` (grader Service already exists; add port 8081 to its `ServicePort` list alongside existing 8080).

`services/labenv-operator/internal/controller/grader.go`:
- `ensureRelayGraderService`: add a second `ServicePort` (`8081` → container port `8081`).
- `ensureRelayGraderDeployment`: add env var `RELAY_GRADER_INTERNAL_PORT=8081`; add container port 8081.
- `ensureRelayGraderNetworkPolicy`: add one ingress rule — `From: pods labeled app=relay-exec` (in-namespace, via `PodSelector` only, no `NamespaceSelector` needed since same namespace), `Ports: [8081]`. Existing ingress-controller rule (port 8080) unchanged. `Egress` stays `[]` — grader never initiates outbound connections.

`services/labenv-operator/internal/controller/relay.go` (`ensureRelayNetworkPolicy`):
- Add one egress rule — `To: pods labeled app=relay-grader` (in-namespace), `Ports: [8081]`. Existing egress rules (lab pods, apiserver) unchanged.

## What is NOT changed

- Browser↔relay-exec WebSocket protocol, resize framing, auth — untouched.
- `/relay/grade/{attemptID}/` external URL, auth pattern (injected headers, first-message discard) — untouched.
- `LoadTasks`/`tasks.json` schema — untouched.
- Grades are never persisted; a `relay-grader` restart resets all buffers and grades to zero. Acceptable: sticky-pass semantics mean a user who already passed will need to re-trigger the matching text after a restart (rare event, matches existing no-persistence model).
- No frontend changes required (existing wholesale-replace `onmessage` handler already supports repeated messages).

## Testing

- `exec/forwarder_test.go`: `Send` never blocks when disconnected or channel full (assert via timing/goroutine count); reconnect loop recovers after a listener restart; no-op forwarder (`addr == ""`) is safe to call.
- `exec/backend_test.go`: extend existing tests — `Backend.Serve` with a non-nil `Forwarder` calls `Send` for each stdout chunk; a nil `Forwarder` (unset) doesn't panic; forwarder failures never affect the WS stdout path (use a `Forwarder` test double whose `Send` panics/errors to prove `Serve` is unaffected... actually: `Send` has no error return by design, so the test proves `Serve`'s behavior is identical with forwarder present vs. absent).
- `grader/backend_test.go`: extend existing tests —
  - `Ingest` splits multi-chunk input across the `\n` boundary correctly (chunk boundary mid-line).
  - ANSI stripping: a chunk containing color codes still matches a plain-text regex.
  - Ring buffer caps at 10 lines, drops oldest.
  - Asset-scoped task only matches its own asset's buffer; lab-wide task matches across assets.
  - Sticky grade: once true, further `Ingest` calls that would no longer match don't flip it back.
  - Invalid regex in a task: logged, skipped, doesn't crash `Ingest` or other tasks' matching.
  - Broadcast: a second `Serve`-connected client receives an unprompted grade update after a matching `Ingest` call; a broadcast write error to one client doesn't affect another.
- `grader/forwarder_listener_test.go`: malformed NDJSON line is skipped, doesn't close the connection; valid lines feed `Ingest`.
- `labenv-operator` envtest: extend existing `ensureRelayGrader`/`ensureRelay` coverage — assert the new port, env var, and NetworkPolicy rule appear on both sides.
