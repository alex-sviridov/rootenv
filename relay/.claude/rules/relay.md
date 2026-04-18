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
At the start of any relay work, read `/home/alex/linuxlab/relay/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `/home/alex/linuxlab/relay/.claude/memory/memory-architecture.md`
- Implementation decision → `/home/alex/linuxlab/relay/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `/home/alex/linuxlab/relay/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `/home/alex/linuxlab/.claude/memory/`.
When the WebSocket interface changes (URL, auth, protocol, error behavior), also update `/home/alex/linuxlab/.claude/memory/memory-relay-interface.md` — the frontend depends on it.

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with live rebuild
