# Design: labenv-operator `ensureRelay`

**Date:** 2026-06-19  
**Status:** Approved

## Goal

Add `ensureRelay` to the labenv-operator controller. When a `LabEnvironment` is provisioned, the operator creates a `relay-primitive` deployment inside the lab namespace, with all supporting Kubernetes resources, so the relay is ready to accept WebSocket connections for that lab.

## New File

`services/labenv-operator/internal/controller/relay.go` — package `controller`.

Keeps relay logic isolated from the main controller file for readability.

## Resources Created (all in `nsName`)

| Function | Kind | Name |
|---|---|---|
| `ensureRelayServiceAccount` | `ServiceAccount` | `relay` |
| `ensureRelayRole` | `Role` | `relay` |
| `ensureRelayRoleBinding` | `RoleBinding` | `relay` |
| `ensureRelayDeployment` | `Deployment` | `relay-primitive` |
| `ensureRelayService` | `Service` | `relay` |
| `ensureRelayIngress` | `Ingress` | `relay` |
| `ensureRelayNetworkPolicy` | `NetworkPolicy` | `allow-traefik-to-relay` |

All follow the existing create-once pattern: `Get` → if `NotFound`, `Create` → `IgnoreAlreadyExists`. No updates on reconcile.

## Operator Env Vars (read in `ensureRelay`, passed to sub-functions)

| Var | Required | Default | Purpose |
|---|---|---|---|
| `RELAY_IMAGE` | yes | — | Image for the relay-primitive container. Reconcile returns error if missing. |
| `RELAY_INGRESS_CLASS` | no | `""` | `ingressClassName` on the Ingress. Omitted from spec if empty. |
| `RELAY_INGRESS_ANNOTATIONS` | no | `""` | Comma-separated `key=value` pairs added as Ingress annotations. |
| `RELAY_INGRESS_BASE_PATH` | no | `/relay` | Path prefix. Full path = `<base>/<envName>`. |

`RELAY_INGRESS_ANNOTATIONS` parsing: split on `,`, then split each token on `=` (first `=` only), skip malformed tokens.

## Resource Specs

### ServiceAccount
Minimal — no `automountServiceAccountToken` override (Deployment sets it explicitly).

### Role
```
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods/exec"]
  verbs: ["create"]
```

### RoleBinding
Binds `relay` ServiceAccount (same namespace) to `relay` Role.

### Deployment
- `replicas: 1`
- `selector/labels`: `app: relay-primitive`
- Pod security context: `runAsNonRoot: true`, `runAsUser: 10001`, `seccompProfile: RuntimeDefault`
- `automountServiceAccountToken: false` — NOT set on pod (SA token is needed for `kubectl exec`). ServiceAccount is set to `relay`.
- Container security context: `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities: drop: ["ALL"]`
- Env: `RELAY_NAMESPACE=<nsName>`
- Resources: requests+limits `cpu: 100m/200m`, `memory: 64Mi/128Mi`
- ReadinessProbe: `httpGet /` on port 8080
- `imagePullPolicy: Always`

### Service
- ClusterIP (not headless)
- Port 8080 → targetPort 8080
- Selector: `app: relay-primitive`

### Ingress
- Path: `<RELAY_INGRESS_BASE_PATH>/<envName>`, `PathType: Prefix`
- Backend: Service `relay`, port 8080
- `ingressClassName`: set if `RELAY_INGRESS_CLASS` is non-empty
- Annotations: parsed from `RELAY_INGRESS_ANNOTATIONS`

### NetworkPolicy
- `podSelector`: `app: relay-primitive`
- PolicyType: Ingress only
- Allow ingress from namespace `kubernetes.io/metadata.name: kube-system`, port 8080/TCP

## Call Site

In `reconcileCreate`, after `ensureLimitRange`, before the assets loop:

```go
if err := r.ensureRelay(ctx, env, nsName); err != nil {
    return ctrl.Result{}, err
}
```

## RBAC Markers (added to relay.go)

```go
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;bind;escalate
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create
```

`bind` and `escalate` are required by Kubernetes when an operator creates a Role/RoleBinding that grants permissions — without them the API server rejects the create with a forbidden error.

These markers are used by `make manifests` to regenerate `config/rbac/role.yaml`.

## What Is Not Changed

- `labenvironment_controller.go` — only the `ensureRelay` call site added
- `labenvironment_types.go` — no CRD schema changes
- No update/patch logic for relay resources — operator restart picks up image changes
