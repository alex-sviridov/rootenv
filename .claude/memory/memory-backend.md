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
| svc_role | text | `relay`, `attempt-controller` for service accounts |
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
| environment | json | lab environment spec (`{duration, assets:[...]}`) — never sent to frontend |
| exercises | json | array of `{id, description, type, asset?, template}`, written by labs-sync.py only; never exposed via `labs_userview` |
| parent | relation → labs | optional |

Rules:
- `listRule`: null (no one can list; frontend uses `labs_userview`)
- `viewRule`: `@request.auth.svc_role = 'attempt-controller'` — attempt-controller expands this via attempts

## labs_userview (view)
Selects `id, title, description, content, parent, type` from `labs` — omits `environment`.

`listRule/viewRule`: `""` (public)

## attempts (base)
One record per lab run per user.

| Field | Type | Notes |
|-------|------|-------|
| user | relation → users | |
| lab | relation → labs | |
| lab_name | text | copied from `labs.title` at provision time by before-create hook |
| current_state | select | `new` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned`; written by attempt-controller upstream reconciler |
| desired_state | select | `provisioned` \| `decommissioned`; set by user action |
| expires_at | date | set by upstream reconciler when `LabEnvironment.Status.ExpiresAt` first appears; written once |
| assets | json | array of `{name, state, status, protocols}` written by upstream reconciler from `LabEnvironment.Status.Assets` |

Rules:
- `createRule`: `@request.auth.id = user.id`
- `listRule/viewRule`: `@request.auth.id = user.id || @request.auth.svc_role = 'attempt-controller'`
- `updateRule`: `@request.auth.id = user.id || @request.auth.svc_role = 'attempt-controller'`
  - Users can only patch `desired_state`. `attempt-controller` may patch `current_state`, `expires_at`, `assets` (before-update hook enforces this).

## Hooks
`pb_hooks/attempts.pb.js`:
- **before-update**: blocks regular users from writing `current_state`, `expires_at`, or `assets`; `attempt-controller` svc_role bypasses
- **before-create**: validates one-active-attempt constraint; copies `labs.title` → `lab_name`; sets `current_state=new`, `desired_state=provisioned`

No after-create hook. No `attempt_configs`. The attempt-controller reads `labs.environment` directly via `?expand=lab` on the attempts API.

## Removed collections
`attempt_configs`, `assets` (base table), `assets_configs`, `commands`, `keys`, `keys_userview` — all removed as part of the labenv-operator refactor. State is now tracked in `attempts.assets` (json) and managed by the attempt-controller upstream reconciler.
