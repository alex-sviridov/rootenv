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
At the start of any infra work, read `/home/alex/linuxlab/infra/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `/home/alex/linuxlab/infra/.claude/memory/memory-architecture.md`
- Implementation decision → `/home/alex/linuxlab/infra/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `/home/alex/linuxlab/infra/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `/home/alex/linuxlab/.claude/memory/`.
