---
description: Key architectural and implementation decisions made for the relay module
paths:
  - "relay/*"
---

# Relay Decisions

_Record decisions here as they are made — include what was decided, why, and what alternatives were rejected._

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
