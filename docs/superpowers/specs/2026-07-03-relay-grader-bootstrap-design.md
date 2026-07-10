# relay-grader bootstrap design

**Date:** 2026-07-03
**Branch:** feat/relay-grader
**Scope:** `services/relay`

## Problem

Lab tasks currently have no way to report completion back to the frontend — tasks are free-form markdown with an embedded `**Task:**` instruction and no structured pass/fail signal. This is the first step toward automated grading: stand up a new relay type, `relay-grader`, that establishes the WebSocket contract and task loading, without implementing any actual grading logic yet.

## Goals

- New `relay-grader` binary, structurally consistent with `relay-exec` (reuses `relaybase.Handler`, same config/lifecycle conventions)
- Attempt-scoped WebSocket endpoint that returns the task list with a `grade` flag for each
- Loads `tasks.json` (mounted at pod creation, like other per-attempt config) into memory at startup
- Fast, minimal binary — no grading logic in this pass

## Non-goals

- Actual grading logic (running checks, changing `grade` from `false` to `true`)
- Re-grade-on-demand protocol messages
- k8s/labenv-operator wiring to actually inject the relay-grader sidecar or mount `tasks.json`
- PocketBase integration (writing grades back, etc.)

## Architecture

```
Browser
  │  wss://<host>/relay/grade/<attemptId>/
  │  (auth via injected headers, same as relay-exec)
  ▼
relay-grader pod (one per LabEnvironment namespace, future work)
  │  relaybase.Handler: WS accept → discard first message → validate
  │    X-Attempt-Id == MY_ATTEMPT_ID, X-User-Id non-empty → acquire conn slot
  │  grader.Backend.Serve:
  │    writes {"<taskId>": false, ...} for all loaded tasks
  │    blocks reading WS (idle) until client disconnects or ctx cancelled
  ▼
tasks.json (mounted file, path from RELAY_TASKS_FILE)
  loaded once at process startup
```

## `relaybase.Handler` change: optional `assetName`

Today `ServeHTTP` does:
```go
assetName := strings.Trim(r.PathValue("assetName"), "/")
if assetName == "" {
    http.Error(w, "missing asset name", http.StatusBadRequest)
    return
}
```

This 400s any route without an `{assetName}` path segment. relay-grader's route (`/relay/grade/{attemptID}/`) has no asset segment — grading is attempt-scoped, not asset-scoped.

Change: only enforce the "must be non-empty" check when the route pattern actually declares `{assetName}`. Simplest implementation — drop the empty check entirely and let `Backend.Serve` receive `""` when the route has no such segment. `exec.Backend.Serve` and `ssh` backends are unaffected since their routes always populate it; grader's `Serve` ignores the parameter.

No other change to `Handler` — auth (`X-Attempt-Id`/`X-User-Id`), first-message discard, connection limiting, and WaitGroup draining all reused as-is.

## `tasks.json` schema and loading

```json
[
  {"id": "task1", "type": "term", "template": "..."},
  {"id": "task2", "type": "term", "template": "..."}
]
```

- `id`, `type`, `template` all required strings.
- `type` must currently be `"term"` — any other value is a startup validation error.
- `template` is parsed and held in memory; unused until grading logic is implemented.
- Loaded once at startup from the path in `RELAY_TASKS_FILE`. Missing file, invalid JSON, or failed validation → log error, `os.Exit(1)` (same pattern as `loadConfig` in relay-exec).

## WebSocket protocol

**Connect:** `wss://<host>/relay/grade/<attemptId>/`

**On successful auth**, server immediately sends one JSON text message:
```json
{"task1": false, "task2": false}
```
Object keyed by task `id`, value is the `grade` bool (always `false` in this bootstrap).

**After sending:** connection stays open. Server loop reads from the WebSocket and discards input, doing nothing, until the client disconnects or the request context is cancelled (mirrors relay-exec's read-goroutine-driven shutdown). No further messages are sent by the server in this pass.

## Repository layout

```
services/relay/
  grader/
    backend.go       — grader.Backend implementing relaybase.Backend; task loading + Serve
    backend_test.go
    Dockerfile
  pkg/relaybase/
    handler.go        — assetName made optional (see above)
    handler_test.go    — new test case: route without {assetName}
  cmd/
    relay-grader/
      main.go          — loadConfig + wiring, mirrors cmd/relay-exec/main.go
```

## Config (env vars)

```
RELAY_MY_ATTEMPT_ID    — required unless RELAY_SKIP_AUTH=true (same as relay-exec)
RELAY_MY_OWNER_ID      — required unless RELAY_SKIP_AUTH=true
RELAY_MY_NAMESPACE     — required (kept for consistency with relay-exec; unused by grader logic today)
RELAY_TASKS_FILE       — required; path to mounted tasks.json
RELAY_LISTEN_PORT      — default 8080
RELAY_SKIP_AUTH        — default false
RELAY_ALLOWED_ORIGINS  — comma-separated, optional
LOG_LEVEL              — debug/info, default info
```

## Binary behavior

- `/healthz` → `{"status":"ok"}`
- `--healthcheck` CLI flag (same as relay-exec: curls localhost healthz, exits 0/1)
- Graceful shutdown: 30s drain window via `WaitGroup`, same as relay-exec
- Route: `mux.Handle("/relay/grade/{attemptID}/", handler)`

## Testing

- `grader/backend_test.go`: black-box test driving `Backend.Serve` through `httptest.Server` + `websocket.Dial` — asserts the JSON response shape and that the connection stays open until client close.
- Task-loading tests: valid file, missing file, invalid JSON, invalid `type` value.
- `pkg/relaybase/handler_test.go`: new case mounting the handler on a mux pattern without `{assetName}`, verifying it no longer 400s and `Backend.Serve` receives `""`.

## What is NOT in scope

- Actually running any check/grading logic — `grade` is always `false`
- labenv-operator changes to deploy relay-grader or mount `tasks.json`
- Frontend changes to consume the new endpoint
- PocketBase writes for grade results
