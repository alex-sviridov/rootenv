# Relay

**Stack:** Go — single long-running process. Docker containrized.

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


## Connection URL
`/relay/<serverid>/` — server id, primary key in table servers

## Constraints
- SSH connections only (no exec or other transports)
- Only component with direct SSH access to lab VMs
- Token validated once at connection time
- Stateless: no in-memory session or server state; all active attempt/server data lives in PocketBase, so multiple relay instances can run in parallel behind a load balancer

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
At the start of any relay work, read `/home/alex/linuxlab/relay/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `/home/alex/linuxlab/relay/.claude/memory/memory-architecture.md`
- Implementation decision → `/home/alex/linuxlab/relay/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `/home/alex/linuxlab/relay/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `/home/alex/linuxlab/.claude/memory/`.
When the WebSocket interface changes (URL, auth, protocol, error behavior), also update `/home/alex/linuxlab/.claude/memory/memory-relay-interface.md` — the frontend depends on it.

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with live rebuild
