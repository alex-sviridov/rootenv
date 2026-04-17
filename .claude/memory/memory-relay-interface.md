---
description: Relay WebSocket interface contract — update whenever the relay API changes
---

# Relay WebSocket Interface

## Connection

**URL:** `ws(s)://<host>/relay/<index>/`

- `<index>` — 0-based server index matching order in `environment.servers`; single-server labs always use `0`
- Auth token passed as query parameter: `?token=<pb_auth_token>`
- Token validated once at connection time against PocketBase

## Protocol

Raw binary WebSocket frames — relay proxies stdin/stdout of an SSH session directly. No framing protocol on top; xterm.js writes/reads raw bytes.

## Error Handling

Connection is closed by the relay on:
- Invalid or expired token
- No active attempt found for the user
- Server index out of range
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
