---
description: Non-obvious architectural details and invariants for the contmgr module
paths:
  - "contmgr/*"
---

# Contmgr Architecture

## Encryption Contract (shared with relay)
AES-256-GCM; key = `sha256.Sum256([]byte(keys.secret))`; ciphertext stored as `base64(12-byte-nonce || ciphertext)`.
The relay uses the identical scheme to decrypt — these two must stay byte-for-byte compatible.

## PocketBase Auth
Contmgr authenticates as a regular user (`/api/collections/users/auth-with-password`), not as admin.
Service account: `svc_contmgr@contmgr.local`, `svc_role="contmgr"`.
Access rules on `assets`, `keys`, `attempts`, `commands` are gated on `svc_role = "contmgr"`.

## Data Flow per Asset
1. Asset enters `state=provisioning` (set by hook on attempt creation)
2. Contmgr reads `attempt.environment.servers` to find the matching asset def (by `name`)
3. Generates Ed25519 keypair → starts container → injects public key via docker exec
4. Waits for SSH TCP readiness (poll `:22` via hostIP:hostPort, up to 30 attempts × 1s)
5. Fetches `keys` record for the attempt → encrypts private key → patches `keys.key_encrypted`
6. Patches `assets.connection` + `assets.configuration` → patches `assets.state=provisioned`

## Per-Attempt Docker Network
Each attempt gets a dedicated Docker bridge network named `lab-<attemptID>`.
- Created before the first container is started during provisioning.
- Containers join the network at create time (via `NetworkingConfig`); `ContainerName` (= `asset.Name`) is set as hostname and DNS alias so containers resolve each other by name.
- Removed after the container is removed during decommission, even if container ID is empty.
- `networkName(attemptID)` in `docker.go` is the single source of truth for the name format.

## Decommission Flow
`commands` record with `command=decommission`, `status=pending` → contmgr sets `status=running` → removes container → removes network → sets `status=done` and `assets.state=decommissioned`.

## `assets.configuration` JSON Schema
```json
{"platform": "container", "id": "<dockerContainerID>"}
```
Extensible for future platforms (vm, etc.).
