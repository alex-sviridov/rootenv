# Lab images built and resolved via Skaffold

## Problem

`Asset.Image` in lab YAMLs (e.g. `ubuntu-sshd`) is a free-text string. The
operator resolves the final image ref at pod-creation time as
`LABENV_REGISTRY + "/" + asset.Image` (`ensurePod`,
`services/labenv-operator/internal/controller/labenvironment_controller.go:468`).

Nothing in the Skaffold/CI pipeline builds or pushes these images. The only
existing build path is `make labs-build`, which runs a raw `docker build`
against `labs/images/ubuntu-sshd` and `k3d image import`s the result directly
into the local k3d cluster's containerd. This only works for local dev — there
is no sandbox/prod equivalent, and `LABENV_REGISTRY` is unset everywhere, so
in practice lab images can't be deployed outside local k3d today.

Meanwhile, service images (`backend`, `frontend`, `labenv-operator`,
`attempt-controller`, `relay-exec`, `relay-authenticator`) are already built
by Skaffold as `artifacts:` and consumed via two patterns:

- Pods that Skaffold deploys directly: Skaffold rewrites the image ref in the
  manifest at apply time.
- Pods created dynamically by `labenv-operator` (which Skaffold can't see or
  rewrite): the resolved ref is round-tripped through a ConfigMap. Skaffold's
  `resourceSelector` rewrites `relay-images`'s `data.image` field at
  deploy time (matching by artifact name), and the operator deployment reads
  it into the `RELAY_EXEC_IMAGE` env var via `configMapKeyRef`.

Lab images need the second pattern, generalized: there isn't one well-known
image (`relay-exec`) but an open-ended set, one per `labs/images/*`
directory, looked up by name at runtime.

## Design

### 1. One Skaffold artifact per `labs/images/*` directory

Directory name = artifact name = the value lab YAMLs put in `Asset.Image`.
Each lab image directory gets a manually-added entry in `skaffold.yaml`,
same shape as existing service artifacts:

```yaml
- image: ubuntu-sshd
  context: labs/images/ubuntu-sshd
  docker:
    dockerfile: Dockerfile
```

Adding a new lab image means adding a new artifact entry — no codegen, same
manual process as adding a new service image today.

### 2. One ConfigMap, one key per lab image

A new `lab-images` ConfigMap in the `labenv-operator-system` namespace, one
key per lab image directory, seeded with `<name>:latest` so Skaffold's
image-name matching finds it:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lab-images
  namespace: labenv-operator-system
data:
  ubuntu-sshd: ubuntu-sshd:latest
```

`skaffold.yaml`'s `resourceSelector.allow` gets one JSON-pointer per key
(`.data.ubuntu-sshd`, etc.) alongside the existing `.data.image` pointer used
for `relay-images`. Skaffold requires static pointers — this list grows
in lockstep with the artifact list whenever a new lab image is added.

### 3. Operator reads the ConfigMap as a mounted volume

Unlike `RELAY_EXEC_IMAGE` (one well-known env var for one well-known image), the
operator needs to look up an arbitrary key (`asset.Image`) at runtime, so an
env var per image doesn't scale. Mount the `lab-images` ConfigMap as a volume
on the controller-manager deployment (e.g. at `/etc/lab-images/`), where each
key becomes a file. `ensurePod` reads `/etc/lab-images/<asset.Image>` to get
the resolved ref.

This replaces the `LABENV_REGISTRY` env var and its string-concat logic
entirely. If the file for a given `asset.Image` doesn't exist, pod creation
fails with a clear error (no silent fallback to the literal asset name).

### 4. Local dev (k3d)

Skaffold's default profile already builds with `local.push: false` and loads
images directly into the cluster's containerd — the same mechanism
`backend`/`frontend`/etc rely on today. Lab images get this for free once
they're declared as artifacts; no separate local-build path is needed.

`make labs-build` (raw `docker build` + `k3d image import`) becomes
redundant and is removed, along with its invocation from the `dev-cluster`
target.

## Out of scope

- Auto-discovery of `labs/images/*` directories into `skaffold.yaml` or the
  ConfigMap (deferred — manual entries are fine at current scale, one image
  today).
- Changes to the lab YAML schema (`Asset.Image` keeps its current meaning:
  an image name, now resolved via the ConfigMap instead of a registry
  prefix).
