# Infrastructure

**Stack:** Docker Compose + Nginx reverse proxy. Each component has `Dockerfile` (prod) and `Dockerfile.dev` (hot-reload) at its module root.

## Compose Files
- `docker-compose.yml` — production
- `docker-compose.dev.yml` — development

## Nginx Routing (single port, path-based)
| Path | Target |
|------|--------|
| `/api/` | PocketBase |
| `/relay/` | Relay (WebSocket) |
| `/` | Frontend |

## Memory Maintenance
Keep `.claude/memory/` up to date: `memory-decisions.md`, `memory-architecture.md`, `memory-preferences.md`.
