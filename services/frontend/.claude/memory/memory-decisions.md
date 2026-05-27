---
description: Key architectural and implementation decisions made for the frontend module
paths:
  - "frontend/*"
---

# Frontend Decisions

## Shared pb instance via `src/lib/pb.js`
Extracted PocketBase instantiation into a shared module so auth state is visible across all API modules. Rejected: each module owning its own instance (breaks auth token sharing).

## `withLoading` as a private store helper
Duplicated once per store rather than extracted to a shared utility. Keeps stores self-contained; two occurrences don't justify an abstraction.

## Validation in stores, not API modules
Client-side input validation (required fields, email format, password min length, confirmation match) lives in store actions — they are the user-input boundary. API modules stay pure.

## `signUp` auto-logs in
`signUp` calls `register` then `login` in sequence and sets `user` directly. Rejected: register-only (worse UX, forces a separate login step).

## Separate `historyLoading` for paginated secondary data
When a store loads both a primary record (e.g. `lastAttempt`) and a paginated list (e.g. `history`), use a second `withHistoryLoading` helper with its own `historyLoading` flag so the main UI is not blocked by history fetches. Both helpers share the same `error` ref. Added in `useAttemptsStore`.

## LabConsole tab state ownership
Tab state (`tabs`, `activeTabId`) lives in LabView, not LabConsole. LabConsole is a pure display component — it receives tabs as props and emits `select-tab`/`close-tab` back up. This lets LabView control which tabs exist as servers are decommissioned.

## TerminalPanel uses v-show, not v-if
All tab panels are mounted once and toggled with `v-show` so the WebSocket connection stays alive when switching tabs. `v-if` would teardown/recreate the WS on every switch.

## Relay WebSocket URL and auth
`${proto}://${location.host}/relay/${serverId}/` — uses `location.host` so it works in both dev (through nginx-dev proxy) and production without config. Token is sent as the first WebSocket message (plain text frame) after `onopen`, not in the URL, per the relay interface contract in `memory-relay-interface.md`.

## xterm.js integration
`@xterm/xterm` + `@xterm/addon-fit` for terminal rendering. Status messages (connecting, errors, disconnection) are written to the terminal buffer itself via `terminal.writeln()` — not HTML overlays — so they render like real terminal output. WebSocket binary frames are passed as `Uint8Array` to `terminal.write()`. `FitAddon` resizes the terminal to fill its container; `ResizeObserver` on the terminal DOM element ensures fit updates when the sidebar/panels resize. `cursorBlink: true`, `cursorStyle: 'block'`, `fontSize: 12` with Tailwind slate theme.

## Canonical container port: 8080
Both dev (Vite `--port 8080`) and prod (nginx-unprivileged) run on 8080. All k8s manifests (Deployment containerPort, Service, Ingress) and skaffold use 8080. Never use 5173 in manifests — that was the old default before alignment.

## Production Dockerfile: nginxinc/nginx-unprivileged + explicit USER
`Dockerfile` uses `nginxinc/nginx-unprivileged:alpine` (runs as uid 101 by default). `USER nginx` must be declared explicitly even though the image already sets it — static scanners (Trivy/Hadolint DS-0002) check for the instruction in the file, not the runtime user.

## Test mocking strategy
API tests mock `@/lib/pb` directly. Store tests mock `@/api/*`. Using `vi.hoisted()` for mock variables referenced inside `vi.mock()` factories.
