# attempt-controller

Bridges PocketBase `attempts` records to Kubernetes `LabEnvironment` custom
resources (`lab.rootenv.io/v1alpha1`, defined by `services/labenv-operator`).

## Status

**Phase 1 only: PocketBase → Kubernetes.** The controller watches PocketBase
and creates/updates/deletes `LabEnvironment` resources accordingly. The
reverse direction (syncing Kubernetes status back to PocketBase
`current_state`, `assets`, `assets_configs`) is not implemented yet.

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `ATTEMPT_CONTROLLER_BACKEND_URL` | yes | PocketBase base URL |
| `ATTEMPT_CONTROLLER_BACKEND_USERNAME` | yes | Service account email used to authenticate against PocketBase |
| `ATTEMPT_CONTROLLER_BACKEND_PASSWORD` | yes | Service account password |
| `ATTEMPT_CONTROLLER_BACKEND_TLS_VERIFY` | no | Set to `false` to skip TLS verification when connecting to PocketBase (default: verify) |

Kubernetes access uses in-cluster config when available, falling back to the
local kubeconfig for development.

## Logic

1. On startup, authenticate to PocketBase and open an SSE subscription to the
   `attempts` collection (`attempts/*?expand=lab`).
2. Once subscribed (and after every reconnect), do a full resync: fetch all
   attempts where `current_state != desired_state` and reconcile each one.
   A periodic timer (`fullResyncInterval`, 5 minutes) repeats this resync
   independently of the subscription, so transient failures self-heal.
3. For each realtime event, re-fetch the attempt with its `lab` relation
   expanded (SSE events don't carry expansions) and reconcile it.
4. Reconciling an attempt:
   - if `desired_state == decommissioned`, delete the corresponding
     `LabEnvironment` (by attempt ID);
   - otherwise, parse `attempt.expand.lab.environment` and apply (server-side
     apply, field manager `attempt-controller`) a `LabEnvironment` with
     `ownerId`, `ownerName`, `labId`, `ttl`, and `assets` derived from it.
5. If a PocketBase request gets a 401 (expired auth token), the client
   re-authenticates once and retries automatically.
