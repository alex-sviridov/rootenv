---
description: Relay WebSocket interface contract — update whenever the relay API changes
---

# Relay WebSocket Interface

## Health Check

`GET /relay/healthz` — returns `200 ok` when ready, `503` with plain-text reason when PocketBase auth failed at startup. Frontend must check this before opening a WebSocket connection.

## Connection

**URL:** `ws(s)://<host>/relay/<serverID>/`

- `<serverID>` — PocketBase `servers` record primary key (15-char string)
- No token in URL or headers — token is sent as the **first WebSocket message** after `onopen`
- Relay reads the first message (10s timeout), validates it against PocketBase, closes with 1008 if invalid
- Authorization: relay fetches `servers` record, then its linked `attempts` record, and asserts `attempt.user == tokenUserID`

## Protocol

Raw binary WebSocket frames — relay proxies stdin/stdout of an SSH session directly. No framing protocol on top; xterm.js writes/reads raw bytes.

## Error Handling

Connection is closed by the relay on:
- Invalid or expired token
- No active attempt found for the user
- Server ID not found
- Server belongs to another user's attempt
- SSH connection failure

No structured error payload — close event with a WebSocket close code is the signal.

## Headers / Auth

| Mechanism | Detail |
|-----------|--------|
| Token | First WebSocket message after `onopen` (plain text frame) |
| Validated against | PocketBase `/api/collections/users/auth-refresh` |
| Auth timeout | 10 seconds — relay closes if no message received |

## Known Constraints

- One SSH session per WebSocket connection; multiple tabs = multiple connections
- No multiplexing
- No resize message protocol documented yet — update here when implemented
