---
name: Relay WebSocket interface
description: Relay WebSocket interface contract — update whenever the relay API changes
type: reference
---

# Relay WebSocket Interface

## Health Check

`GET /relay/ssh/healthz` — returns JSON 200 when ready, JSON 503 before first PocketBase auth succeeds.

```json
// 200
{"status":"ok","backend":"connected","active_connections":3}

// 503
{"status":"starting","backend":"connecting","active_connections":0}
```

Frontend fetches this before opening a WebSocket connection.

## Connection

**URL:** `ws(s)://<host>/relay/ssh/<serverID>/`

- `<serverID>` — PocketBase `assets` record primary key (15-char string)
- No token in URL or headers — token is sent as the **first WebSocket message** after `onopen`
- Relay reads the first message (10s timeout), validates against PocketBase, closes with 1008 if invalid
- Authorization: relay fetches `assets` record and asserts `server.user == tokenUserID`

## First Message Format (relay-ssh)

`<pb_token>\n<secret>`
- `pb_token`: user's PocketBase session token
- `secret`: AES key for decrypting the server's SSH private key (SSH-specific, opaque to relaybase)

## Protocol

### Message Framing

Raw binary WebSocket frames with control-byte prefix:

| First byte | Meaning           | Payload                                |
|------------|-------------------|----------------------------------------|
| `\x01`     | Terminal resize   | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh     | `REFRESH\n<token>` (8+ bytes)           |
| Any other  | stdin data        | Forward to SSH as-is                   |

### Stdin/Stdout
Plain bytes (first byte ≠ `\x00` or `\x01`) are forwarded to SSH as terminal input.

## Error Handling

Connection is closed by the relay on:
- Invalid or expired token
- Server ID not found
- Server belongs to another user
- SSH connection failure

## Token Refresh

Relay closes with code 1002 + reason `"session expired"` when the token expires.

Client can proactively send in-band: `\x00REFRESH\n<token>` — relay validates and updates without reconnecting.

## Routing (Traefik)

- External: `/relay/ssh/<serverID>/` → Traefik strips `/relay/ssh` → relay-ssh sees `/<serverID>/`
- Each relay type gets its own Traefik IngressRoute + strip-prefix middleware
- Healthz: `/relay/ssh/healthz` → relay-ssh sees `/healthz`

## Known Constraints

- One SSH session per WebSocket connection; multiple tabs = multiple connections
- No multiplexing
- Backend unavailability does not crash relay; new connections queue until backend recovers; active sessions are unaffected

## Internal: relay-exec → relay-grader forwarding (not client-facing)

**Port:** relay-grader listens on `RELAY_GRADER_INTERNAL_PORT` (default 8081), a plain TCP listener separate from its HTTP/WS port (8080). No auth beyond NetworkPolicy — only pods labeled `app: relay-exec` in the same namespace can reach this port.

**Protocol:** newline-delimited JSON, one message per line: `{"asset":"<assetName>","data":"<raw chunk>"}\n`. `data` is a raw, possibly mid-line, PTY output chunk — relay-exec does no line-splitting; relay-grader reassembles per-asset and splits on `\n` itself.

**Delivery guarantee:** fire-and-forget, best-effort only. relay-exec's `Forwarder.Send` never blocks and drops messages if disconnected or the internal channel (cap 256) is full. relay-exec's terminal sessions are completely unaffected by relay-grader being down, slow, or absent (`RELAY_GRADER_ADDR` unset → no-op forwarder, nothing dialed).

**Grading:** relay-grader strips ANSI escapes, keeps a 10-line ring buffer per asset, and re-runs every not-yet-passed task's compiled regex (`Task.Template`) against the relevant buffer(s) whenever new lines arrive. Asset-scoped tasks (`Task.Asset != ""`) match only that asset's buffer; lab-wide tasks match any asset's buffer. Grades are sticky (`false → true` only, never revert) and reset to all-false on relay-grader restart (in-memory only, no persistence).

**Push updates:** `/relay/grade/{attemptID}/` clients now receive more than one message per connection — an initial bootstrap snapshot, then a fresh full grade map broadcast every time any task's grade changes. Frontend's existing wholesale-replace `onmessage` handler already supports this.
