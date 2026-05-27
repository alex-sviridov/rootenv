# Frontend

**Stack:** Vue.js, PocketBase JS SDK, xterm.js, tailwind.css

## UI Structure
Lab view has three panels:
- **Sidebar**: task list + lab controls (provision/deprovision) + server controls (status, start/stop/reboot)
- **Content**: markdown render of selected task
- **Console**: tabbed; each tab is an independent SSH connection via relay

## Auth
PocketBase SDK (email/password). Auth token passed to relay on WebSocket connect.

## Relay Connection
Each console tab opens `WSS /relay/<index>/` (0-based server index). Single-server labs always use `/relay/0/`.

## Lab Browsing
Labs grouped by directory — UI mirrors `labs/` structure (subdirectory = group).

## Component Structure
- One responsibility per component; small and readable
- Shared components → `components/ui/`; view-specific → `components/<viewname>/`

## State & API
- Pinia stores and API modules are strictly separate — stores don't fetch, API modules don't import stores

## Styling
- Tailwind utility classes only; custom CSS only when Tailwind can't express it

## Testing Policy
- Write test first, then implement; feature is not done until test passes

## Memory Maintenance
At the start of any frontend work, read `services/frontend/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `services/frontend/.claude/memory/memory-architecture.md`
- Implementation decision → `services/frontend/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `services/frontend/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns (e.g. relay WebSocket interface, PocketBase collections) go to `.claude/memory/`.
Before implementing any relay connection, read `.claude/memory/memory-relay-interface.md`.

## Dockerfiles
- `Dockerfile` — production; multi-stage, `nginxinc/nginx-unprivileged:alpine`, port 8080, `USER nginx` required for DS-0002
- `Dockerfile.dev` — dev with Vite hot-reload, port 8080 (`--port 8080`), used by skaffold
- Both expose **8080** — keep in sync with k8s manifests
