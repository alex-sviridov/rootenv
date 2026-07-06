---
description: Key architectural and implementation decisions made for the relay module
paths:
  - "relay/*"
---

# Relay Decisions

_Record decisions here as they are made — include what was decided, why, and what alternatives were rejected._

## relay-exec — Binary Size & Performance

- **No `kubernetes.Clientset`:** `exec/kube.go` builds the pod exec URL manually (`/api/v1/namespaces/{ns}/pods/{pod}/exec?...`) instead of using `kubernetes.Clientset` + `kubernetes/scheme` + `corev1.PodExecOptions`. Those packages register all 30+ API group types at init time, adding ~12 MB to the binary. Only `k8s.io/client-go/rest` and `k8s.io/client-go/tools/remotecommand` are imported.
- **Transport cached at startup:** `KubeExecer` calls `spdy.RoundTripperFor(cfg)` once in `NewKubeExecer` and stores `host`, `transport`, `upgrader`. Each `Exec` call reuses them via `remotecommand.NewSPDYExecutorForTransports` — avoids re-parsing TLS config per session.
- **Build flags:** both Dockerfiles use `-ldflags="-s -w"` (strip debug info + DWARF). Result: 37 MB → 14 MB (62% reduction). Both Dockerfiles now use `golang:1.26-alpine` (was `1.24` in `exec/Dockerfile`).
- **`LOG_LEVEL` env:** relay-exec now reads `LOG_LEVEL=debug` (matching relay-ssh). Wired into `slog.HandlerOptions.Level`.

## relay-exec — Logging & Config

- **Backend.Log field:** `exec.Backend` carries a `*slog.Logger` (defaults to `slog.Default()` if nil). Set in `main.go` with `namespace` pre-baked as a field. `KubeExecer.Exec` has no logging — errors bubble up to `Backend.Serve` which logs them.
- **Session log lines:** `relaybase.Handler` logs `"ws connected"` and `"ws disconnected"` at the connection level. `Backend.Serve` logs `"exec session ended with error"` only on error — no duplicate success line.
- **loadConfig():** `relay-exec/main.go` now has a `loadConfig()` function returning a `config` struct (matching relay-ssh pattern). Config validation exits via `return config{}, false` — `main` calls `os.Exit(1)` centrally.
- **Handler tests:** `handler_test.go` mounts the handler on a real `http.ServeMux` (pattern `/relay/exec/{attemptID}/{assetName}/`) so `PathValue("assetName")` is populated. Tests were previously broken because the handler was called directly without a mux.

## Iteration 4 — Terminal Resize

- **Framing protocol:** Single control-byte prefix on WebSocket messages — `\x01` for resize (cols uint16 LE, rows uint16 LE), `\x00` for token refresh, else stdin.
- **Resize handling:** New goroutine in `runProxy` consumes `resizeChan` and calls `session.WindowChange(rows, cols)`. Testable via `windowChangeFn` callback (defaults to `session.WindowChange`).
- **Frontend:** `terminal.onResize` triggers binary frame send. `fitAddon.fit()` called on connect + `window.addEventListener('resize')` to fit on browser resize.
- **Capacity:** `resizeChan` buffered to 1; resize frames drop if channel full (last resize wins during burst).

## Iteration 3 — SSH Proxying

- **SSH auth:** public-key only via `RELAY_PRIVATE_KEY_PATH`; `InsecureIgnoreHostKey` used intentionally — lab VMs are ephemeral and network-isolated (Docker/VPC is the trust boundary).
- **PTY:** `xterm-256color`, 24×80 initial size. Resize deferred to Iteration 4.
- **Backpressure:** SSH stdout → `chan []byte` with capacity 512; if the channel is full for >10 s the connection is dropped with `StatusGoingAway`. Generous buffer handles `cat large_file` without OOM.
- **Idle timeout:** `RELAY_IDLE_TIMEOUT` (default 30 m); timer reset on every WebSocket read. Closes with `StatusGoingAway`.
- **Rate limiting:** token-bucket on WebSocket→SSH stdin only (1 MB/s sustained, 256 KB burst). SSH→WS output is never rate-limited to avoid stream corruption.
- **Goroutine layout:** three goroutines per session — SSH stdout→chan, chan→WS write, WS read→SSH stdin. Main `runProxy` blocks on idle timer / ctx cancel.
- **`min` helper:** defined in `proxy.go` for Go <1.21 compatibility (module is `go 1.26` but kept explicit for clarity).
- **pbclient test fix:** `TestNewWithCredentials_success` and `TestNewWithCredentials_badPassword` mocked `/api/admins/auth-with-password` but the client calls `/api/collections/users/auth-with-password` — corrected in Iteration 3.

## Iteration 2 — Auth & Authorization

- **pbclient package:** `pkg/pbclient` — thin HTTP client wrapping PocketBase REST API. No PocketBase SDK dep; plain `net/http`.
- **Admin auth:** relay authenticates as admin via `RELAY_BACKEND_USERNAME` / `RELAY_BACKEND_PASSWORD` at startup using `/api/admins/auth-with-password`. Admin token stored on `Client` struct; used for `GetServer` / `GetAttempt` queries.
- **Token validation:** user token validated via `POST /api/collections/users/auth-refresh` with the token in `Authorization` header — PB returns user record with `id` on success, 401/403 on failure.
- **Auth token delivery:** relay accepts token in `Authorization` header or `?token=` query param (some WS clients can't set headers).
- **CORS:** `InsecureSkipVerify: true` on `websocket.Accept` — CORS enforced upstream by nginx, not the relay.
- **WS library:** `nhooyr.io/websocket` — idiomatic Go, no gorilla dep.
- **Route pattern:** `/{serverID}/` using Go 1.22 `http.ServeMux` path value — no router dep needed.
- **Config env vars renamed:** `POCKETBASE_URL` still accepted as fallback; canonical names now `RELAY_BACKEND_URL`, `RELAY_BACKEND_USERNAME`, `RELAY_BACKEND_PASSWORD`.
- **`New` vs `NewWithCredentials`:** `New(baseURL, adminToken)` for tests with a pre-baked token; `NewWithCredentials` for production (calls PB auth on construction).

## Iteration 1 — Skeleton & Wiring

- **Port:** relay listens on `PORT` env var, defaults to `8080`.
- **Config:** flat `loadConfig()` in `main.go` reads `PORT`, `POCKETBASE_URL` (default `http://backend:8090`), `LOG_LEVEL` (`debug`/`info`).
- **Logger:** `log/slog` with JSON handler — structured output, no third-party dep.
- **Live-reload:** `air` with `.air.toml`; builds to `./tmp/relay`.
- **Nginx strip-prefix:** `/relay/` proxied as `proxy_pass http://relay/` — nginx strips the prefix, so relay handlers are rooted at `/` (e.g., `/healthz`, future `/{serverid}/`).
- **WebSocket timeout:** `proxy_read_timeout 3600s` on the `/relay/` block to keep long-lived WS connections alive.
- **Graceful shutdown:** 30-second drain window on `SIGINT`/`SIGTERM`.

## relay-grader — Live Grading (exec→grader forwarding)

- **Two listeners in relay-grader:** the existing HTTP mux (port 8080: frontend WS + healthz) is untouched; grading input arrives on a second, plain-TCP listener (`ServeInternalListener`, port 8081) with its own accept loop — kept separate so the frontend-facing auth/handler code (`relaybase.Handler`) needed no changes.
- **No auth on the internal link:** trust boundary is NetworkPolicy only (pods labeled `app: relay-exec` in-namespace), matching the existing exec→apiserver pattern rather than inventing a new token scheme for an internal-only hop.
- **`NewBackend` constructor added:** compiles all task regexes once (`sync.Once`-guarded `init()`), skips (logs + never matches) any task with an invalid regex instead of failing the whole grader — one bad lab definition must not take down grading for other tasks.
- **Ring buffer size fixed at 10 lines** (`ringCapacity` constant in `grader/lines.go`), per-asset, ANSI-stripped before line-splitting (stripping first avoids a rare case where an escape sequence's bytes could be mistaken for a newline boundary).
- **Sticky grades:** a task's grade only ever transitions `false → true`; matching is skipped entirely for already-passed tasks (saves recomputation, and prevents a task from "un-passing" if matching text scrolls out of the 10-line window).
- **Broadcast on change only:** `Serve` now registers/deregisters each connected client in a `map[*websocket.Conn]struct{}`; a grade change triggers a full-map broadcast to every registered client, not just the one that triggered it (multiple browser tabs on the same attempt all see updates).
