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

## UserID Population
`ListPendingAssets` uses `?expand=attempt` to populate `Asset.UserID` from the attempt relation.
`GetAsset` does NOT expand — so assets fetched during decommission have `UserID=""`.
**Fix:** `user_id` is stored in `assets.configuration` JSON at provision time and read back during decommission as a fallback.

## Data Flow per Asset (Kubernetes)
1. Asset enters `state=pending` (set by hook on attempt creation)
2. Contmgr reads `asset.Configuration` to get image, ssh_user, CPU, memory
3. Creates NetworkPolicy `{userID}-{attemptID}-netpol` (idempotent — skips if already exists)
4. Generates Ed25519 keypair
5. Creates Pod `{userID}-{attemptID}-{assetName}` and Service `{userID}-{attemptID}-{assetName}-svc` in `rootenv-users`
6. Waits for pod phase `Running` (polls every 1s, up to 60s)
7. Injects public key via pod exec: `sh -c "mkdir -p /conf.d/authorized_keys && printf '%s' <key> > /conf.d/authorized_keys/<user>"`
8. Fetches `keys` record → encrypts private key → patches `keys.key_encrypted`
9. Patches `assets.connection` (host = service DNS, port = 22) + `assets.configuration` (pod, svc, user_id) → patches `assets.state=provisioned`

## Per-Attempt NetworkPolicy
Each attempt gets a NetworkPolicy in `rootenv-users` named `{userID}-{attemptID}-netpol`.
- **podSelector**: `user-id: {userID}`, `attempt-id: {attemptID}`
- **Ingress**: from pods with same labels in same namespace; port 22/TCP from `rootenv-infra` namespace
- **Egress**: to pods with same labels in same namespace; port 53 UDP+TCP to `kube-system` (DNS)
- Both pod-to-pod peers use `NamespaceSelector` + `PodSelector` together — `PodSelector` alone would match pods in ALL namespaces
- Created idempotently (AlreadyExists ignored); deleted only when last provisioned/provisioning asset for the attempt is gone

## Decommission Flow
`commands` record with `command=decommission`, `status=pending` → contmgr sets `status=running` → deletes pod → deletes service → checks remaining provisioned assets for the attempt → deletes NetworkPolicy if none remain → sets `status=done` and `assets.state=decommissioned`.

## `assets.configuration` JSON Schema
```json
{"platform": "container", "pod": "<podName>", "svc": "<svcName>", "user_id": "<userID>"}
```

## Connection Info Stored in PocketBase
```json
{"host": "<svc>.rootenv-users.svc.cluster.local", "port": 22, "user": "<ssh_user>"}
```
The relay dials this host:port directly — no port forwarding or host IP needed.

## ContMgr Never Dials SSH
ContMgr communicates with pods exclusively through the Kubernetes API (pod phase polling + exec).
It does not dial port 22 on the service or the pod IP. Only the relay does.
