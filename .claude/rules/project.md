# LinuxLab Architecture

Web interface for interactive Linux learning — browse labs, provision a terminal, follow content in the browser.

## Components
- **Frontend** (`/frontend`) — Vue.js; see [frontend.md](frontend.md)
- **Backend** (`/backend`) — PocketBase; see [backend.md](backend.md)
- **Relay** (`/relay`) — Go WebSocket-to-SSH proxy; see [relay.md](relay.md)
- **Labs** (`/labs`) — YAML definitions synced into PocketBase on startup; see [labs.md](labs.md)
- **Infrastructure** (`/infra`) — Docker Compose + Nginx; see [infra.md](infra.md)

## Labs Format
- Subdirectories = groups; YAML filename (no extension) = slug. E.g. `labs/rhcsa/rhcsa-lab1.yaml` → slug `rhcsa-lab1`, group `rhcsa`
- YAML keys: `meta` (name, description), `content` (tasks with title+markdown), `environment` (server defs, never sent to frontend)

## Data Flow
```
Browser ──HTTPS──► PocketBase  (auth, labs, provision)
        ──WSS───► Relay ──validates──► PocketBase
                        ──resolves───► PocketBase (servers)
                        ──SSH────────► Lab VM
```

## Key Constraints
- One active attempt per user at a time
- Decommission + re-provision always creates a new attempt record
- Attempt records are denormalized (lab details copied at provision time)
- `environment` never exposed to frontend
- Relay is the only SSH-capable component
- Relay is stateless; multiple instances can run in parallel behind a load balancer
- Server lifecycle (start/stop/reboot) via PocketBase `commands` queue, not relay
- Lab definitions live in repo; PocketBase synced on every startup
