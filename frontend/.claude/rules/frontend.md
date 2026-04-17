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
Keep `.claude/memory/` up to date: `memory-decisions.md`, `memory-architecture.md`, `memory-preferences.md`, and root `memory-relay-interface.md` (read before implementing relay connections).

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with hot-reload
