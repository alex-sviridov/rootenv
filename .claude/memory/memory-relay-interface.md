---
description: Relay WebSocket interface contract — update whenever the relay API changes
---

# Relay WebSocket Interface

## Health Check

`GET /relay/healthz` — returns `200 ok` when ready, `503` with plain-text reason when PocketBase auth failed at startup. Frontend must check this before opening a WebSocket connection.

## Connection

**URL:** `ws(s)://<host>/relay/<serverID>/`

- `<serverID>` — PocketBase `servers` record primary key (15-char string)
- Auth token passed as `Authorization` header **or** `?token=<pb_auth_token>` query param
- Token validated once at connection time against PocketBase
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
| Token | Query param `?token=` |
| Validated against | PocketBase `/api/collections/users/auth-refresh` (or equivalent) |

## Known Constraints

- One SSH session per WebSocket connection; multiple tabs = multiple connections
- No multiplexing
- No resize message protocol documented yet — update here when implemented
