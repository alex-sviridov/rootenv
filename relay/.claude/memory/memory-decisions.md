---
description: Key architectural and implementation decisions made for the relay module
paths:
  - "relay/*"
---

# Relay Decisions

_Record decisions here as they are made — include what was decided, why, and what alternatives were rejected._

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
