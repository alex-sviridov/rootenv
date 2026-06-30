# Lab Images via Skaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Skaffold build lab images (`labs/images/*`) the same way it builds service images, and have `labenv-operator` resolve `Asset.Image` to the built ref via a mounted ConfigMap instead of a `LABENV_REGISTRY` string-concat.

**Architecture:** Each `labs/images/<name>` directory becomes a Skaffold artifact named `<name>`. A `lab-images` ConfigMap (one key per lab image, `<name>: <name>:latest`) is deployed alongside the operator and rewritten by Skaffold's `resourceSelector` at build/deploy time. The ConfigMap is mounted as a volume on the controller-manager pod; the operator reads `/etc/lab-images/<asset.Image>` to resolve the final image ref, replacing the old `LABENV_REGISTRY` env var logic.

**Tech Stack:** Go (controller-runtime/Ginkgo for the operator), Skaffold v4beta14, Kustomize, Make.

## Global Constraints

- Directory name under `labs/images/` = Skaffold artifact name = the value lab YAMLs use for `Asset.Image` (spec section 1).
- Skaffold `resourceSelector.allow[].image` pointers must be static — one explicit JSON pointer per ConfigMap key, no wildcards (spec section 2).
- `lab-images` ConfigMap lives in the `labenv-operator-system` namespace (spec section 2), same namespace as `relay-exec-image`.
- Operator looks up the resolved ref by reading a file at `/etc/lab-images/<asset.Image>`; if the file doesn't exist, pod creation fails with a clear error — no silent fallback to the literal asset name (spec section 3).
- `LABENV_REGISTRY` env var and its string-concat logic are removed entirely (spec section 3).
- `make labs-build` and its invocation from `dev-cluster` are removed; Skaffold's default profile (`local.push: false`) already loads images into the k3d cluster's containerd (spec section 4).
- Only `ubuntu-sshd` exists today as a lab image — no auto-discovery is being built; every new lab image requires a manual `skaffold.yaml` artifact entry + a manual ConfigMap key + a manual `resourceSelector` pointer (spec "Out of scope").

---

### Task 1: Add `loadLabImageRef` to the operator and wire it into `ensurePod`

**Files:**
- Create: `services/labenv-operator/internal/controller/labimages.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller.go:456-471` (the `image := asset.Image` / `LABENV_REGISTRY` block inside `ensurePod`)
- Test: `services/labenv-operator/internal/controller/labimages_test.go`

**Interfaces:**
- Produces: `func loadLabImageRef(name string) (string, error)` in package `controller` — reads `<dir>/<name>` where `<dir>` is `os.Getenv("LAB_IMAGES_DIR")`, defaulting to `/etc/lab-images` if unset. Returns the trimmed file contents, or an error if the file doesn't exist or can't be read.
- Consumes (in `ensurePod`): replaces the existing `image := asset.Image; if prefix := os.Getenv("LABENV_REGISTRY"); ...` block with a call to `loadLabImageRef(asset.Image)`; on error, `ensurePod` returns that error immediately (same error-propagation style as `ensureRelay` → `loadRelayConfig`).

This task only touches Go code (the resolution function and its call site), not the Kubernetes manifests — those come in Task 3. Tests in this task set `LAB_IMAGES_DIR` to a temp directory with files written into it, so they don't depend on any cluster state.

- [ ] **Step 1: Write the failing test for `loadLabImageRef`**

Create `services/labenv-operator/internal/controller/labimages_test.go`:

```go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("loadLabImageRef", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "lab-images-test")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Setenv("LAB_IMAGES_DIR", dir)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("returns the trimmed contents of the file matching the image name", func() {
		Expect(os.WriteFile(filepath.Join(dir, "ubuntu-sshd"), []byte("ubuntu-sshd:abc123\n"), 0644)).To(Succeed())

		ref, err := loadLabImageRef("ubuntu-sshd")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref).To(Equal("ubuntu-sshd:abc123"))
	})

	It("returns an error when no file matches the image name", func() {
		_, err := loadLabImageRef("does-not-exist")
		Expect(err).To(MatchError(ContainSubstring("does-not-exist")))
	})

	It("defaults to /etc/lab-images when LAB_IMAGES_DIR is unset", func() {
		Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
		_, err := loadLabImageRef("ubuntu-sshd")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("/etc/lab-images/ubuntu-sshd"))
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/labenv-operator && go test ./internal/controller/... -run TestControllers -v 2>&1 | grep -A5 "loadLabImageRef"`

Expected: build FAIL with `undefined: loadLabImageRef` (the function doesn't exist yet).

- [ ] **Step 3: Implement `loadLabImageRef`**

Create `services/labenv-operator/internal/controller/labimages.go`:

```go
/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultLabImagesDir = "/etc/lab-images"

// loadLabImageRef resolves the built image reference for a lab asset image
// name (e.g. "ubuntu-sshd") by reading the file Skaffold's resourceSelector
// writes into the lab-images ConfigMap, mounted as a volume on the
// controller-manager pod.
func loadLabImageRef(name string) (string, error) {
	dir := os.Getenv("LAB_IMAGES_DIR")
	if dir == "" {
		dir = defaultLabImagesDir
	}

	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("resolving lab image %q: reading %s: %w", name, path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/labenv-operator && go test ./internal/controller/... -v 2>&1 | grep -A3 "loadLabImageRef"`

Expected: PASS for all three `loadLabImageRef` specs.

- [ ] **Step 5: Replace the `LABENV_REGISTRY` block in `ensurePod`**

Read `services/labenv-operator/internal/controller/labenvironment_controller.go:456-471` first to confirm line numbers haven't drifted, then replace:

```go
	image := asset.Image
	if prefix := os.Getenv("LABENV_REGISTRY"); prefix != "" {
		image = prefix + "/" + asset.Image
	}
```

with:

```go
	image, err := loadLabImageRef(asset.Image)
	if err != nil {
		return err
	}
```

Note the function already declares `err error` via `:=` later at `r.Create(ctx, &pod)` — check the existing body. Since this is now the first `err`-producing statement in `ensurePod`, this introduces `err` into scope; the existing `if err := r.Create(ctx, &pod); err != nil { return err }` at the end of the function uses `:=` in its own `if` scope, so it's unaffected and will still compile.

- [ ] **Step 6: Remove the now-unused `os` import if no longer referenced**

Run: `cd services/labenv-operator && go build ./... 2>&1`

If it complains `"os" imported and not used` in `labenvironment_controller.go`, check whether `os` is still used elsewhere in that file (`grep -n "os\." services/labenv-operator/internal/controller/labenvironment_controller.go`). Remove the `"os"` import line only if it's now unused in that specific file — do not touch `relay.go`'s import, which still uses `os.Getenv` for `RELAY_IMAGE`.

- [ ] **Step 7: Run the full operator test suite**

Run: `cd services/labenv-operator && go test ./... 2>&1 | tail -30`

Expected: all tests pass. Tests touching `ensurePod` (if any pre-existing ones set up assets and reconcile) will now fail if they don't set `LAB_IMAGES_DIR` — check `labenvironment_controller_test.go` for any test reaching `ensurePod` via `Reconcile`/`reconcileCreate` and add the same `LAB_IMAGES_DIR` + temp-file setup pattern from Step 1 to its `BeforeEach`/`DeferCleanup` if needed to keep it green. Search first:

Run: `grep -n "reconcileCreate\|Reconcile(ctx" services/labenv-operator/internal/controller/labenvironment_controller_test.go`

If the top-level `Describe("LabEnvironment Controller" ...)` test (`labenvironment_controller_test.go:38-101`) calls `Reconcile` and creates an asset with `Image: "busybox"` (it does, at line 68), add `LAB_IMAGES_DIR` setup to its existing `BeforeEach` (line 50) so `loadLabImageRef("busybox")` succeeds:

```go
			BeforeEach(func() {
				Expect(os.Setenv("RELAY_IMAGE", "relay-primitive:test")).To(Succeed())
				DeferCleanup(func() { Expect(os.Unsetenv("RELAY_IMAGE")).To(Succeed()) })

				labImagesDir, err := os.MkdirTemp("", "lab-images")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(filepath.Join(labImagesDir, "busybox"), []byte("busybox:test"), 0644)).To(Succeed())
				Expect(os.Setenv("LAB_IMAGES_DIR", labImagesDir)).To(Succeed())
				DeferCleanup(func() {
					Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
					Expect(os.RemoveAll(labImagesDir)).To(Succeed())
				})
```

Add `"path/filepath"` to that test file's imports if not already present (check `services/labenv-operator/internal/controller/labenvironment_controller_test.go:17-36` for the existing import block).

Run: `cd services/labenv-operator && go test ./... 2>&1 | tail -30`

Expected: PASS, no failures.

- [ ] **Step 8: Commit**

```bash
git add services/labenv-operator/internal/controller/labimages.go services/labenv-operator/internal/controller/labimages_test.go services/labenv-operator/internal/controller/labenvironment_controller.go services/labenv-operator/internal/controller/labenvironment_controller_test.go
git commit -m "feat(labenv-operator): resolve lab images via mounted ConfigMap instead of LABENV_REGISTRY"
```

---

### Task 2: Mount the `lab-images` ConfigMap on the controller-manager deployment

**Files:**
- Modify: `services/labenv-operator/config/manager/manager.yaml:101-108`

**Interfaces:**
- Consumes: nothing from Task 1 directly, but the resulting mount path must match `defaultLabImagesDir = "/etc/lab-images"` from Task 1's `labimages.go`.
- Produces: the `lab-images` ConfigMap volume mounted at `/etc/lab-images` in the controller-manager pod — Task 3 creates the actual ConfigMap resource this mount references; until Task 3 lands, the deployment will fail to start (expected — these two tasks deploy together, verified manually in Task 4).

This task is a pure manifest edit; there's no Go test for kustomize YAML. Verification is `kustomize build` succeeding and the resulting YAML containing the right volume/mount.

- [ ] **Step 1: Edit `manager.yaml` to add the volume and volume mount**

Read `services/labenv-operator/config/manager/manager.yaml:90-111` first to confirm current content matches, then change:

```yaml
        env:
          - name: RELAY_IMAGE
            valueFrom:
              configMapKeyRef:
                name: relay-exec-image
                key: image
        volumeMounts: []
      volumes: []
```

to:

```yaml
        env:
          - name: RELAY_IMAGE
            valueFrom:
              configMapKeyRef:
                name: relay-exec-image
                key: image
        volumeMounts:
          - name: lab-images
            mountPath: /etc/lab-images
            readOnly: true
      volumes:
        - name: lab-images
          configMap:
            name: lab-images
```

- [ ] **Step 2: Verify the kustomize base still builds**

Run: `kustomize build deploy/base 2>&1 | grep -A10 "name: lab-images"`

Expected: no output yet (the `lab-images` ConfigMap resource itself doesn't exist until Task 3), but the command must not error. Then confirm the volume mount appears on the controller-manager container:

Run: `kustomize build deploy/base 2>&1 | grep -B2 -A4 "mountPath: /etc/lab-images"`

Expected: shows the `lab-images` volumeMount under the `manager` container.

- [ ] **Step 3: Commit**

```bash
git add services/labenv-operator/config/manager/manager.yaml
git commit -m "feat(labenv-operator): mount lab-images ConfigMap on controller-manager"
```

---

### Task 3: Add the `lab-images` ConfigMap and wire it into the base kustomization

**Files:**
- Create: `deploy/base/56-lab-images.yaml`
- Modify: `deploy/base/kustomization.yaml`

**Interfaces:**
- Produces: a `lab-images` ConfigMap in namespace `labenv-operator-system` with key `ubuntu-sshd: ubuntu-sshd:latest`, matching what Task 2's volume mount expects and what Task 4's `resourceSelector` pointer will target.

This follows the exact pattern of `deploy/base/55-relay-exec-image.yaml`, generalized to one key per lab image (today, just `ubuntu-sshd`).

- [ ] **Step 1: Create the ConfigMap manifest**

Read `deploy/base/55-relay-exec-image.yaml` first to confirm the existing pattern (namespace, naming), then create `deploy/base/56-lab-images.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lab-images
  namespace: labenv-operator-system
data:
  ubuntu-sshd: ubuntu-sshd:latest
```

- [ ] **Step 2: Add it to the base kustomization resources list**

Read `deploy/base/kustomization.yaml` first to confirm current resource ordering, then add `56-lab-images.yaml` immediately after `55-relay-exec-image.yaml`:

```yaml
resources:
  - 00-namespace-infra.yaml
  - 05-frontend-config.yaml
  - 10-backend-pvc.yaml
  - 20-backend-deploy.yaml
  - 22-frontend-deploy.yaml
  - 30-backend-svc.yaml
  - 32-frontend-svc.yaml
  - 40-backend-ingress.yaml
  - 49-frontend-ingress.yaml
  - ../../services/labenv-operator/config/default
  - 55-relay-exec-image.yaml
  - 56-lab-images.yaml
  - 50-attempt-controller-secrets.yaml
  - 51-attempt-controller-serviceaccount.yaml
  - 53-attempt-controller.yaml
  - 60-relay-middleware.yaml
  - 61-relay-authenticator.yaml
  - 62-relay-auth-middleware.yaml
```

- [ ] **Step 3: Verify the base builds and the ConfigMap + mount both appear**

Run: `kustomize build deploy/base 2>&1 | grep -B2 -A4 "name: lab-images"`

Expected: two matches — the ConfigMap resource itself (`kind: ConfigMap`, `name: lab-images`, `data: ubuntu-sshd: ubuntu-sshd:latest`) and the volumeMount/volume from Task 2 referencing it by name.

Run: `kustomize build deploy/overlays/dev 2>&1 | grep -B2 -A4 "name: lab-images"`

Expected: same, confirming the dev overlay picks it up transitively through the base.

- [ ] **Step 4: Commit**

```bash
git add deploy/base/56-lab-images.yaml deploy/base/kustomization.yaml
git commit -m "feat(deploy): add lab-images ConfigMap to base kustomization"
```

---

### Task 4: Build `ubuntu-sshd` as a Skaffold artifact and resolve it via resourceSelector

**Files:**
- Modify: `skaffold.yaml`

**Interfaces:**
- Consumes: the `lab-images` ConfigMap's `ubuntu-sshd` key from Task 3 (must already exist in the deployed manifests for `resourceSelector` to find and rewrite it).
- Produces: a Skaffold artifact named `ubuntu-sshd` built from `labs/images/ubuntu-sshd`, and a `resourceSelector` pointer (`.data.ubuntu-sshd`) so Skaffold rewrites the ConfigMap's `ubuntu-sshd` key to the real built tag at deploy time — this is what `loadLabImageRef("ubuntu-sshd")` from Task 1 reads at runtime.

- [ ] **Step 1: Add the `ubuntu-sshd` artifact**

Read `skaffold.yaml` first to confirm current artifact list and indentation, then add a new artifact entry to the `build.artifacts` list (placed after `relay-authenticator`, before the `deploy:` key):

```yaml
    - image: relay-authenticator
      context: services/relay-authenticator
      docker:
        dockerfile: Dockerfile
    - image: ubuntu-sshd
      context: labs/images/ubuntu-sshd
      docker:
        dockerfile: Dockerfile
```

- [ ] **Step 2: Extend `resourceSelector` to cover the new ConfigMap key**

Change:

```yaml
resourceSelector:
  allow:
    - groupKind: "ConfigMap"
      image: [".data.image"]
```

to:

```yaml
resourceSelector:
  allow:
    - groupKind: "ConfigMap"
      image: [".data.image", ".data.ubuntu-sshd"]
```

- [ ] **Step 3: Verify Skaffold accepts the config**

Run: `skaffold diagnose --yaml-only 2>&1 | grep -A6 "image: ubuntu-sshd"`

Expected: shows the `ubuntu-sshd` artifact with `context: labs/images/ubuntu-sshd` and `dockerfile: Dockerfile`. If `skaffold` isn't available in this environment, instead run `skaffold fix --overwrite=false 2>&1 | head -5` or `cat skaffold.yaml | python3 -c "import sys,yaml; yaml.safe_load(sys.stdin)"` to confirm the YAML at least parses; note in the commit message if full `skaffold diagnose` couldn't be run locally and flag it for manual verification against a real cluster (Task 5 covers that).

- [ ] **Step 4: Commit**

```bash
git add skaffold.yaml
git commit -m "feat(deploy): build ubuntu-sshd lab image via skaffold"
```

---

### Task 5: Remove `LABENV_REGISTRY` references, `make labs-build`, and verify end-to-end on k3d

**Files:**
- Modify: `Makefile`
- Search-verify (no code changes expected, but must confirm no leftover references): `services/labenv-operator/`, `deploy/`, `README.md`

**Interfaces:**
- Consumes: everything from Tasks 1-4 — this task is the integration check across the whole chain (Skaffold build → ConfigMap rewrite → volume mount → `loadLabImageRef` → pod image).

- [ ] **Step 1: Confirm no remaining `LABENV_REGISTRY` references**

Run: `grep -rn "LABENV_REGISTRY" --include="*.go" --include="*.yaml" --include="Makefile" --include="*.md" .`

Expected: no matches. (Task 1 already removed the only Go usage; this step is a safety net in case it's referenced in a manifest or doc that wasn't touched yet — if found, remove it there too, following the same reasoning as Task 1 Step 5.)

- [ ] **Step 2: Remove the `labs-build` target and its use in `dev-cluster`**

Read `Makefile` first to confirm current content, then:

Change:

```makefile
dev-cluster: .dev-cluster-remove .dev-cluster-create dev-rebuild .wait-backend .dev-dbusers-init .dev-labs-sync labs-build
```

to:

```makefile
dev-cluster: .dev-cluster-remove .dev-cluster-create dev-rebuild .wait-backend .dev-dbusers-init .dev-labs-sync
```

And remove the now-orphaned target entirely:

```makefile
labs-build:
	docker build -t ubuntu-sshd:latest labs/images/ubuntu-sshd
	k3d image import ubuntu-sshd:latest -c rootenv

```

Also remove `labs-build` from the `.PHONY` line at the top of the file:

Change:

```makefile
.PHONY: dev-cluster dev prod-deploy dbusers-init labs-sync labs-build
```

to:

```makefile
.PHONY: dev-cluster dev prod-deploy dbusers-init labs-sync
```

- [ ] **Step 3: Bring up a local k3d cluster end-to-end and confirm a lab pod gets the Skaffold-built image**

Run: `make dev-cluster 2>&1 | tail -60`

Expected: cluster comes up, `skaffold run` builds all artifacts including `ubuntu-sshd`, deploy succeeds, backend becomes available, labs sync runs. Watch for any error about `/etc/lab-images/ubuntu-sshd` not found — that would mean the ConfigMap rewrite or volume mount isn't wired correctly and Tasks 2-4 need re-checking.

- [ ] **Step 4: Provision a lab environment using the `ubuntu-sshd` image and confirm the pod's image ref**

Use whatever existing flow creates a `LabEnvironment` CR for manual testing (check `services/labenv-operator/test/e2e/e2e_test.go` for the pattern, or apply one directly):

```bash
kubectl apply -f - <<'EOF'
apiVersion: lab.rootenv.io/v1alpha1
kind: LabEnvironment
metadata:
  name: smoke-test
spec:
  ownerId: smoke-test-owner
  labId: smoke-test-lab
  assets:
    - name: main
      image: ubuntu-sshd
EOF
kubectl wait --for=condition=Ready pod/main -n rootenv-lab-smoke-test --timeout=60s
kubectl get pod main -n rootenv-lab-smoke-test -o jsonpath='{.spec.containers[0].image}'
```

Expected: the printed image is the Skaffold-built local tag for `ubuntu-sshd` (not the literal string `ubuntu-sshd`), confirming the full chain — Skaffold build → `lab-images` ConfigMap rewrite → volume mount → `loadLabImageRef` → pod spec — works end to end.

Clean up:

```bash
kubectl delete labenvironment smoke-test
```

- [ ] **Step 5: Run the full operator and any deploy-related test suites one more time**

Run: `cd services/labenv-operator && go test ./... 2>&1 | tail -30`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "chore(deploy): remove make labs-build now that skaffold builds lab images"
```

---

## Self-Review Notes

- **Spec coverage:** Section 1 (Skaffold artifact per `labs/images/*` dir) → Task 4. Section 2 (ConfigMap + resourceSelector) → Tasks 3-4. Section 3 (volume mount + `loadLabImageRef`, no silent fallback) → Tasks 1-2. Section 4 (local dev needs no separate path, `make labs-build` removed) → Task 5. "Out of scope" (no auto-discovery) → respected throughout; every task adds entries by hand.
- **Type consistency:** `loadLabImageRef(name string) (string, error)` defined in Task 1 is the only new Go symbol, used only within `ensurePod` in the same task — no cross-task Go signature drift to check.
- **Manifest/code path consistency:** Task 1's `defaultLabImagesDir = "/etc/lab-images"` matches Task 2's `mountPath: /etc/lab-images` and Task 3's ConfigMap name `lab-images` matches Task 2's `configMap.name: lab-images`. Task 4's `resourceSelector` pointer `.data.ubuntu-sshd` matches Task 3's ConfigMap key `ubuntu-sshd`.
