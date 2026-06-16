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
One record per lab run per user. Users can read directly (no view needed).

| Field | Type | Notes |
|-------|------|-------|
| user | relation → users | |
| lab | relation → labs | |
| lab_name | text | copied from labs.title at provision |
| current_state | select | `new` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned`; set by hooks based on assets |
| desired_state | select | `provisioned` \| `decommissioned`; set by user (or cron for expiry); contmgr reconciles toward this |
| expires_at | date | set by upstream reconciler when LabEnvironment.Status.ExpiresAt first appears; written once |
| assets | json | array of `{name, state, status, protocols}` written by upstream reconciler from LabEnvironment.Status.Assets |

Rules:
- `createRule`: `@request.auth.id = user.id`
- `listRule/viewRule`: `@request.auth.id = user.id || svc_role = relay || svc_role = contmgr`
- `updateRule`: `@request.auth.id = user.id && @request.data.current_state:isset = false`
  - Users can only patch `desired_state`. Hooks (admin context) update `current_state`.

## attempt_configs (base)
Service-only sensitive config for each attempt. Cascade-deleted when attempt is deleted.

| Field | Type | Notes |
|-------|------|-------|
| attempt | relation → attempts (cascade delete) | |
| environment | json | copied from labs.environment at provision — read by hooks to fan out assets |
| finished | date | set when current_state becomes `decommissioned` (audit timestamp) |

All rules: `svc_role = relay || svc_role = contmgr`

## assets (base)
One record per server in the lab's `environment` YAML. User-facing fields only.

| Field | Type | Notes |
|-------|------|-------|
| attempt | relation → attempts | |
| name | text | from YAML environment |
| state | select | `pending` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned` |
| status | select | `poweredon` \| `rebooting` \| `poweredoff` |
| expires_at | date | set at provision time; cron sets attempt.desired_state=decommissioned when expired |

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
Queue for server lifecycle ops (start/stop/restart). No longer used for decommission — that is driven by attempt.desired_state.

| Field | Type | Notes |
|-------|------|-------|
| asset | relation → assets | |
| command | select | `start` \| `stop` \| `restart` \| `decommission` (decommission is a no-op stub) |
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
WHERE: `attempts.current_state != 'decommissioned'` (revokes key access once attempt is done)

`listRule/viewRule`: `@request.auth.id = user.id`

## Hooks
State transitions automated in `pb_hooks/`:
- `attempts.pb.js` — before-create: validates active attempt constraint (via `attempts.current_state`), sets initial states, creates `attempt_configs` record; after-create: reads environment from `attempt_configs`, fans out assets
- `assets.pb.js` — after-update: recomputes `attempt.current_state` from all asset states; cron sets `attempt.desired_state=decommissioned` for attempts with expired assets
- `commands.pb.js` — stub; decommission no longer flows through commands
- `attempts` upstream sync — `attempt-controller` upstream reconciler watches `LabEnvironment` status and PATCHes `current_state`, `expires_at` (once), and `assets` on the attempt record; the service account `svc_role=attempt-controller` bypasses the hook's field-protection guard

## Contmgr Reconciler
Contmgr polls `attempts WHERE desired_state='decommissioned' AND current_state!='decommissioned'` and decommissions their active assets directly (no commands queue). `current_state` is always set by hooks, never by contmgr.
