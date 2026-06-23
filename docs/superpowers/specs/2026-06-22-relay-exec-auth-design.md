# Design: relay-exec Ingress Authentication

**Date:** 2026-06-22
**Status:** Approved

## Goal

Add authentication to the Traefik ingress route created per relay-exec instance. A shared `relay-authenticator` in `rootenv-infra` validates the user's PocketBase session cookie and verifies attempt ownership before Traefik proxies the WebSocket upgrade. The relay-exec namespace stays fully isolated — relay-exec never calls PocketBase.

## Constraints

- No PocketBase access from inside the lab namespace (principal isolation)
- No new PocketBase endpoints or schema changes
- No token in URL query string (avoids log exposure)
- The browser WebSocket API cannot send custom headers on upgrade — token delivered via cookie

## Auth Flow

```
Browser
  1. sets cookie pb_auth=<token> (SameSite=Strict; Secure; path=/)
  2. opens WebSocket wss://.../relay/exec/<attemptId>/server-0/
     → browser sends cookie automatically on the HTTP upgrade

Traefik (kube-system)
  3. matches ForwardAuth middleware on the per-attempt ingress route
  4. calls relay-authenticator /auth  (rootenv-infra, over cluster DNS)
     forwarded headers: Cookie, X-Forwarded-Uri: /relay/exec/<attemptId>/server-0/

relay-authenticator
  5. reads pb_auth cookie → token
  6. parses X-Forwarded-Uri → extracts <attemptId> (2nd segment after /relay/exec/)
  7. calls PocketBase auth-refresh(token) → userID
  8. calls PocketBase GET /attempts/<attemptId> with token → verifies ownership
  9. returns 200 + X-User-Id: <userID>  (or 401/403 on failure)

Traefik
  10. proxies WebSocket upgrade to relay-exec, forwarding X-User-Id header

relay-exec (separate task)
  11. reads X-User-Id from trusted upstream header — no PocketBase call needed
```

## Components

### 1. `services/relay-authenticator/internal/auth/handler.go`

Replace header-based input with cookie + URI parsing:

- Read token from `Cookie: pb_auth=<token>` (not `Authorization` header)
- Read attempt ID by parsing `X-Forwarded-Uri` header: split on `/`, find the segment after `exec` in `/relay/exec/<attemptId>/...`
- Return 400 if either is missing or URI cannot be parsed
- All PocketBase validation logic (`ValidateToken`, `GetAttempt`) unchanged
- Response headers unchanged: `X-User-Id`, `X-Attempt-Id`

Path parsing rule: given `/relay/exec/<attemptId>/server-0/`, split on `/`, drop empty segments, find index of `exec`, take the next segment. Return 400 if pattern not found.

### 2. `services/relay-authenticator/internal/auth/handler_test.go`

Update tests to use cookie + `X-Forwarded-Uri` instead of `Authorization` + `X-Attempt-Id` headers. Cover:
- success path
- missing cookie → 401
- unparseable/missing URI → 400
- invalid token → 401
- attempt ownership denied → 403

### 3. `deploy/base/61-relay-authenticator.yaml`

New manifest in `rootenv-infra` namespace:

- `Deployment`: `relay-authenticator`, 1 replica, image `relay-authenticator:latest`
  - Env: `INGAUTH_POCKETBASE_URL` = `http://backend-svc.rootenv-infra.svc.cluster.local:8090`
  - Liveness probe: `GET /healthz` — checks the process is alive
  - Readiness probe: `GET /readyz` — checks PocketBase reachability; add `/readyz` endpoint to `cmd/main.go` that calls `GET <INGAUTH_POCKETBASE_URL>/api/health` and returns 200/503
  - Security context: `runAsNonRoot`, `readOnlyRootFilesystem`, drop ALL caps
- `Service`: `relay-authenticator`, port 8080 → 8080
- `NetworkPolicy`: allow ingress on port 8080 from `kube-system` (Traefik) only; allow egress to `rootenv-infra` on port 8090 (PocketBase) only

### 4. `deploy/base/62-relay-auth-middleware.yaml`

New Traefik `Middleware` CRD in `kube-system` (where Traefik runs, so the middleware reference `kube-system-relay-auth-middleware@kubernetescrd` resolves):

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: relay-auth-middleware
  namespace: kube-system
spec:
  forwardAuth:
    address: http://relay-authenticator.rootenv-infra.svc.cluster.local:8080/auth
    authResponseHeaders:
      - X-User-Id
```

### 5. `services/relay-authenticator/cmd/main.go`

Add `/readyz` endpoint that calls `GET <INGAUTH_POCKETBASE_URL>/api/health`. Returns 200 if PocketBase responds 200, 503 otherwise.

### 6. `services/labenv-operator/internal/controller/relay.go`

`RELAY_AUTH_MIDDLEWARE` is removed — the middleware name is environment-independent and belongs in base. Instead, hardcode `traefik.ingress.kubernetes.io/router.middlewares: kube-system-relay-auth-middleware@kubernetescrd` into `relayConfig.ingressAnnotations` as a default, alongside any annotations from `RELAY_INGRESS_ANNOTATIONS` (which can still override). No new env var needed.

### 7. `services/frontend/src/composables/useExecRelayConnection.js`

Before `new WebSocket(url)` in `connect()`, set:

```js
document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
```

This refreshes the cookie on every connect so it stays current if the session was renewed.

### 8. `deploy/overlays/dev/kustomization.yaml`

No `RELAY_AUTH_MIDDLEWARE` patch needed — middleware annotation is now a default in the operator.

### 9. `deploy/base/kustomization.yaml`

Add `61-relay-authenticator.yaml` and `62-relay-auth-middleware.yaml` to the resources list.

## Network Policy

**relay-authenticator** (in `rootenv-infra`, defined in `61-relay-authenticator.yaml`):
- Ingress: allow port 8080 from `kube-system` (Traefik) only
- Egress: allow port 8090 to `rootenv-infra` (PocketBase) only

**relay-exec lab namespace** (existing `ensureRelayNetworkPolicy`): unchanged — relay-exec has no egress to `rootenv-infra` or `kube-system`.

## What Changes Later (out of scope)

- relay-exec reading and enforcing `X-User-Id` (next task per the user's stated plan)
- Removing `RELAY_SKIP_AUTH=true` from the relay-exec deployment
