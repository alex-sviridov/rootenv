---
description: PocketBase collections schema — update this file whenever collections change
type: project
rule: Always read this file before writing any backend migration, hook, or API handler that touches collections
---
# PocketBase Collections

## users (auth)
| Field | Type | Notes |
|-------|------|-------|
| id | text | auto 15 chars |
| email | email | required |
| emailVisibility | bool | |
| verified | bool | |
| name | text | |
| avatar | file | image types only |
| password / tokenKey | hidden | auth internals |

## labs (base)
Synced from `labs/` YAML on every backend startup. Do not edit via UI.

| Field | Type | Notes |
|-------|------|-------|
| id | text | slug, pattern `^[A-Za-z0-9_]+$` |
| type | select | `folder` \| `lab` |
| title | text | |
| description | text | |
| content | json | array of `{title, content}` tasks |
| environment | json | array of server defs — never sent to frontend |
| parent | relation → labs | optional |

## labs_userview (view)
Selects `id, title, description, content, parent, type` from `labs` — omits `environment`.

## attempts (base)
One record per lab run per user. Denormalized at provision time.

| Field | Type | Notes |
|-------|------|-------|
| user | relation → users | |
| lab | relation → labs | |
| lab_name | text | copied from labs.title at provision |
| finished | date | set when all servers decommissioned |

Constraint: at most one active attempt per user.

## attempts_userview (view)
Selects `id, created, updated, finished, user, lab_name, lab, state`.  
`state` is computed from linked servers:
- `new` — no servers yet
- `provisioning` — any server in `provisioning`
- `provisioned` — all servers `provisioned`
- `decommissioning` — any server in `decommissioning`
- `decommissioned` — all servers `decommissioned` OR `finished` is set

`listRule`: `@request.auth.id = user.id`

## servers (base)
One record per server in the lab's `environment` YAML.

| Field | Type | Notes |
|-------|------|-------|
| attempt | relation → attempts | |
| name | text | from YAML environment |
| state | select | `pending` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned` |
| status | select | `poweredon` \| `rebooting` \| `poweredoff` |
| connection | json | `{user, host, port, privateKey}` — SSH details |

## servers_userview (view)
Selects `s.id, s.name, a.user, s.status, s.state, a.id as attempt_id` from servers where `state != 'decommissioned'`.

## commands (base)
Queue for server lifecycle ops. Watched by hooks.

| Field | Type | Notes |
|-------|------|-------|
| server | relation → servers | |
| command | select | `start` \| `stop` \| `restart` \| `decommission` |
| status | select | `pending` → `running` → `done` \| `error` |

## Hooks
State transitions automated in `pb_hooks/`: `servers.pb.js`, `attempts.pb.js`, `commands.pb.js`.  
Migrations source of truth: `backend/pb_migrations/`.
