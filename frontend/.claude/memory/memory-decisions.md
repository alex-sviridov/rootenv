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

## Relay WebSocket URL in TerminalPanel
`${proto}://${location.host}/relay/${serverId}/?token=${pb.authStore.token}` — uses `location.host` so it works in both dev (through nginx-dev proxy) and production without config. Token passed as query param because browser WebSocket API does not support custom headers.

## Test mocking strategy
API tests mock `@/lib/pb` directly. Store tests mock `@/api/*`. Using `vi.hoisted()` for mock variables referenced inside `vi.mock()` factories.
