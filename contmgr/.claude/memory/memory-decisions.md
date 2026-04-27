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
