# Backend

**Stack:** PocketBase (single binary) — auth, database, file serving, hooks.

## Collections
- `users` — built-in auth collection
- `labs` — synced from `labs/` on startup; not edited via UI
- `attempts` — one per lab run per user; denormalized (lab name + environment copied at provision); enforces one active attempt per user
- `servers` — one per server in lab environment YAML; linked to attempt; stores state, status, SSH details
- `commands` — queue for server lifecycle ops (start/stop/reboot); orchestrator watches and calls cloud provider API

## Provision Flow
1. "Start Lab" → attempt created along with server records (`new`)
3. Hook per server `new`: mock sets server to `available`/`running` with hardcoded connection details
4. All servers `available` → attempt `provisioned`

## Decommission Flow
1. Decommission → attempt `decommissioning`
2. Hook: servers move through decommission states → `terminated`
3. All servers `terminated` → attempt `decommissioned`

## Current Provisioning
Mock only — servers skip cloud provisioning and go directly to `available`/`running`.

## Memory Maintenance
At the start of any backend work, read `/home/alex/linuxlab/backend/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `/home/alex/linuxlab/backend/.claude/memory/memory-architecture.md`
- Implementation decision → `/home/alex/linuxlab/backend/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `/home/alex/linuxlab/backend/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `/home/alex/linuxlab/.claude/memory/`.
When any collection schema changes, also update `/home/alex/linuxlab/.claude/memory/memory-backend.md` — other modules depend on it.

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with mounted data dir
