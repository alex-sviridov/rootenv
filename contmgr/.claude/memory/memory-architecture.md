---
name: Contmgr Architecture
description: Non-obvious architectural details and invariants for the contmgr module
type: project
---

# Contmgr Architecture

## Encryption Contract (shared with relay)
AES-256-GCM; key = `sha256.Sum256([]byte(keys.secret))`; ciphertext stored as `base64(12-byte-nonce || ciphertext)`.
The relay uses the identical scheme to decrypt — these two must stay byte-for-byte compatible.

## PocketBase Auth
Contmgr authenticates as a regular user (`/api/collections/users/auth-with-password`), not as admin.
Service account: `svc_contmgr@contmgr.local`, `svc_role="contmgr"`.
Access rules on `assets`, `keys`, `attempts`, `commands` are gated on `svc_role = "contmgr"`.

## Per-Attempt Namespaces
Each attempt gets its own namespace `rootenv-lab-{attemptID}`.
- Namespace created at provision time with labels/annotations (user-id, lab-id, expires-at, user-email via ?expand=user)
- Pod name = `{assetName}` (no user/attempt prefix — namespace provides isolation)
- Service name = `{assetName}-svc`
- NetworkPolicy named `allow-relay` with `podSelector: {}` (applies to all pods in namespace)
- Decommission = delete individual pod+svc, then delete entire namespace when last asset is gone

## Data Flow per Asset (Kubernetes)
1. Asset enters `state=pending` (set by hook on attempt creation)
2. Contmgr reads `assets_configs.configuration` to get image, ssh_user, CPU, memory
3. Creates namespace `rootenv-lab-{attemptID}` with labels+annotations (idempotent)
4. Creates Role + RoleBinding `contmgr` in the new namespace (idempotent)
5. Creates NetworkPolicy `allow-relay` (idempotent)
6. Generates Ed25519 keypair
7. Creates Pod `{assetName}` and Service `{assetName}-svc` in `rootenv-lab-{attemptID}`
8. Waits for pod phase `Running`
9. Injects public key via pod exec: writes to `/home/{ssh_user}/.ssh/authorized_keys`
10. Fetches `keys` record → encrypts private key → patches `keys.key_encrypted`
11. Patches `assets_configs.connection` (host = service DNS, port = 22) + `.configuration` (namespace, pod, svc) → patches `assets.state=provisioned`

## NetworkPolicy (simplified)
Named `allow-relay` in each attempt namespace:
- **podSelector**: `{}` (all pods in the namespace)
- **Ingress**: from same namespace (namespaceSelector only); port 22/TCP from `rootenv-infra`
- **Egress**: to same namespace; port 53 UDP+TCP to `kube-system/kube-dns`

## Decommission Flow
1. Mark asset `decommissioning`
2. Delete pod and service by derived names (`podName(asset.Name)`, `svcName(asset.Name)`)
3. Check `ListProvisionedAssetsByAttempt` — if none remain, delete entire namespace
4. Mark asset `decommissioned`

## `assets.configuration` JSON Schema
```json
{"platform": "container", "namespace": "rootenv-lab-<attemptID>", "pod": "<assetName>", "svc": "<assetName>-svc"}
```

## Connection Info Stored in PocketBase
```json
{"host": "<svc>.<namespace>.svc.cluster.local", "port": 22, "user": "<ssh_user>"}
```
The relay dials this host:port directly — no port forwarding or host IP needed.

## RBAC Model
- ServiceAccount `contmgr` in `rootenv-infra`
- ClusterRole `contmgr-cluster` with namespace + pod/svc/netpol + role/rolebinding permissions
- ClusterRoleBinding `contmgr-cluster` binding the SA
- At namespace creation, contmgr also creates a Role + RoleBinding within each new namespace (for scoped access)

## ContMgr Never Dials SSH
ContMgr communicates with pods exclusively through the Kubernetes API (pod phase polling + exec).
It does not dial port 22 on the service or the pod IP. Only the relay does.

## Controller-Runtime Architecture (current)

```
ctrl.Manager
├── LabReconciler          — polls PocketBase every pollInterval, calls RunOnce()
│     trigger: source.Channel (startup event) + RequeueAfter
└── PodStatusController    — reacts to pod phase changes → patches PB asset status
      trigger: For(&corev1.Pod{}) via filtered informer cache
```

- Manager owns graceful shutdown (ctrl.SetupSignalHandler), /healthz + /readyz (port 8081), shared informer cache.
- `NewContmgr(*pbClient, *K8sClient, ...)` is shared between both controllers via `*Contmgr` pointer.
- Manager's `client.Client` (cache-backed) used only by PodStatusController for `Get(pod)`.
- Raw `*K8sClient` (client-go) used for all provisioning: CreatePod, WaitPodRunning, ExecInPod, etc.
- Informer cache is label-filtered: only watches pods with `app.kubernetes.io/managed-by=rootenv-contmgr`.
- Pod labels set at pod creation: `rootenv.io/asset-name` and `rootenv.io/attempt-id` (used by PodStatusController to look up PB asset).

## PID Limit via LimitRange

When `CONTMGR_PID_PER_NAMESPACE > 0`, contmgr creates a `LimitRange` named `pids` in each
attempt namespace (idempotent, alongside NetworkPolicy). Sets `type: Container`, `max.pids`,
`default.pids`, `defaultRequest.pids` to the configured value.

`config.pidLimit` ← `CONTMGR_PID_PER_NAMESPACE` env var (int64, 0 = disabled).
`Contmgr.pidLimit` field; `EnsureLimitRange(ctx, namespace, pidLimit)` in `k8sDoer`.
Deployment default: `2000`.

Note: `pids` is NOT a valid container resource in `pod.spec.containers[].resources` — it
is only valid inside a `LimitRange`. Attempting to set it in resource limits/requests causes
a pod validation error.

## Pod Security Context (current)

Every user pod gets:
- **`spec.hostUsers: false`** — kernel user-namespace remapping (K8s 1.30+)
- **`spec.securityContext.seccompProfile: RuntimeDefault`** — container runtime's default syscall filter
- **`containers[0].securityContext.capabilities.drop: [NET_RAW]`** — only NET_RAW dropped; all other caps kept for lab compatibility
- **`resources.requests`** — CPU = limit/4, memory = limit/2 (predictable scheduling)
- **`resources.limits.pids`** — default 500, overridable via `AssetDef.pids`; prevents fork bombs
- **`spec.runtimeClassName`** — set from `CONTMGR_RUNTIME_CLASS` env var (empty = unset = cluster default; "gvisor" = gVisor)

`PodParams` fields: `Pids int64`, `RuntimeClass string`.
`AssetDef` fields: `Pids int64 json:"pids"` (0 → use default 500).
`config` field: `runtimeClass string` from `CONTMGR_RUNTIME_CLASS`.

## Key Files
- `main.go` — Manager setup, controller registration
- `lab_reconciler.go` — LabReconciler (polls PB)
- `pod_controller.go` — PodStatusController (pod events → PB status) + namespaceToAttemptID()
- `contmgr.go` — business logic: RunOnce, ProvisionAsset, DecommissionAsset, UpdateAssetStatusFromPod, podPhaseToStatus
- `k8s.go` — raw k8s operations (no WatchPodStatuses — removed)
- `config.go` — env config; probeAddr (CONTMGR_PROBE_ADDR, :8081), metricsAddr (CONTMGR_METRICS_ADDR, "0")
