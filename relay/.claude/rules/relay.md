# Relay

**Stack:** Go — single long-running process.

## Responsibilities
- Accepts WebSocket connections from frontend (xterm.js)
- Validates PocketBase auth token before allowing connection
- Resolves target: looks up user's active attempt, selects server by URL index
- Proxies terminal over SSH to lab VM

## Connection URL
`/relay/<index>/` — 0-based server index. Single-server labs always use `/relay/0/`.

## Constraints
- SSH connections only (no exec or other transports)
- Only component with direct SSH access to lab VMs
- Token validated once at connection time
- Stateless: no in-memory session or server state; all active attempt/server data lives in PocketBase, so multiple relay instances can run in parallel behind a load balancer

## Memory Maintenance
Keep `.claude/memory/` up to date: `memory-decisions.md`, `memory-architecture.md`, `memory-preferences.md`, and root `memory-relay-interface.md` (update when WebSocket interface changes: URL, auth, protocol, error behavior).

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with live rebuild
