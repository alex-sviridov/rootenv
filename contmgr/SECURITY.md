# Security Model

LinuxLab gives users a root shell inside a container. This document explains how the system
is hardened so that a user being root inside their container cannot affect other users or the
host node.

---

## Namespace isolation

Every lab attempt gets its own Kubernetes namespace: `rootenv-lab-{attemptID}`.

- Pods from different users are in different namespaces and cannot see each other through the
  Kubernetes API or by pod name.
- Pod names are short (`{assetName}`) because the namespace already provides the isolation
  boundary — no cross-namespace collision is possible.
- When a lab ends, the entire namespace is deleted, taking all pods, services, and network
  policies with it. No residual state is left on the cluster.

---

## Network policy

Each namespace gets a `NetworkPolicy` named `allow-relay` that is applied to all pods
(`podSelector: {}`). The default posture is deny-all; the policy then carves out only what
is needed:

| Direction | Allowed | Reason |
|-----------|---------|--------|
| Ingress | Same namespace | Multi-server labs (pods talk to each other) |
| Ingress | Port 22/TCP from `rootenv-infra` | Relay SSHes in; nothing else may |
| Egress | Same namespace | Inter-pod communication within the lab |
| Egress | Port 53 UDP+TCP to `kube-system/kube-dns` | DNS resolution |

Everything else is blocked:

- **No outbound internet.** Pods cannot exfiltrate data or download tooling mid-lab.
- **No access to `rootenv-infra`.** A compromised pod cannot reach PocketBase, the relay,
  or the contmgr API.
- **No cross-namespace pod traffic.** Two users' namespaces are completely dark to each other.

---

## Pod security context

Users are root inside their container by design — that is the point of the product. The
security context is therefore not about preventing root access inside the container; it is
about limiting what root inside the container can do to the host.

### Kernel user namespaces (`hostUsers: false`)

`spec.hostUsers: false` activates Linux user namespace remapping (K8s 1.30+). UID 0 inside
the container maps to a high, unprivileged UID on the host. A container escape drops the
attacker onto the node as an ordinary non-root user rather than as root. This is the highest
single-control payoff in the spec.

### Seccomp (`RuntimeDefault`)

`seccompProfile: RuntimeDefault` installs the container runtime's default seccomp filter.
This blocks roughly 300 rarely-used syscalls — `ptrace`, `keyctl`, `kexec_load`, etc. — that
have historically been the source of container escapes, while allowing everything a normal
Linux session needs.

### Capabilities

Only `NET_RAW` is dropped. The full Linux capability set is otherwise preserved because lab
content legitimately uses:

- `CAP_SYS_ADMIN` — mount, namespaces, cgroups (RHCSA labs)
- `CAP_NET_ADMIN` — `ip`, `tc`, routing, iptables (networking labs)
- `CAP_SYS_PTRACE` — `strace`, debuggers

`NET_RAW` is dropped because raw sockets (the capability it guards) have no legitimate use in
any current lab and are the primary primitive for ICMP tunnels, ARP spoofing, and VLAN
hopping.

`readOnlyRootFilesystem` and `runAsNonRoot` are intentionally not set — SSH sessions and
container init require a writable filesystem, and users are meant to be root.

### PID limit

Kubernetes enforces PID limits at the kubelet level only — there is no per-pod or
per-namespace API. 
**k3s** — add to `/etc/rancher/k3s/config.yaml` on each lab node and restart:
```yaml
kubelet-arg:
  - "pod-max-pids=500"
```

**kubeadm** — edit the kubelet ConfigMap and restart kubelet on each node:
```
kubectl -n kube-system edit configmap kubelet-config
# add podPidsLimit: 500 under kubeletconfig
systemctl restart kubelet
```

Verify:
```
kubectl get --raw /api/v1/nodes/<node>/proxy/configz | jq '.kubeletconfig.podPidsLimit'
```

This caps the total PIDs across all processes in a pod to 500, preventing fork bombs from
exhausting the node's PID namespace. Apply to lab nodes only — infra nodes can use a higher
or unset limit.

### Resource limits and requests

Every pod has explicit CPU, memory, and optional disk (`ephemeral-storage`) limits sourced
from the lab's asset definition. Without limits, a user could starve other pods on the same
node.

`requests` are set at CPU/4 and memory/2 of the corresponding limits, giving the scheduler
a realistic estimate for bin-packing without over-reserving.

---

## Optional: gVisor (`runtimeClassName`)

Setting `CONTMGR_RUNTIME_CLASS=gvisor` on the contmgr deployment makes every user pod run
under [gVisor](https://gvisor.dev) (`runsc`). gVisor interposes all syscalls in a userspace
kernel — a container escape reaches the gVisor sandbox, not the host kernel. This requires
gVisor to be installed on each node as a `RuntimeClass`.

Default is empty (cluster default OCI runtime). The feature is transparent to users.

---

## SSH key handling

Each pod gets a fresh Ed25519 keypair at provision time:

1. The keypair is generated in memory — never written to disk.
2. The public key is injected into the pod via `kubectl exec` after the pod reaches `Running`.
3. The private key is encrypted with AES-256-GCM (key = SHA-256 of a PocketBase-generated
   secret) and stored in PocketBase. Only the relay can decrypt it at connection time.
4. The `exec` script uses `printf '%s'` with `%q`-quoted key material to prevent shell
   injection regardless of key content.

The relay is the only component that ever dials SSH. Contmgr never opens a connection to
port 22.

---

## RBAC

### Contmgr service account

Contmgr runs as ServiceAccount `contmgr` in `rootenv-infra` with a ClusterRole that grants
only the verbs it actually uses: `create`/`delete`/`get`/`list`/`watch` on namespaces, pods,
services, network policies, roles, and role bindings. No `update`, no `patch`, no access to
secrets or config maps.

### Per-namespace role

At namespace creation time, contmgr creates a `Role` + `RoleBinding` inside the new
namespace scoping its own access to that namespace's resources. This limits the blast radius
if the ClusterRole were ever over-granted.

### Contmgr pod

The contmgr container itself runs with a hardened security context: `runAsNonRoot`,
`readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, `seccompProfile: RuntimeDefault`,
`capabilities.drop: [ALL]`.

---

## What is not in scope

- **Host-level isolation between nodes** — if multi-tenancy across nodes is required, node
  taints/tolerations and node selectors should be added so that user pods land on dedicated
  nodes.
- **Image provenance** — images are trusted at the point of definition. Signing and admission
  verification (e.g. Sigstore/Cosign) are out of scope for this component.
- **Audit logging** — Kubernetes audit logs capture all API activity; forwarding and alerting
  on them is an infrastructure concern outside contmgr.
