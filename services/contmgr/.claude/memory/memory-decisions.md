---
name: Contmgr Decisions
description: Key architectural and implementation decisions made for the contmgr module
type: project
---

# Contmgr Decisions

## Initial Design

- **Polling over event-driven:** PocketBase hooks can't push to external services reliably; polling every 5s is simple and sufficient at this scale.
- **Ed25519 keypairs:** Smaller keys, faster generation, modern standard. Generated in-memory — never touch disk.
- **AES-256-GCM for key storage:** Authenticated encryption; nonce prepended to ciphertext in single base64 blob for simplicity. Key derived via SHA-256 of the PocketBase-generated `secret` field.
- **Public key injection via pod exec:** Avoids building custom images; works with any image that has a shell.
- **`errgroup` for concurrency:** Errors per asset are logged but don't abort the poll cycle — one bad pod shouldn't block others.
- **No HTTP server in contmgr:** Pure worker; observability via stdout logs only.

## controller-runtime Migration (feat_contmgr-isolate-aetup)

- **controller-runtime v0.20.4 adopted:** Replaces manual ticker loop + goroutine statusWatcher. Aligns with k8s.io/* v0.32.x already in use.
- **Two controllers under one Manager:** `LabReconciler` (polls PB, provisions/decommissions via RunOnce) + `PodStatusController` (reacts to pod phase changes → patches PB asset status).
- **LabReconciler trigger via source.Channel:** No primary k8s resource to watch — a pre-loaded buffered channel fires once on startup; `RequeueAfter: pollInterval` sustains the loop. `labReconcileKey = {Name: "global"}` is the synthetic fixed request.
- **PodStatusController uses `For(&corev1.Pod{})` + informer cache:** Replaces the raw `Watch()` reconnect loop. `IsNotFound` in `Reconcile` means pod deleted → status "stopped". Controller-runtime work queue provides natural event coalescing (no sync.Map dedup cache needed).
- **Informer cache filtered by label:** `cache.ByObject` with `app.kubernetes.io/managed-by=rootenv-contmgr` — manager only indexes contmgr-owned pods, avoiding cluster-wide watch.
- **Raw K8sClient kept for exec/wait:** Manager's `client.Client` is used only by PodStatusController for `Get(pod)`. All provisioning ops (CreatePod, WaitPodRunning, ExecInPod) still use raw client-go K8sClient (SPDY executor requires raw rest.Config).
- **Leader election OFF (stateless design preserved):** Multiple instances can run in parallel — flag `CONTMGR_LEADER_ELECT` added but defaults to false.
- **health.go deleted:** `/healthz` and `/readyz` provided by Manager at port 8081 (`CONTMGR_PROBE_ADDR`). Plain "ok" response — no JSON body. Sufficient for k8s liveness probes.
- **WatchPodStatuses removed from k8sDoer interface:** Informer cache replaces the low-level Watch call; interface is cleaner.
- **`podPhaseToStatus` moved to contmgr.go:** Shared by `UpdateAssetStatusFromPod` (business logic). Takes `string` not `corev1.PodPhase` — simpler to call from both the controller and tests.
- **`UpdateAssetStatusFromPod` returns nil on asset-not-found:** Orphaned pods cause lookup failure; returning nil avoids retry churn (controller-runtime would backoff on non-nil error).

## Pod Security Hardening

- **`NET_RAW` only dropped (not ALL caps):** Users are root by design; RHCSA/networking labs need `SYS_ADMIN`, `NET_ADMIN`, etc. `NET_RAW` blocks raw socket abuse (ping floods, ARP spoofing) with minimal lab impact.
- **`readOnlyRootFilesystem: false` intentionally:** Pods run SSH + interactive sessions; writable rootfs is required.
- **`runAsNonRoot: false` intentionally:** Users are meant to be root inside the container.
- **AppArmor skipped:** `seccompProfile: RuntimeDefault` covers the same kernel surface. Custom AppArmor profiles require per-node distribution — high ops cost for marginal gain.
- **`hostUsers: false` (user namespace remapping):** Root in container maps to unprivileged UID on host. Requires K8s 1.30+ with user namespace feature gate. Huge host-escape mitigation almost for free.
- **PID limit default 500:** Prevents fork bombs. Overridable per-asset via `AssetDef.pids` JSON field (0 → default). Stored as `"pids"` in the container resource limits list.
- **Resource requests at 25% CPU / 50% memory of limits:** Predictable scheduling; scheduler no longer treats pods as burstable-to-zero. No ephemeral storage request (causes scheduling issues).
- **`CONTMGR_RUNTIME_CLASS` env var for gVisor:** Empty = cluster default, `"gvisor"` = gVisor userspace kernel. Activates for all user pods with no code change. Default off — requires cluster-level gVisor install.
- **`K8sClient.clientset` typed as `kubernetes.Interface`:** Changed from `*kubernetes.Clientset` to allow `fake.NewSimpleClientset()` in tests without reflection hacks.
- **ContMgr own pod hardened in deploy manifest:** `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, `seccompProfile: RuntimeDefault`, `capabilities.drop: [ALL]`.

## Kubernetes Migration (replacing Docker)

- **ClusterIP Service per pod:** Relay dials the stable DNS name; pod restarts (if any) don't change the address. No NodePort or host port mapping needed.
- **`restartPolicy: Never` for user pods:** Lab pods should not auto-restart on SSH disconnect or crash — they require a full reprovision cycle. Matches original Docker behavior.
- **`imagePullPolicy: IfNotPresent`:** Images come from an external registry; pull on first use, cache thereafter.
- **NetworkPolicy with `NamespaceSelector` + `PodSelector` together:** Using `PodSelector` alone would match pods with the same labels in any namespace. The combined selector scopes pod-to-pod traffic to `rootenv-users` only.
- **Removed egress to `rootenv-infra` from NetworkPolicy:** Pods never initiate connections to infra — the relay dials in. Unnecessary and potentially over-permissive.
- **Added DNS egress (port 53 to `kube-system`):** Required for pods to resolve hostnames via `kube-dns`. Without this, inter-pod name resolution fails when NetworkPolicy blocks all egress.
- **`user_id` stored in `assets.configuration`:** `GetAsset` doesn't expand the attempt relation, so `UserID` is empty when called during decommission. Storing it in configuration avoids an extra PB round-trip.
- **RBAC Role scoped to `rootenv-users` only (not ClusterRole):** ContMgr has no reason to touch infra resources. Least privilege.
- **ContMgr never dials SSH:** Readiness check is pod phase `Running` only. The relay is the sole SSH client. This avoids contmgr needing network access to the pod's port 22.
