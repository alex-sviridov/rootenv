# Design: labenv-operator `ensureRelayGrader`

**Date:** 2026-07-03
**Branch:** feat/relay-grader
**Status:** Approved

## Problem

`relay-grader` (bootstrapped in `services/relay/grader`) is not deployed anywhere — the labenv-operator only provisions `relay-exec`. This design adds a second `ensureRelayGrader` path to the operator so a `relay-grader` pod runs per `LabEnvironment`, reachable over WebSocket, with a placeholder task list.

Real task content (lab YAML → PocketBase → attempt-controller → CRD → `tasks.json`) does not exist anywhere in the pipeline yet and is out of scope here — this design uses a fixed placeholder `tasks.json` so the deployment/networking wiring can be built and tested independently. Threading real task data through is a separate future design.

## Pre-existing typo fix

`services/labenv-operator/config/manager/manager.yaml` already references a `RELAY_GRADER_IMAGE` env var sourced from the `relay-images` ConfigMap, but the key is misspelled `grafer` (ConfigMap actually defines `grader`, per `deploy/base/55-relay-images.yaml`). This design fixes that typo to `grader` — without it, `RELAY_GRADER_IMAGE` never resolves and the operator container fails to start.

## New File

`services/labenv-operator/internal/controller/grader.go` — package `controller`, new file alongside `relay.go`.

All grader logic lives here, mirroring how `relay.go` isolates relay-exec logic. `ensureRelayGrader` does not call or depend on any `ensureRelay*` function in `relay.go`, and vice versa.

## Resources Created (all in `nsName`)

| Function | Kind | Name |
|---|---|---|
| `ensureGraderTasksConfigMap` | `ConfigMap` | `grader-tasks` |
| `ensureRelayGraderDeployment` | `Deployment` | `relay-grader` |
| `ensureRelayGraderService` | `Service` | `relay-grader` |
| `ensureRelayGraderIngress` | `Ingress` | `relay-grader` |
| `ensureRelayGraderNetworkPolicy` | `NetworkPolicy` | `networkpolicy-relay-grader` |

Same create-once pattern as `ensureRelay`: `Get` → if `NotFound`, `Create` → `IgnoreAlreadyExists`. No updates on reconcile.

No ServiceAccount, Role, or RoleBinding — relay-grader makes no Kubernetes API calls (unlike relay-exec, which needs `pods`/`pods/exec` for `kubectl exec`). It uses the namespace's default ServiceAccount implicitly (no `serviceAccountName` set on the pod spec).

## Config (new env vars, read in `loadGraderConfig`)

| Var | Required | Default | Purpose |
|---|---|---|---|
| `RELAY_GRADER_IMAGE` | yes | — | Image for the relay-grader container. `ensureRelayGrader` returns error if missing (same pattern as `RELAY_EXEC_IMAGE`). |
| `RELAY_GRADER_INGRESS_BASE_PATH` | no | `/relay/grade` | Path prefix. Full path = `<base>/<envName>`. |

`ingressClass`, `ingressAnnotations` (including the auth middleware annotation), and `ingressControllerNamespace` are reused as-is from the existing `loadRelayConfig()` — no duplication, grader's ingress config function takes the same `relayConfig` struct plus its own base path.

## Resource Specs

### ConfigMap (`grader-tasks`)
```yaml
data:
  tasks.json: |
    [{"id": "task1", "type": "term", "template": "echo hi"}]
```
Static placeholder, identical content for every LabEnvironment. Not derived from `env.Spec` in this design.

### Deployment (`relay-grader`)
- `replicas: 1`
- `selector`/pod labels: `app: relay-grader`
- Pod security context: same as relay-exec — `runAsNonRoot: true`, `runAsUser: 10001`, `seccompProfile: RuntimeDefault`
- No `serviceAccountName` override
- Volume: `grader-tasks` ConfigMap mounted read-only at `/etc/grader`
- Container security context: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities: drop: ["ALL"]`
- Env:
  - `RELAY_MY_NAMESPACE=<nsName>`
  - `RELAY_MY_ATTEMPT_ID=<env.Name>`
  - `RELAY_MY_OWNER_ID=<env.Spec.OwnerId>`
  - `RELAY_TASKS_FILE=/etc/grader/tasks.json`
- `imagePullPolicy: IfNotPresent`
- ReadinessProbe: `httpGet /healthz` on port 8080
- Resources: requests `cpu: 20m` / `memory: 64Mi`, limits `cpu: 50m` / `memory: 96Mi` (same as relay-exec — grader does no exec workload, this is generous)

### Service (`relay-grader`)
- ClusterIP
- Port 8080 → targetPort 8080
- Selector: `app: relay-grader`

### Ingress (`relay-grader`)
- Separate `Ingress` object from relay-exec's `relay` Ingress — fully independent, so `ensureRelayGrader` has no create-once coupling with `ensureRelay`
- Path: `<RELAY_GRADER_INGRESS_BASE_PATH>/<envName>`, `PathType: Prefix`
- Backend: Service `relay-grader`, port 8080
- `ingressClassName`: set if `RELAY_INGRESS_CLASS` is non-empty (shared config)
- Annotations: same auth-middleware annotation as relay-exec's Ingress (shared config)

### NetworkPolicy (`networkpolicy-relay-grader`)
- `podSelector`: `app: relay-grader`
- PolicyTypes: `Ingress`, `Egress` (both declared)
- Ingress: allow from namespace `kubernetes.io/metadata.name: <ingressControllerNamespace>`, port 8080/TCP (same shape as relay-exec's ingress rule)
- Egress: **zero rules** — deny-all. relay-grader loads tasks into memory at startup and serves a WebSocket; it makes no outbound calls (not even DNS) after start, so no egress is needed.

## Call Site

In `reconcileCreate`, alongside the existing `ensureRelay` call:

```go
if err := r.ensureRelay(ctx, env, nsName); err != nil {
    return ctrl.Result{}, err
}
if err := r.ensureRelayGrader(ctx, env, nsName); err != nil {
    return ctrl.Result{}, err
}
```

## RBAC Markers

No new RBAC markers needed for ServiceAccount/Role/RoleBinding (grader has none). Existing markers for `configmaps`, `deployments`, `services`, `ingresses`, `networkpolicies` already cover the new resource kinds created here (ConfigMap is new — add):

```go
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create
```

## What Is Not Changed

- `labenvironment_types.go` — no CRD schema changes; no `tasks` field added
- `attempt-controller` — no changes; tasks are not sourced from PocketBase/lab YAML in this design
- Lab YAML format — unchanged
- `relaybase`, `grader` package Go code — unchanged (already bootstrapped)
- The existing `relay` Ingress / relay-exec resources — untouched by this work

## Testing

- Extend `labenvironment_controller_test.go` (Ginkgo) with cases mirroring the existing relay-exec suite:
  - `ensureRelayGrader` creates ConfigMap, Deployment, Service, Ingress, NetworkPolicy
  - Returns error when `RELAY_GRADER_IMAGE` is missing
  - Idempotent on second reconcile (no duplicate-create errors)
- `BeforeEach` sets `RELAY_GRADER_IMAGE` alongside the existing `RELAY_EXEC_IMAGE` test env var
