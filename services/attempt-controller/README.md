# attempt-controller

Bridges PocketBase `attempts` records and Kubernetes `LabEnvironment` custom
resources (`lab.rootenv.io/v1alpha1`, defined by `services/labenv-operator`)
in both directions.

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

Two goroutines run concurrently after startup.

### Downstream (PocketBase → Kubernetes)

1. Authenticate to PocketBase and open an SSE subscription to the `attempts`
   collection (`attempts/*?expand=lab`).
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

### Upstream (Kubernetes → PocketBase)

1. On startup, list all existing `LabEnvironment` CRs and reconcile each one
   to PocketBase. A periodic resync (5 minutes) repeats this independently.
2. Watch the `LabEnvironment` resource stream for Add/Update/Delete events and
   reconcile on each event.
3. Reconciling a `LabEnvironment` (whose name is the PocketBase attempt ID):
   - writes `attempts.current_state` from `Status.Phase`:
     `Pending`/`Degraded` → `provisioning`, `Ready` → `provisioned`,
     `Terminating` → `decommissioning`;
   - writes `attempts.expires_at` from `Status.ExpiresAt` once (never
     overwritten on subsequent reconciles);
   - writes `attempts.assets` as a JSON array `[{name, state, status, protocols}]`
     derived from `Status.Assets` on every reconcile.
4. On a Delete event, sets `attempts.current_state` to `decommissioned`.
5. Deduplication: reconciles with the same `resourceVersion` as the last
   successful sync are skipped to avoid redundant PocketBase writes.
