# Design: relay-exec Integration

**Date:** 2026-06-19
**Status:** Approved

## Goal

Wire up `relay-exec` end-to-end: one relay-exec pod per lab namespace, reachable from the browser via `/relay/exec/<attemptId>/<assetName>/`. Authentication is skipped for now via `RELAY_SKIP_AUTH`. SSH is removed from the frontend entirely.

## Affected Areas

1. **Relay** — add `SkipAuth` to `relaybase.Handler`; update `cmd/relay-exec/main.go`
2. **Operator** — switch deployment from `relay-primitive` to `relay-exec`; update ingress path, network policy, env vars
3. **Frontend** — remove SSH; add exec composable and panel; fix `secrets` bug; wire `attemptId` through
4. **Skaffold + dev overlay** — switch artifact and `RELAY_EXEC_IMAGE`

---

## 1. Relay

### `pkg/relaybase/handler.go`

Add `SkipAuth bool` field to `Handler`. When true:
- Still reads and discards the first WebSocket message (preserves client contract)
- Skips `X-Attempt-Id` and `X-User-Id` header checks
- Uses `"anonymous"` as `userID` for logging and connection limiter

### `cmd/relay-exec/main.go`

- Read `RELAY_SKIP_AUTH` env var (`"true"` → set `Handler.SkipAuth = true`)
- When `RELAY_SKIP_AUTH=true`, `RELAY_MY_ATTEMPT_ID` and `RELAY_MY_OWNER_ID` are optional (warn but don't exit if missing)
- Set `handler.SkipAuth` from the env var

---

## 2. Operator (`services/labenv-operator/internal/controller/relay.go`)

### `loadRelayConfig`

`RELAY_INGRESS_BASE_PATH` default changes from `/relay` → `/relay/exec`.

### `ensureRelayDeployment`

- Deployment name: `relay-primitive` → `relay-exec`
- Labels: `app: relay-primitive` → `app: relay-exec`
- Container name: `relay-primitive` → `relay-exec`
- Env vars added:
  - `RELAY_MY_ATTEMPT_ID` = `env.Name` (CR name is the attempt ID)
  - `RELAY_MY_OWNER_ID` = `env.Spec.OwnerId`
  - `RELAY_SKIP_AUTH` = `"true"`
- Readiness probe path: `/` → `/healthz`

### `ensureRelayNetworkPolicy`

`podSelector` `app: relay-primitive` → `app: relay-exec`.

### `ensureRelayIngress`

No structural change — path is already `cfg.ingressBasePath + "/" + env.Name`. With the updated default (`/relay/exec`), the path becomes `/relay/exec/<attemptId>`. Traefik strips this prefix; relay-exec sees `/<assetName>/`.

### Dev overlay (`deploy/overlays/dev/kustomization.yaml`)

- `RELAY_EXEC_IMAGE` value: `relay-primitive:<digest>` → `relay-exec:latest` (Skaffold will resolve the digest)
- `RELAY_INGRESS_BASE_PATH`: not set (default `/relay/exec` is correct)

---

## 3. Frontend

### Removed entirely

- `services/frontend/src/composables/useSshRelayConnection.js`
- `services/frontend/src/composables/__tests__/useRelayConnection.spec.js`
- `services/frontend/src/components/lab/TerminalPanel.vue`

### `useLabSession.js` — fix `secrets` bug + expose `attemptId`

Return `secrets: {}` and `attemptId` from the composable:
- `secrets: {}` — empty object so `LabView` destructure doesn't get `undefined`; SSH tabs are gone so this is never populated
- `attemptId` — `computed(() => attemptsStore.lastAttempt?.id ?? null)` — passed down to `LabConsole` so exec connections can build their URL

### New: `useExecRelayConnection.js`

Parameters: `(attemptId, assetName)`

- URL: `/relay/exec/${attemptId}/${assetName}/`
- First WS message: `pb.authStore.token` (relay discards it under skip-auth; kept so the contract is forward-compatible when auth is added)
- Binary frames: same resize protocol as SSH (`\x01` + cols/rows uint16 LE)
- `onclose`: writes disconnect message to terminal; no token-refresh logic
- No healthz check (each lab has its own relay instance; healthz is per-lab, not global)
- Terminal config: identical to SSH version (xterm, dark theme, fit addon, web links)

### New: `ExecTerminalPanel.vue`

Props: `assetName: String`, `attemptId: String`

Uses `useExecRelayConnection(attemptId, assetName)`. Same template as the old `TerminalPanel` (terminal div + Alt+W hint + keyboard intercept logic).

### `LabConsole.vue`

- Remove `ssh: TerminalPanel` from `tabComponents`; add `exec: ExecTerminalPanel`
- Remove `secrets` prop
- Remove `secrets[tab.serverId]` gate — render `ExecTerminalPanel` unconditionally when `tab.type === 'exec'`
- Add `attemptId` prop; pass to `ExecTerminalPanel`

### `LabView.vue`

- Remove `secrets` from `useLabSession` destructure
- Pass `attemptId` prop to `LabConsole`

### `useTerminalTabs.spec.js`

Replace `'ssh'` protocol strings with `'exec'` throughout — the tab logic is protocol-agnostic so only the string values change.

---

## 4. Skaffold (`skaffold.yaml`)

Replace:
```yaml
- image: relay-primitive
  context: services/relay
  docker:
    dockerfile: cmd/relay-primitive/Dockerfile
```
With:
```yaml
- image: relay-exec
  context: services/relay
  docker:
    dockerfile: cmd/relay-exec/Dockerfile
```

---

## Data Flow (after this change)

```
Browser
  → WebSocket /relay/exec/<attemptId>/<assetName>/
  → Traefik (strips /relay/exec/<attemptId>)
  → relay-exec pod in lab namespace (/<assetName>/)
  → kubectl exec into pod named <assetName> in same namespace
```

## What Is Not Changed

- `relay-authenticator` — not wired yet; `RELAY_SKIP_AUTH=true` bypasses header checks
- `relay-ssh` — left in the repo, just removed from frontend
- `LabEnvironment` CRD — no schema changes
- `useTerminalTabs.js` — logic unchanged, only test strings updated
- Connection limiter, resize framing, graceful shutdown — all inherited unchanged
