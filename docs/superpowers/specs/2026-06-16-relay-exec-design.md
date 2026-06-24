# relay-exec design

**Date:** 2026-06-16
**Branch:** feat/crd-labenv-operator
**Scope:** `services/relay-authenticator`, `services/relay`, `services/labenv-operator`, `deploy/`

## Problem

relay-ssh is cluster-wide and uses SSH with encrypted keys stored in PocketBase. The goal is to replace it with `kubectl exec` as the transport, with one relay instance per LabEnvironment namespace, and a central ingress authenticator that validates PocketBase tokens before routing to the correct relay.

## Goals

- Replace SSH with `kubectl exec` — eliminates key lifecycle entirely
- One `relay-exec` pod per LabEnvironment namespace — blast radius limited to one namespace
- labenv namespaces remain fully isolated — no egress to PocketBase from within the namespace
- Auth logic centralised in one stateless service (`relay-authenticator`) reusable by future relay types
- No admin token at the auth boundary — PocketBase's own rules enforce ownership

## Architecture

```
Browser
  │  wss://<host>/relay/<attemptId>/exec/<assetName>/
  │  First WS message: <pb_token>
  ▼
Traefik
  │  IngressRoute matches /relay/<attemptId>/exec/<assetName>/
  │  Middleware: set X-Attempt-Id: <attemptId> (static, from route)
  │  Middleware: ForwardAuth → relay-authenticator
  │    sends: Authorization + X-Attempt-Id
  │    receives: 200 + X-User-Id + X-Attempt-Id  (or 401/403 → reject)
  │  Middleware: stripPrefix /relay/<attemptId>/exec
  │  routes to relay-exec Service in namespace labenv-<attemptId>
  ▼
relay-exec pod  (one per LabEnvironment namespace)
  │  reads first WS message → pb_token (discarded — auth already done)
  │  validates: X-Attempt-Id == MY_ATTEMPT_ID (env var)
  │             X-User-Id is non-empty
  │  acquires connection slot
  │  pod name == assetName, namespace == MY_NAMESPACE
  │  kubectl exec into pod → shell
  │  proxies WebSocket ↔ exec stream
  ▼
asset pod (e.g. workstation, target) in same namespace
```

## New components

| Component | Location | What it does |
|---|---|---|
| `relay-authenticator` | `services/relay-authenticator/` | Stateless HTTP service; validates PB token + attempt ownership; sets X-User-Id / X-Attempt-Id |
| `relay-exec` | `services/relay/cmd/relay-exec/` + `services/relay/exec/` | WebSocket → kubectl exec proxy; uses shared relaybase.Handler |
| `relaybase.Handler` | `services/relay/pkg/relaybase/` | Generic WS handler extracted from ssh; shared by all relay types |
| operator changes | `services/labenv-operator/` | Provisions relay-exec Deployment + Service + RBAC + IngressRoute + Middlewares per namespace |
| Traefik resources | created by operator in `rootenv-infra` | IngressRoute + ForwardAuth Middleware + headers + stripPrefix per attempt |

## Section 1: `relay-authenticator`

### Repository layout

```
services/relay-authenticator/
  cmd/main.go
  internal/
    auth/
      handler.go
      handler_test.go
    pbclient/
      client.go        — ValidateToken + GetAttempt (user token only, no admin token)
      client_test.go
  Dockerfile
  go.mod
```

Own Go module — no shared code with relay.

### Handler logic (`GET /auth`)

Inputs (forwarded by Traefik ForwardAuth):
- `Authorization: <pb_token>` — from the browser's WebSocket upgrade request
- `X-Attempt-Id: <attemptId>` — injected by Traefik headers middleware before ForwardAuth

Steps:
1. `POST /api/collections/users/auth-refresh` with `Authorization: <pb_token>` → `userId`
2. `GET /api/collections/attempts/records/<attemptId>` with `Authorization: <pb_token>` — PocketBase viewRule enforces ownership; 403 means wrong user or attempt not found
3. Return `200` + `X-User-Id: <userId>` + `X-Attempt-Id: <attemptId>`

No admin token. Two PocketBase calls per WebSocket upgrade (at handshake time only).

### Error responses

| Condition | HTTP |
|---|---|
| Missing `Authorization` | 401 |
| Invalid/expired PB token | 401 |
| Missing `X-Attempt-Id` | 400 |
| Attempt not found or 403 from PB | 403 |
| PocketBase unreachable | 503 |

### Config (env vars)

```
INGAUTH_POCKETBASE_URL         — PocketBase base URL
INGAUTH_POCKETBASE_TLS_VERIFY  — default true; set false in dev for self-signed certs
INGAUTH_PORT                   — default 8080
```

## Section 2: `relay-exec` and `relaybase.Handler`

### Repository layout

```
services/relay/
  pkg/
    relaybase/
      handler.go       — new: generic Handler; accepts WS, reads first message, validates headers, calls Backend
      handler_test.go
      auth.go          — existing (ssh-specific ValidateToken logic stays in ssh/)
      limiter.go       — existing
      server.go        — existing
  exec/
    backend.go         — implements relaybase.Backend: resolves assetName→pod, kubectl exec↔WS proxy
    metrics.go
    backend_test.go
    Dockerfile
  ssh/
    handler.go         — existing; token refresh (\x00 frame) stays here, not in relaybase
    ...
  cmd/
    relay-exec/
      main.go          — wires relaybase.Handler{Backend: &exec.Backend{...}}
    relay-ssh/
      main.go          — existing
```

### `relaybase.Backend` interface

```go
type Backend interface {
    Serve(ctx context.Context, conn *websocket.Conn, assetName string, userID string) error
}
```

### `relaybase.Handler` flow

1. Extract `assetName` from URL path (`/{assetName}/`)
2. Accept WebSocket upgrade
3. Read first message → `pb_token` within 10s timeout (received for protocol compatibility; discarded — auth already done by ingress). Close `StatusPolicyViolation` on timeout.
4. Validate `X-Attempt-Id == MY_ATTEMPT_ID` (env var) and `X-User-Id` non-empty → else close `StatusPolicyViolation`
5. Acquire connection slot via `Limiter`
6. Call `Backend.Serve(ctx, conn, assetName, userID)`

### `exec.Backend.Serve()`

1. Pod name == `assetName`, namespace == `MY_NAMESPACE`
2. Open kubectl exec via in-cluster ServiceAccount → shell (`/bin/sh` or `/bin/bash`)
3. Proxy WebSocket ↔ exec stream
   - `\x01` + 4 bytes → terminal resize (cols/rows uint16 LE)
   - anything else → stdin
4. Clean close on context cancellation or exec exit

No `\x00` token-refresh frame — that is ssh-specific and stays in `ssh/handler.go`.

### Config (env vars) — universal for all relay types

```
RELAY_MY_ATTEMPT_ID    — set by operator at Deployment creation
RELAY_MY_OWNER_ID      — set by operator at Deployment creation
RELAY_MY_NAMESPACE     — set by operator (= labenv-<attemptId>)
RELAY_PORT             — default 8080
RELAY_ALLOWED_ORIGINS  — comma-separated, optional
```

No PocketBase connection from relay-exec. No periodic token revalidation. Namespace has zero PocketBase egress.

### relay-exec RBAC

ServiceAccount in labenv namespace with a namespace-scoped Role:
```
pods:      get, list
pods/exec: create
```

## Section 3: Operator changes

### New ensure* calls (added to reconcile sequence)

```
ensureRelayServiceAccount(ctx, nsName)
ensureRelayRole(ctx, nsName)
ensureRelayRoleBinding(ctx, nsName)
ensureRelayDeployment(ctx, env, nsName)
ensureRelayService(ctx, nsName)
ensureIngressRoute(ctx, env, nsName)     — creates resources in rootenv-infra
```

### Deployment

```
image:              LABENV_RELAY_EXEC_IMAGE (operator env var)
serviceAccountName: relay-exec-sa
env:
  RELAY_MY_ATTEMPT_ID = env.Name
  RELAY_MY_OWNER_ID   = env.Spec.OwnerId
  RELAY_MY_NAMESPACE  = nsName
  RELAY_PORT          = 8080
```

### Service

ClusterIP, port 8080, selector `app: relay-exec`.

### NetworkPolicy changes

Switch `ensureNetworkPolicy` from `IgnoreAlreadyExists` to `CreateOrPatch` so existing namespaces get the new rules on operator upgrade.

Two new rules:

**Ingress — Traefik → relay-exec only (port 8080):**
```yaml
ingress:
  - from:
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: kube-system  # Traefik namespace
    ports: [8080/TCP]
    to:
      podSelector:
        matchLabels:
          app: relay-exec
```

**Egress — relay-exec → kube-apiserver (port 6443) for pods/exec:**
```yaml
egress:
  - ports: [6443/TCP]
    to: []   # kube-apiserver has no namespace; empty to = all destinations on this port
```
Note: this egress rule applies to all pods in the namespace. A more targeted approach is to label relay-exec pods and use a separate NetworkPolicy object scoped to that podSelector, rather than adding it to the shared namespace policy.

Asset pods retain existing rules (DNS + same-namespace only).

### Traefik resources (created in `rootenv-infra` per LabEnvironment)

**Middleware — inject X-Attempt-Id:**
```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: relay-exec-headers-<attemptId>
  namespace: rootenv-infra
spec:
  headers:
    customRequestHeaders:
      X-Attempt-Id: "<attemptId>"
```

**Middleware — ForwardAuth:**
```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: relay-exec-auth-<attemptId>
  namespace: rootenv-infra
spec:
  forwardAuth:
    address: http://relay-authenticator-svc.rootenv-infra.svc/auth
    authRequestHeaders: ["Authorization", "X-Attempt-Id"]
    authResponseHeaders: ["X-User-Id", "X-Attempt-Id"]
```

**Middleware — stripPrefix:**
```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: relay-exec-strip-<attemptId>
  namespace: rootenv-infra
spec:
  stripPrefix:
    prefixes: ["/relay/<attemptId>/exec"]
```

**IngressRoute:**
```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: relay-exec-<attemptId>
  namespace: rootenv-infra
  labels:
    rootenv.io/attempt-id: <attemptId>
spec:
  entryPoints: [websecure]
  routes:
    - match: PathPrefix(`/relay/<attemptId>/exec/`)
      kind: Rule
      middlewares:
        - name: relay-exec-headers-<attemptId>
        - name: relay-exec-auth-<attemptId>
        - name: relay-exec-strip-<attemptId>
      services:
        - name: relay-exec-svc
          namespace: labenv-<attemptId>
          port: 8080
```

**Cleanup:** operator deletes all four resources from `rootenv-infra` when LabEnvironment is deleted.

**New operator ClusterRole rules:**
```
ingressroutes.traefik.io:   create, delete, patch, get
middlewares.traefik.io:     create, delete, patch, get
```

scoped to `rootenv-infra` namespace via RoleBinding (not ClusterRoleBinding).

### New operator env vars

```
LABENV_RELAY_EXEC_IMAGE   — relay-exec container image (tag injected at build time)
```

## Section 4: Security invariants

1. **NetworkPolicy is the primary trust boundary** — relay-exec only accepts connections from the Traefik namespace; header forgery requires compromising Traefik or the operator.
2. **relay-authenticator never uses admin token** — all PocketBase calls use the user's own token; PocketBase enforces ownership via its own viewRule.
3. **relay-exec validates headers against its own identity** — `X-Attempt-Id` must match `MY_ATTEMPT_ID`; a misconfigured route pointing to the wrong relay is rejected at the relay.
4. **labenv namespace has zero PocketBase egress** — asset pods and relay-exec cannot reach PocketBase.
5. **One active attempt per user** enforced by PocketBase before-create hook (existing).

## What is NOT in scope

- relay-ssh removal (parallel operation during transition)
- relay-http and relay-filemanager (future; will reuse `relaybase.Handler` and `relay-authenticator`)
- Frontend changes to connect to the new URL pattern
- relay-authenticator HA / replicas (operator concern, not design concern)
