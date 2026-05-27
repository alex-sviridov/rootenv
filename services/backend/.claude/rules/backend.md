# Backend

**Stack:** PocketBase (single binary) — auth, database, file serving, hooks.

## Collections
- `users` — built-in auth collection
- `labs` — synced from `labs/` on startup; not edited via UI
- `attempts` — one per lab run per user; denormalized (lab name + environment copied at provision); enforces one active attempt per user
- `servers` — one per server in lab environment YAML; linked to attempt; stores state, status, SSH details
- `commands` — queue for server lifecycle ops (start/stop/reboot); orchestrator watches and calls cloud provider API

## Server State Machine
`pending → provisioning → provisioned → decommissioning → decommissioned`

## Provision Flow
1. "Start Lab" → attempt created along with server records (`pending`)
2. `afterCreate` hook: `pending → provisioning`
3. `afterUpdate` hook: `provisioning → provisioned`
4. All servers `provisioned` → attempt `provisioned`

## Decommission Flow
1. Decommission → servers set to `decommissioning`
2. `afterUpdate` hook: `decommissioning → decommissioned`
3. All servers `decommissioned` → attempt `decommissioned`

## Current Provisioning
Mock only — hooks immediately advance state without real cloud calls.

## Memory Maintenance
At the start of any backend work, read `services/backend/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `services/backend/.claude/memory/memory-architecture.md`
- Implementation decision → `services/backend/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `services/backend/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `.claude/memory/`.
When any collection schema changes, also update `.claude/memory/memory-backend.md` — other modules depend on it.

## Dockerfiles
- `Dockerfile` — production; `Dockerfile.dev` — dev with mounted data dir
