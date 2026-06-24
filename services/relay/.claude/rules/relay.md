# Relay

**Stack:** Go — multiple binaries (one per relay type), each in its own container. Shared infrastructure in `pkg/relaybase/`.

## Responsibilities
- Accepts WebSocket connections from frontend (xterm.js)
- Validates PocketBase auth token before allowing connection
- Performs authorization check to ensure user has access to requested server (server related to attempt owned by user)
- Proxies terminal over SSH to lab VM
- uses connection json from PocketBase to determine connection details (host, port, username, private key)
- Forwards terminal output back to frontend in real time
- Forwards terminal input from frontend to lab VM in real time
- Allows multiple isolated concurrent connection, including multiple connections to the same server by same user
- Logs connection metadata (user, server, timestamps) to stdout for auditing and analytics
- Handles connection errors gracefully, sending error messages back to frontend and closing connections cleanly
- Enforces security and isolation between connections, ensuring users can only access servers they are authorized for and cannot interfere with other users' sessions
- Designed to be horizontally scalable, allowing multiple relay instances to run in parallel behind a load balancer if needed as user base grows
- Minimal latency and overhead to provide a responsive terminal experience for users interacting with their lab VMs through the web interface
- Designed for maintainability and extensibility, with clear code structure and documentation to allow future developers to easily understand and modify the relay's behavior as requirements evolve
- Provides a secure and efficient bridge between the frontend terminal interface and the backend lab VMs, enabling users to interact with their servers in real time through the web application while ensuring robust authentication, authorization, and session management.
- Hide server connection details from the frontend, only exposing a generic WebSocket interface for terminal input/output, while all connection logic and credentials management is handled securely on the backend relay service.


## Repository Structure

```
relay/
  pkg/
    pbclient/     # PocketBase REST client
    relaybase/    # Shared: Handler (exec auth), Authenticator (SSH auth), ConnLimiter, HandleHealthz, BackoffReconnector
  exec/           # package exec — kubectl exec backend (Backend struct + KubeExecer)
  ssh/            # package ssh — SSH relay handler, proxy, key decrypt, metrics
  cmd/
    relay-exec/   # Binary entry point for the exec relay (sidecar per lab environment)
    relay-ssh/    # Binary entry point for the SSH relay
```

## Connection URL (relay-ssh)
- External (via Traefik): `/relay/ssh/<serverid>/`
- Internal (what relay-ssh sees after Traefik strips `/relay/ssh`): `/<serverid>/`
- Healthz: `/relay/ssh/healthz` → `{"status":"ok","backend":"connected","active_connections":N}`

## Connection URL (relay-exec)
- External (via Traefik): `/relay/exec/<attemptID>/<assetName>/`
- Auth: injected headers `X-Attempt-Id` and `X-User-Id` (set by the operator sidecar injector, not the client)
- First WS message: discarded (placeholder for future token); auth happens via headers only
- Healthz: `/healthz` → `{"status":"ok"}`
- One relay-exec instance runs per lab environment (sidecar); `RELAY_MY_ATTEMPT_ID` and `RELAY_MY_NAMESPACE` are required env vars

## Auth message format (relay-ssh)
First WebSocket message: `<pb_token>\n<secret>`
- `pb_token`: user's PocketBase session token (validated by relaybase)
- `secret`: AES key for decrypting the SSH private key (SSH-specific, never seen by relaybase)

## Constraints
- relay-ssh: SSH connections only; only component with direct SSH access to lab VMs
- Token validated once at connection time; revalidated every 30 min (default)
- Stateless: no in-memory session or server state; multiple relay instances can run in parallel
- PocketBase admin token kept alive by `BackoffReconnector` — relay does NOT crash if backend is temporarily unavailable; active sessions are unaffected, new connections wait until backend recovers

## WebSocket Protocol

### Framing
Single control-byte prefix distinguishes control frames from stdin:

| First byte | Meaning           | Payload                                |
|------------|-------------------|----------------------------------------|
| `\x01`     | Terminal resize   | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh     | `REFRESH\n<token>` (8+ bytes)           |
| Any other  | stdin data        | Forward to SSH session as-is           |

### Resize
- Frontend sends `\x01` frame when xterm.Terminal.onResize fires (browser window resized, fitAddon.fit() called)
- Relay detects frame, extracts cols/rows, calls `session.WindowChange(rows, cols)`
- Buffered channel (capacity 1) — resize frames drop if full (last resize wins during burst)

## Memory Maintenance
At the start of any relay work, read `services/relay/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `services/relay/.claude/memory/memory-architecture.md`
- Implementation decision → `services/relay/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `services/relay/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `.claude/memory/`.
When the WebSocket interface changes (URL, auth, protocol, error behavior), also update `.claude/memory/memory-relay-interface.md` — the frontend depends on it.

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with live rebuild
