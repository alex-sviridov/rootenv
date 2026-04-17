---
description: PocketBase collections schema — update this file whenever collections change
originSessionId: e043a70b-f5ff-4efc-9520-44425301d31d
---
# PocketBase Collections

## users
Built-in PocketBase auth collection. No custom fields documented yet.

## labs
Synced from `labs/` YAML files on every backend startup. Not editable via PocketBase UI.

| Field | Type | Notes |
|-------|------|-------|
| id | text | group/slug |
| title | text | from `meta.title` |
| description | text | from `meta.description` |
| content | json | array of `{title, content}` tasks |
| environment | json | array of `{servername}` servers |

## labs_userview
User accessible view based on labs without environment shown.

| Field | Type | Notes |
|-------|------|-------|
| id | text | group/slug |
| title | text | from `meta.title` |
| description | text | from `meta.description` |
| content | json | array of `{title, content}` tasks |

## attempts
One record per lab run per user. Denormalized — lab name and environment copied at provision time.

| Field | Type | Notes |
|-------|------|-------|
| user | relation → users | |
| lab | relation → labs | |
| lab_name | text | copied at provision |
| environment | json | copied at provision; never exposed to frontend |

Constraint: at most one active attempt per user.

## userview_attempts

| Field | Type | Notes |
|-------|------|-------|
| user | relation → users | |
| lab_name | text | copied at provision |
| state | select | `new` (all servers are `new`) → `provisioning` (any server is `provisioning`) → `provisioned` (all servers are `provisoned`) → `decommissioning` (any server is `decommissioning`) → `decommissioned` (all servers are `decommissioned`)|

## servers
One record per server defined in the lab's `environment` YAML. Linked to an attempt.

| Field | Type | Notes |
|-------|------|-------|
| attempt | relation → attempts | |
| name | text | server identifier from YAML |
| state | select | `new` → `provisioning` → `provisioned` → `decommissioning` → `decommissioned` |
| status | text | `poweredon`, `rebooting`, `poweredoff` |
| connection | text | SSH user@host:port |
| key | text | SSH private key |

## commands
Queue for server lifecycle operations. Watched by an orchestrator process.

| Field | Type | Notes |
|-------|------|-------|
| server | relation → servers | |
| command | select | `start` / `stop` / `reboot` |
| status | select | `pending` → `running` → `done` / `error` |
