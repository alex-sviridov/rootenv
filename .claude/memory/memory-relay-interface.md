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

### Message Framing

Raw binary WebSocket frames with control-byte prefix to distinguish control frames from stdin data:

| First byte | Meaning           | Payload                                |
|------------|-------------------|----------------------------------------|
| `\x01`     | Terminal resize   | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh     | `REFRESH\n<token>` (8+ bytes)           |
| Any other  | stdin data        | Forward to SSH as-is                   |

### Stdin/Stdout
Relay proxies stdin/stdout of an SSH session directly. Plain bytes (first byte ≠ `\x00` or `\x01`) are forwarded to SSH as terminal input.

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

## Token Refresh

If the relay detects that the token has expired during a session (via periodic revalidation), it closes the connection with:
- Close code: 1002 (Policy Violation)
- Reason: `"session expired"`

The frontend should:
1. Detect close code 1002 + reason "session expired"
2. Call `pb.collection('users').authRefresh()` to get a fresh token
3. Reconnect to the relay with the new token

Alternative: Client can proactively send an in-band token refresh message (format: `\x00REFRESH\n<token>`) without closing the connection. Relay will validate the token and update its internal state without reconnecting.

## Known Constraints

- One SSH session per WebSocket connection; multiple tabs = multiple connections
- No multiplexing
