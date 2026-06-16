# attempt-controller upstream (K8s → PocketBase sync)

**Date:** 2026-06-16  
**Branch:** feat/crd-labenv-operator  
**Scope:** `services/attempt-controller`

## Problem

The attempt-controller currently syncs one way: PocketBase attempts → Kubernetes `LabEnvironment` CRs (downstream). The reverse direction is missing. When the labenv-operator provisions pods and updates `LabEnvironment.Status`, nothing writes that state back to PocketBase — so `attempts.current_state`, `attempts.expires_at`, and `attempts.assets` remain stale.

## Goal

Add an upstream reconciler goroutine that watches `LabEnvironment` status changes and keeps the three PocketBase fields in sync:

| PocketBase field | Written from |
|---|---|
| `attempts.current_state` | `LabEnvironment.Status.Phase` |
| `attempts.expires_at` | `LabEnvironment.Status.ExpiresAt` (written once, never overwritten) |
| `attempts.assets` | `LabEnvironment.Status.Assets` (JSON array) |

## Architecture

Two goroutines in `main.go`, symmetric in structure:

```
main.go
 ├── go pb.RunAttemptSubscription(...)   ← downstream (existing)
 └── go upRec.Run(ctx, dyn)             ← upstream (new)
```

The upstream `Reconciler` is in a new package `internal/upstream`. It shares the existing `*pocketbase.Client` (concurrency-safe) and `dynamic.Interface` (concurrency-safe).

## Package layout

```
internal/upstream/
  reconcile.go   — Reconciler struct, reconcileLabEnv(), phase/asset mapping
  watcher.go     — Run(), list-then-watch using client-go ListWatch + informer
```

No new module dependencies. `k8s.io/client-go/tools/cache` is already in the transitive graph.

## K8s watching strategy

Use `cache.NewInformer` with a `cache.ListWatch` scoped to `LabEnvironmentGVR`. The informer handles:
- Initial list (calls `AddFunc` for every existing resource on start)
- Reconnect and re-list on watch error
- `resourceVersion` bookkeeping

Event handlers: `AddFunc`, `UpdateFunc`, `DeleteFunc` — each calls `reconcileLabEnv`.

### Startup resync

On the initial list the informer fires `AddFunc` for every existing `LabEnvironment`. This gives the same "reconcile current state on startup" guarantee as the downstream's `ResyncAttempts` call on reconnect.

### Periodic resync

`cache.NewInformer` accepts a `resyncPeriod`. Set to **5 minutes** (matching `fullResyncInterval` in the downstream) — the informer will re-deliver all cached objects to `UpdateFunc` periodically, self-healing any missed PocketBase writes.

## Deduplication

The `Reconciler` keeps `map[string]string` of `attemptID → lastSyncedResourceVersion`. A reconcile is skipped when the incoming object's `resourceVersion` matches the stored value. The map is accessed only from the single goroutine that drains the informer's work queue — no mutex needed.

`expires_at` is tracked separately: once written, the attempt ID is added to a `map[string]bool` and that field is never included in future PATCHes.

## Phase mapping

| `LabEnvironment.Status.Phase` | `attempts.current_state` |
|---|---|
| `Pending` | `provisioning` |
| `Degraded` | `provisioning` |
| `Ready` | `provisioned` |
| `Terminating` | `decommissioning` |
| *(Delete event)* | `decommissioned` |

Empty phase (object just created, status not yet set) → skip, wait for next event.

## Assets JSON

Each `AssetStatus` maps to one element in the `attempts.assets` JSON array:

```json
[
  {"name": "workstation", "state": "provisioned", "status": "poweredon", "protocols": ["ssh"]},
  {"name": "target",      "state": "provisioning", "status": "poweredon", "protocols": []}
]
```

Asset `state` derived from `AssetStatus.Phase`:

| `AssetStatus.Phase` | `state` |
|---|---|
| `Running` or `Succeeded` | `provisioned` |
| `Pending` | `provisioning` |
| `Terminating` | `decommissioning` |
| anything else | `pending` |

`status` is always `poweredon` — power state is managed by the `commands` queue, not the operator.  
`address` is **not** included — the relay derives it from the asset name.  
`protocols` comes from `AssetStatus.Protocols` (may be empty slice, not null).

## PocketBase write

New method on `*pocketbase.Client`:

```go
func (c *Client) PatchAttempt(ctx context.Context, id string, patch map[string]any) error
```

- `PATCH /api/collections/attempts/records/{id}`
- 401-retry pattern identical to existing `get()`
- Only fields present in `patch` are sent; caller controls which fields are included
- The `attempt-controller` service account (`svc_role = "attempt-controller"`) is already allowed by the PocketBase `updateRule` and the hook bypass

## Interface boundary

The upstream reconciler depends on a narrow interface (same pattern as downstream):

```go
type PocketBaseWriter interface {
    PatchAttempt(ctx context.Context, id string, patch map[string]any) error
}
```

This keeps the upstream testable without a live PocketBase.

## Error handling

- PATCH errors: log and continue (next periodic resync will retry)
- K8s watch errors: handled by the informer's built-in reconnect
- 401 on PATCH: handled inside `PatchAttempt` via `reauth` + retry (same as `get`)
- Delete event for an attempt already marked `decommissioned`: PATCH is still sent (idempotent)

## What is NOT in scope

- Writing `assets_configs` (connection details) — that remains contmgr's responsibility
- Writing `assets` collection records — those are created by PocketBase hooks on attempt creation
- Any change to the labenv-operator itself
- Any change to the downstream reconciler
