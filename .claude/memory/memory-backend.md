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
| svc_role | text | `relay` or `contmgr` for service accounts |
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
| environment | json | copied from labs.environment at provision |
| finished | date | set when all assets decommissioned |

Rules: `listRule/viewRule` = relay only. `createRule` = `@request.auth.id = user.id`.

## attempts_userview (view)
Selects `id, created, updated, finished, user, lab_name, lab, state`.
`state` is computed from linked assets:
- `new` — no assets yet
- `provisioning` — any asset pending/provisioning
- `provisioned` — all assets provisioned
- `decommissioning` — any asset decommissioning
- `decommissioned` — all assets decommissioned OR `finished` is set

`listRule/viewRule`: `@request.auth.id = user`

## assets (base)
One record per server in the lab's `environment` YAML. User-facing fields only.

| Field | Type | Notes |
|-------|------|-------|
| attempt | relation → attempts | |
| name | text | from YAML environment |
| state | select | `pending` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned` |
| status | select | `poweredon` \| `rebooting` \| `poweredoff` |
| expires_at | date | set at provision time; cron creates decommission command when expired |

Rules:
- `listRule`: `@request.auth.id = attempt.user.id` — owner can list AND subscribe
- `viewRule`: `@request.auth.id = attempt.user.id || svc_role = relay || svc_role = contmgr`
- `updateRule`: `svc_role = contmgr`
- `createRule`: null (hooks create as admin)

## assets_configs (base)
Service-only sensitive config for each asset. Cascade-deleted when asset is deleted.

| Field | Type | Notes |
|-------|------|-------|
| asset | relation → assets (cascade delete) | |
| connection | json | `{host, port, user}` — SSH connection details; written by contmgr after provisioning |
| configuration | json | `{platform, pod, svc, user_id}` — runtime config; written by contmgr after provisioning |
| platform | text | `container` |

All rules: `svc_role = relay || svc_role = contmgr`

## commands (base)
Queue for server lifecycle ops. Watched by hooks and contmgr.

| Field | Type | Notes |
|-------|------|-------|
| asset | relation → assets | |
| command | select | `start` \| `stop` \| `restart` \| `decommission` |
| status | select | `pending` → `running` → `done` \| `error` |

`createRule`: `@request.auth.id = asset.attempt.user.id && status = 'pending'`

## keys (base)
SSH key material per asset. Service-only.

| Field | Type | Notes |
|-------|------|-------|
| asset | relation → assets (cascade delete) | |
| secret | text | 32-char alphanumeric; auto-generated; used as AES key material |
| key_encrypted | text | AES-256-GCM encrypted private key (base64); written by contmgr |

Rules: `svc_role = relay || svc_role = contmgr`

## keys_userview (view)
Joins keys → assets → attempts to expose `secret` to the asset's owner.
Fields: `id, asset, attempt, user, secret`

`listRule/viewRule`: `@request.auth.id = user.id`

## Hooks
State transitions automated in `pb_hooks/`:
- `assets.pb.js` — creates assets + assets_configs + keys records on attempt create; checks attempt finished on asset decommission; cron decommissions expired assets
- `attempts.pb.js` — validates active attempt constraint; populates lab_name + environment from labs on create
- `commands.pb.js` — sets asset state to `decommissioning` when decommission command created
