# relay-exec Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire up relay-exec end-to-end so each lab namespace runs a relay-exec pod reachable from the browser at `/relay/exec/<attemptId>/<assetName>/`, with SSH removed from the frontend entirely.

**Architecture:** The operator deploys `relay-exec` (instead of `relay-primitive`) with `RELAY_SKIP_AUTH=true` and injects the attempt/owner IDs as env vars. The frontend drops all SSH code and gains a new `useExecRelayConnection` composable + `ExecTerminalPanel` component that connect to `/relay/exec/<attemptId>/<assetName>/`. Traefik routes by ingress path (`/relay/exec/<attemptId>`) and strips the prefix before forwarding to the relay pod.

**Tech Stack:** Go 1.26, controller-runtime, Kubernetes networking/apps/rbac APIs, Vue 3 (Composition API), xterm.js, Vitest, Ginkgo/Gomega.

## Global Constraints

- All relay Go code lives in `services/relay/` (module `github.com/alexsviridov/linuxlab/relay`)
- All operator Go code lives in `services/labenv-operator/` (module `github.com/alex-sviridov/rootenv/services/labenv-operator`)
- Frontend tests: `cd services/frontend && npm run test:unit -- --run`
- Relay tests: `cd services/relay && go test ./...`
- Operator tests: `cd services/labenv-operator && go test ./...`
- Commit format: `<type>: <what>` (feat/fix/chore/refactor)
- `master` branch; tests must pass before every commit
- No squash/force-push

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Modify | `services/relay/pkg/relaybase/handler.go` | Add `SkipAuth bool` field; skip header checks when true |
| Modify | `services/relay/pkg/relaybase/handler_test.go` | Tests for skip-auth mode |
| Modify | `services/relay/cmd/relay-exec/main.go` | Read `RELAY_SKIP_AUTH`; make attempt/owner IDs optional when skipping |
| Create | `services/relay/cmd/relay-exec/Dockerfile` | Build image for relay-exec |
| Modify | `services/labenv-operator/internal/controller/relay.go` | Switch to relay-exec: names, labels, env vars, ingress path, probe, NetworkPolicy |
| Modify | `services/labenv-operator/internal/controller/labenvironment_controller_test.go` | Update ensureRelay test assertions |
| Delete | `services/frontend/src/composables/useSshRelayConnection.js` | Remove SSH composable |
| Delete | `services/frontend/src/composables/__tests__/useRelayConnection.spec.js` | Remove SSH composable tests |
| Delete | `services/frontend/src/components/lab/TerminalPanel.vue` | Remove SSH terminal panel |
| Create | `services/frontend/src/composables/useExecRelayConnection.js` | Exec WebSocket composable |
| Create | `services/frontend/src/composables/__tests__/useExecRelayConnection.spec.js` | Tests for exec composable |
| Create | `services/frontend/src/components/lab/ExecTerminalPanel.vue` | Exec terminal panel component |
| Modify | `services/frontend/src/composables/useLabSession.js` | Return `attemptId`; remove `secrets` |
| Modify | `services/frontend/src/views/LabView.vue` | Destructure `attemptId` instead of `secrets`; pass to `LabConsole` |
| Modify | `services/frontend/src/components/lab/LabConsole.vue` | Replace SSH panel with exec; accept `attemptId` prop; remove `secrets` gate |
| Modify | `services/frontend/src/composables/__tests__/useTerminalTabs.spec.js` | Replace `'ssh'` strings with `'exec'` |
| Modify | `skaffold.yaml` | Replace relay-primitive artifact with relay-exec |
| Modify | `deploy/overlays/dev/kustomization.yaml` | Update `RELAY_EXEC_IMAGE` and add `RELAY_EXEC_INGRESS_BASE_PATH` |

---

## Task 1: Add SkipAuth to relaybase.Handler

**Files:**
- Modify: `services/relay/pkg/relaybase/handler.go`
- Modify: `services/relay/pkg/relaybase/handler_test.go`

**Interfaces:**
- Produces: `Handler.SkipAuth bool` field — when true, bypasses attempt-ID and user-ID header checks and uses `"anonymous"` as userID

- [ ] **Step 1: Write the failing tests**

Add to `services/relay/pkg/relaybase/handler_test.go` (after the existing tests):

```go
func TestHandler_skip_auth_calls_backend(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		SkipAuth:    true,
		AuthTimeout: 2 * time.Second,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No X-Attempt-Id or X-User-Id headers injected
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte("tok")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve was not called with SkipAuth=true")
	}
	fb.mu.Lock()
	if fb.userID != "anonymous" {
		t.Errorf("userID: got %q, want %q", fb.userID, "anonymous")
	}
	fb.mu.Unlock()
}

func TestHandler_skip_auth_ignores_wrong_attempt_id(t *testing.T) {
	fb := &fakeBackend{}
	h := &relaybase.Handler{
		Backend:     fb,
		Limiter:     relaybase.NewConnLimiter(10),
		AttemptID:   "atm_123",
		SkipAuth:    true,
		AuthTimeout: 2 * time.Second,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Attempt-Id", "atm_WRONG")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	conn := dialWS(t, srv)
	defer conn.CloseNow()
	_ = conn.Write(context.Background(), websocket.MessageText, []byte("tok"))
	time.Sleep(100 * time.Millisecond)
	if !fb.wasCalled() {
		t.Error("Backend.Serve should still be called when SkipAuth=true")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd services/relay && go test ./pkg/relaybase/ -run TestHandler_skip -v
```

Expected: `FAIL` — `relaybase.Handler` has no `SkipAuth` field.

- [ ] **Step 3: Add SkipAuth to Handler**

In `services/relay/pkg/relaybase/handler.go`, add `SkipAuth bool` to the struct and update `ServeHTTP`:

```go
type Handler struct {
	Backend        Backend
	Limiter        *ConnLimiter
	AttemptID      string
	OwnerID        string
	SkipAuth       bool          // when true, skip X-Attempt-Id and X-User-Id checks
	AllowedOrigins []string
	AuthTimeout    time.Duration
	WG             *sync.WaitGroup
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assetName := strings.Trim(r.PathValue("assetName"), "/")
	if assetName == "" {
		assetName = strings.Trim(strings.SplitN(r.URL.Path, "/", 3)[1], "/")
	}
	if assetName == "" {
		http.Error(w, "missing asset name", http.StatusBadRequest)
		return
	}

	log := slog.With("asset", assetName, "remote", r.RemoteAddr)

	acceptOpts := &websocket.AcceptOptions{}
	if len(h.AllowedOrigins) > 0 {
		acceptOpts.OriginPatterns = h.AllowedOrigins
	} else {
		acceptOpts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		log.Warn("ws accept failed", "err", err)
		return
	}

	authTimeout := h.AuthTimeout
	if authTimeout == 0 {
		authTimeout = 10 * time.Second
	}
	authCtx, authCancel := context.WithTimeout(r.Context(), authTimeout)
	_, _, err = conn.Read(authCtx)
	authCancel()
	if err != nil {
		log.Warn("auth failed: no first message received", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}

	var userID string
	if h.SkipAuth {
		userID = "anonymous"
	} else {
		attemptID := r.Header.Get("X-Attempt-Id")
		userID = r.Header.Get("X-User-Id")

		if attemptID != h.AttemptID {
			log.Warn("security: X-Attempt-Id mismatch", "got", attemptID, "want", h.AttemptID)
			_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
			return
		}
		if userID == "" {
			log.Warn("security: missing X-User-Id")
			_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
			return
		}
	}

	if err := h.Limiter.Acquire(userID); err != nil {
		log.Warn("connection limit exceeded", "user_id", userID)
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	defer h.Limiter.Release(userID)

	log = log.With("user_id", userID)
	log.Info("ws connected", "active_total", h.Limiter.Total())

	if h.WG != nil {
		h.WG.Add(1)
		defer h.WG.Done()
	}

	if err := h.Backend.Serve(r.Context(), conn, assetName, userID); err != nil {
		log.Error("backend error", "err", err)
	}

	log.Info("ws disconnected")
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
```

- [ ] **Step 4: Run all relay tests**

```bash
cd services/relay && go test ./...
```

Expected: all pass including the two new skip-auth tests.

- [ ] **Step 5: Commit**

```bash
git add services/relay/pkg/relaybase/handler.go services/relay/pkg/relaybase/handler_test.go
git commit -m "feat(relay): add SkipAuth mode to relaybase.Handler"
```

---

## Task 2: Update relay-exec main and add Dockerfile

**Files:**
- Modify: `services/relay/cmd/relay-exec/main.go`
- Create: `services/relay/cmd/relay-exec/Dockerfile`

**Interfaces:**
- Consumes: `Handler.SkipAuth bool` from Task 1
- Produces: `relay-exec` binary and Docker image; env vars `RELAY_SKIP_AUTH`, `RELAY_MY_ATTEMPT_ID` (optional when skip), `RELAY_MY_OWNER_ID` (optional when skip), `RELAY_MY_NAMESPACE` (required)

- [ ] **Step 1: Update main.go**

Replace `services/relay/cmd/relay-exec/main.go` with:

```go
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	skipAuth := os.Getenv("RELAY_SKIP_AUTH") == "true"

	attemptID := os.Getenv("RELAY_MY_ATTEMPT_ID")
	if attemptID == "" && !skipAuth {
		slog.Error("RELAY_MY_ATTEMPT_ID is required (set RELAY_SKIP_AUTH=true to skip auth)")
		os.Exit(1)
	}
	ownerID := os.Getenv("RELAY_MY_OWNER_ID")
	if ownerID == "" && !skipAuth {
		slog.Error("RELAY_MY_OWNER_ID is required (set RELAY_SKIP_AUTH=true to skip auth)")
		os.Exit(1)
	}
	namespace := os.Getenv("RELAY_MY_NAMESPACE")
	if namespace == "" {
		slog.Error("RELAY_MY_NAMESPACE is required")
		os.Exit(1)
	}

	port := os.Getenv("RELAY_PORT")
	if port == "" {
		port = "8080"
	}

	var origins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}

	kubeExecer, err := exec.NewKubeExecer()
	if err != nil {
		slog.Error("failed to create kube execer", "err", err)
		os.Exit(1)
	}

	backend := exec.Backend{Namespace: namespace, Execer: kubeExecer}
	limiter := relaybase.NewConnLimiter(16)

	var wg sync.WaitGroup
	handler := &relaybase.Handler{
		Backend:        &backend,
		Limiter:        limiter,
		AttemptID:      attemptID,
		OwnerID:        ownerID,
		SkipAuth:       skipAuth,
		AllowedOrigins: origins,
		WG:             &wg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/{assetName}/", handler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("relay-exec starting", "port", port, "skip_auth", skipAuth, "attempt_id", attemptID, "namespace", namespace)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all sessions drained")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timeout: sessions still active")
	}
}
```

- [ ] **Step 2: Create Dockerfile**

Create `services/relay/cmd/relay-exec/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o relay-exec ./cmd/relay-exec

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 relay && adduser -S -u 10001 -G relay relay
WORKDIR /app
COPY --from=builder /app/relay-exec .
EXPOSE 8080
USER relay
CMD ["./relay-exec"]
```

- [ ] **Step 3: Verify relay builds**

```bash
cd services/relay && go build ./cmd/relay-exec/
```

Expected: exits 0, produces `relay-exec` binary in `services/relay/`.

- [ ] **Step 4: Run relay tests**

```bash
cd services/relay && go test ./...
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add services/relay/cmd/relay-exec/main.go services/relay/cmd/relay-exec/Dockerfile
git commit -m "feat(relay-exec): add RELAY_SKIP_AUTH support and Dockerfile"
```

---

## Task 3: Update operator to deploy relay-exec

**Files:**
- Modify: `services/labenv-operator/internal/controller/relay.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller_test.go`

**Interfaces:**
- Produces: operator creates `relay-exec` deployment (not `relay-primitive`) with `RELAY_MY_ATTEMPT_ID=env.Name`, `RELAY_MY_OWNER_ID=env.Spec.OwnerId`, `RELAY_SKIP_AUTH=true`, `RELAY_MY_NAMESPACE=nsName`; ingress path `/relay/exec/<envName>`; NetworkPolicy selects `app: relay-exec`

- [ ] **Step 1: Update relay.go**

Apply these changes to `services/labenv-operator/internal/controller/relay.go`:

**a) Change default ingress base path** in `loadRelayConfig`:
```go
basePath := os.Getenv("RELAY_EXEC_INGRESS_BASE_PATH")
if basePath == "" {
    basePath = "/relay/exec"
}
```

**b) Replace `ensureRelayDeployment`** — change name, labels, container name, env vars, and readiness probe:
```go
func (r *LabEnvironmentReconciler) ensureRelayDeployment(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string, cfg relayConfig) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-exec"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay-exec",
			Namespace: nsName,
			Labels:    map[string]string{"app": "relay-exec"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "relay-exec"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "relay-exec"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "relay",
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(10001)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "relay-exec",
							Image:           cfg.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "RELAY_MY_NAMESPACE", Value: nsName},
								{Name: "RELAY_MY_ATTEMPT_ID", Value: env.Name},
								{Name: "RELAY_MY_OWNER_ID", Value: env.Spec.OwnerId},
								{Name: "RELAY_SKIP_AUTH", Value: "true"},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &deploy))
}
```

**c) Update `ensureRelayService`** — change selector from `relay-primitive` to `relay-exec`:
```go
func (r *LabEnvironmentReconciler) ensureRelayService(ctx context.Context, nsName string) error {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "relay",
			Namespace: nsName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "relay-exec"},
			Ports: []corev1.ServicePort{
				{Port: 8080, TargetPort: intstr.FromInt32(8080)},
			},
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &svc))
}
```

**d) Update `ensureRelayNetworkPolicy`** — change `podSelector` from `relay-primitive` to `relay-exec`:
```go
Spec: networkingv1.NetworkPolicySpec{
    PodSelector: metav1.LabelSelector{
        MatchLabels: map[string]string{"app": "relay-exec"},
    },
    // ... rest unchanged
```

- [ ] **Step 2: Update the controller test**

In `services/labenv-operator/internal/controller/labenvironment_controller_test.go`, find the `ensureRelay` `Describe` block and update these assertions:

**a) In `BeforeEach`**, change env var values:
```go
os.Setenv("RELAY_EXEC_IMAGE", "relay-exec:test")
os.Setenv("RELAY_INGRESS_CLASS", "traefik")
os.Setenv("RELAY_EXEC_INGRESS_BASE_PATH", "/relay/exec")
os.Setenv("RELAY_INGRESS_ANNOTATIONS", "traefik.ingress.kubernetes.io/router.entrypoints=websecure")
```

**b) In the `DeferCleanup`**, also unset `RELAY_EXEC_INGRESS_BASE_PATH` (already present, no change needed there since the existing defer clears it).

**c) In the `"creates all relay resources"` It block**, update these By assertions:

```go
By("Deployment relay-exec exists with correct image and env")
var deploy appsv1.Deployment
Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay-exec"}, &deploy)).To(Succeed())
Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("relay-exec:test"))
Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
    corev1.EnvVar{Name: "RELAY_MY_NAMESPACE", Value: nsName},
    corev1.EnvVar{Name: "RELAY_MY_ATTEMPT_ID", Value: envName},
    corev1.EnvVar{Name: "RELAY_MY_OWNER_ID", Value: "usr-test"},
    corev1.EnvVar{Name: "RELAY_SKIP_AUTH", Value: "true"},
))
```

```go
By("Ingress relay exists with correct path and annotation")
var ing networkingv1.Ingress
Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
Expect(ing.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/relay/exec/" + envName))
Expect(ing.Annotations).To(HaveKey("traefik.ingress.kubernetes.io/router.entrypoints"))
Expect(*ing.Spec.IngressClassName).To(Equal("traefik"))
```

```go
By("NetworkPolicy allow-traefik-to-relay exists")
var np networkingv1.NetworkPolicy
Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "allow-traefik-to-relay"}, &np)).To(Succeed())
Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", "relay-exec"))
```

- [ ] **Step 3: Run operator tests**

```bash
cd services/labenv-operator && go test ./...
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add services/labenv-operator/internal/controller/relay.go \
        services/labenv-operator/internal/controller/labenvironment_controller_test.go
git commit -m "feat(labenv-operator): deploy relay-exec instead of relay-primitive"
```

---

## Task 4: Remove SSH from frontend

**Files:**
- Delete: `services/frontend/src/composables/useSshRelayConnection.js`
- Delete: `services/frontend/src/composables/__tests__/useRelayConnection.spec.js`
- Delete: `services/frontend/src/components/lab/TerminalPanel.vue`

**Interfaces:**
- Consumes: nothing (pure removal)
- Produces: no SSH files remain; tests still pass

- [ ] **Step 1: Delete SSH files**

```bash
rm services/frontend/src/composables/useSshRelayConnection.js
rm services/frontend/src/composables/__tests__/useRelayConnection.spec.js
rm services/frontend/src/components/lab/TerminalPanel.vue
```

- [ ] **Step 2: Verify no remaining imports of deleted files**

```bash
grep -r "useSshRelayConnection\|TerminalPanel" services/frontend/src --include="*.vue" --include="*.js"
```

Expected: no output (we will wire up exec in Task 6).

- [ ] **Step 3: Run frontend tests**

```bash
cd services/frontend && npm run test:unit -- --run
```

Expected: all pass (the deleted spec file is gone; no other test referenced these files).

- [ ] **Step 4: Commit**

```bash
git add -A services/frontend/src/composables/useSshRelayConnection.js \
           services/frontend/src/composables/__tests__/useRelayConnection.spec.js \
           services/frontend/src/components/lab/TerminalPanel.vue
git commit -m "chore(frontend): remove SSH relay composable and panel"
```

---

## Task 5: Add useExecRelayConnection composable

**Files:**
- Create: `services/frontend/src/composables/useExecRelayConnection.js`
- Create: `services/frontend/src/composables/__tests__/useExecRelayConnection.spec.js`

**Interfaces:**
- Produces: `useExecRelayConnection(attemptId, assetName)` → `{ terminal, fitAddon }`
  - URL: `/relay/exec/${attemptId}/${assetName}/`
  - First WS message on open: `pb.authStore.token` (string)
  - Binary frames: `\x01` + cols (uint16 LE) + rows (uint16 LE) for resize; other bytes forwarded as stdin
  - `onclose`: writes `\r\nDisconnected (code ${e.code}${reason})` to terminal
  - `onerror`: writes `\r\nConnection error` to terminal
  - Unmount: closes WS with code 1000 + `"tab closed"`, disposes terminal

- [ ] **Step 1: Write the tests**

Create `services/frontend/src/composables/__tests__/useExecRelayConnection.spec.js`:

```js
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { withSetup } from './utils'

const { mockToken } = vi.hoisted(() => ({ mockToken: 'test-token' }))

vi.mock('@/lib/pb', () => ({
  pb: { authStore: { get token() { return mockToken } } },
}))

vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    constructor() {
      this.buffer = []
      this.dataHandler = null
      this.resizeHandler = null
      this.selectionHandler = null
    }
    writeln(text) { this.buffer.push(text) }
    write(data) { this.buffer.push(data) }
    onData(handler) { this.dataHandler = handler }
    onResize(handler) { this.resizeHandler = handler }
    onSelectionChange(handler) { this.selectionHandler = handler }
    getSelection() { return '' }
    input(text) { this.buffer.push(text) }
    loadAddon() {}
    dispose() {}
    open() {}
    get textarea() { return { addEventListener: vi.fn() } }
  }
  return { Terminal: MockTerminal }
})

vi.mock('@xterm/addon-fit', () => {
  class MockFitAddon { fit() {} }
  return { FitAddon: MockFitAddon }
})

vi.mock('@xterm/addon-web-links', () => {
  class MockWebLinksAddon {}
  return { WebLinksAddon: MockWebLinksAddon }
})

import { useExecRelayConnection } from '../useExecRelayConnection'

class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    this.binaryType = null
    MockWebSocket.lastInstance = this
  }
  send(data) { this.sent.push(data) }
  close(code, reason) { this._closedWith = { code, reason } }
}
MockWebSocket.CONNECTING = 0
MockWebSocket.OPEN = 1
MockWebSocket.CLOSING = 2
MockWebSocket.CLOSED = 3

beforeEach(() => {
  MockWebSocket.lastInstance = null
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:8080' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useExecRelayConnection', () => {
  it('opens WebSocket at /relay/exec/<attemptId>/<assetName>/ (ws for http)', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/exec/atm_123/workstation/')
    unmount()
  })

  it('opens WebSocket at wss when protocol is https', async () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' })

    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.url).toContain('wss://')
    unmount()
  })

  it('sets binaryType to arraybuffer', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.binaryType).toBe('arraybuffer')
    unmount()
  })

  it('sends only the token as first message on open (no secret)', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([mockToken])
    unmount()
  })

  it('registers onData handler to forward terminal input to ws', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(result.terminal.dataHandler).not.toBeNull()
    unmount()
  })

  it('forwards terminal input through ws when onData fires', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    MockWebSocket.lastInstance.onopen()
    result.terminal.dataHandler('ls')

    expect(MockWebSocket.lastInstance.sent).toContain('ls')
    unmount()
  })

  it('writes binary data from ws messages to terminal', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onmessage({ data: new ArrayBuffer(4) })

    expect(result.terminal.buffer.some(item => item instanceof Uint8Array)).toBe(true)
    unmount()
  })

  it('writes disconnect message on close with code and reason', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1008, reason: 'unauthorized' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1008')
    expect(output).toContain('unauthorized')
    unmount()
  })

  it('writes disconnect message with code only when reason is empty', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1000')
    expect(output).not.toMatch(/1000.*:/)
    unmount()
  })

  it('writes connection error on ws error', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onerror()

    expect(result.terminal.buffer.some(m => m.includes('Connection error'))).toBe(true)
    unmount()
  })

  it('closes ws with code 1000 on unmount when open', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toEqual({ code: 1000, reason: 'tab closed' })
  })

  it('does not close ws when already closing on unmount', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.CLOSING
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toBeUndefined()
  })

  it('disposes terminal on unmount', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    const spy = vi.spyOn(result.terminal, 'dispose')
    unmount()

    expect(spy).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd services/frontend && npm run test:unit -- --run --reporter=verbose 2>&1 | grep -E "FAIL|useExec"
```

Expected: `FAIL` — module `useExecRelayConnection` not found.

- [ ] **Step 3: Create the composable**

Create `services/frontend/src/composables/useExecRelayConnection.js`:

```js
import { onMounted, onUnmounted } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { pb } from '@/lib/pb'

const SCROLLBACK_LINES = 10000

export function useExecRelayConnection(attemptId, assetName) {
  const terminal = new Terminal({
    scrollback: SCROLLBACK_LINES,
    cursorBlink: true,
    cursorStyle: 'block',
    fontFamily: 'monospace',
    fontSize: 12,
    theme: {
      background: '#0f172a',
      foreground: '#cbd5e1',
      cursor: '#cbd5e1',
    },
  })

  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.loadAddon(new WebLinksAddon())

  let ws = null
  let onDataHandler = null
  let isUnmounting = false

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/exec/${attemptId}/${assetName}/`
    ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      ws.send(pb.authStore.token)

      onDataHandler = (data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(data)
        }
      }
      terminal.onData(onDataHandler)

      terminal.onResize(({ cols, rows }) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          const buf = new ArrayBuffer(5)
          const view = new DataView(buf)
          view.setUint8(0, 0x01)
          view.setUint16(1, cols, true)
          view.setUint16(3, rows, true)
          ws.send(buf)
        }
      })

      fitAddon.fit()
      window.addEventListener('resize', () => fitAddon.fit())
    }

    ws.onmessage = (e) => {
      terminal.write(new Uint8Array(e.data))
    }

    ws.onclose = (e) => {
      if (isUnmounting) return
      const reason = e.reason ? `: ${e.reason}` : ''
      terminal.writeln(`\r\nDisconnected (code ${e.code}${reason})`)
    }

    ws.onerror = () => {
      terminal.writeln('\r\nConnection error')
    }
  }

  onMounted(() => {
    terminal.writeln('Connecting…')
    connect()
  })

  onUnmounted(() => {
    isUnmounting = true
    if (ws) {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'tab closed')
      }
    }
    terminal.dispose()
  })

  return { terminal, fitAddon }
}
```

- [ ] **Step 4: Run tests**

```bash
cd services/frontend && npm run test:unit -- --run
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add services/frontend/src/composables/useExecRelayConnection.js \
        services/frontend/src/composables/__tests__/useExecRelayConnection.spec.js
git commit -m "feat(frontend): add useExecRelayConnection composable"
```

---

## Task 6: Wire exec panel into LabConsole, LabView, and useLabSession

**Files:**
- Create: `services/frontend/src/components/lab/ExecTerminalPanel.vue`
- Modify: `services/frontend/src/composables/useLabSession.js`
- Modify: `services/frontend/src/views/LabView.vue`
- Modify: `services/frontend/src/components/lab/LabConsole.vue`
- Modify: `services/frontend/src/composables/__tests__/useTerminalTabs.spec.js`

**Interfaces:**
- Consumes: `useExecRelayConnection(attemptId, assetName)` from Task 5
- `ExecTerminalPanel` props: `{ assetName: String, attemptId: String }`
- `useLabSession` returns: adds `attemptId: computed(() => attemptsStore.lastAttempt?.id ?? null)`; removes `secrets`
- `LabConsole` new prop: `attemptId: String` (replaces `secrets`)

- [ ] **Step 1: Create ExecTerminalPanel.vue**

Create `services/frontend/src/components/lab/ExecTerminalPanel.vue`:

```vue
<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useExecRelayConnection } from '@/composables/useExecRelayConnection'

const props = defineProps({
  assetName: { type: String, required: true },
  attemptId: { type: String, required: true },
})

const termEl = ref(null)
const { terminal, fitAddon } = useExecRelayConnection(props.attemptId, props.assetName)
const showAltWHint = ref(false)
let resizeObserver = null
let terminalFocused = false
let altWHintTimer = null

const CTRL_KEY_MAP = {
  t: '\x14', r: '\x12', n: '\x0e',
  a: '\x01', e: '\x05', k: '\x0b', u: '\x15',
  l: '\x0c', c: '\x03', z: '\x1a', d: '\x04',
  f: '\x06', b: '\x02', p: '\x10', q: '\x11',
}

function onDocumentKeydown(e) {
  if (!terminalFocused) return

  if (e.altKey && !e.ctrlKey && !e.metaKey && e.key.toLowerCase() === 'w') {
    e.preventDefault()
    e.stopPropagation()
    terminal.input('\x17')
    return
  }

  if (!e.ctrlKey || e.altKey || e.metaKey) return
  const seq = CTRL_KEY_MAP[e.key.toLowerCase()]
  if (!seq) return
  e.preventDefault()
  e.stopPropagation()
  terminal.input(seq)
}

function onBeforeUnload(e) {
  if (!terminalFocused) return
  e.preventDefault()
  e.returnValue = ''
  altWHintTimer = setTimeout(() => {
    showAltWHint.value = true
    altWHintTimer = setTimeout(() => { showAltWHint.value = false }, 5000)
  }, 500)
}

function onContextMenu(e) {
  e.preventDefault()
  navigator.clipboard.readText().then((text) => {
    if (text) terminal.input(text)
  }).catch(() => {})
}

onMounted(() => {
  if (!termEl.value) return

  terminal.open(termEl.value)
  fitAddon.fit()

  resizeObserver = new ResizeObserver(() => fitAddon.fit())
  resizeObserver.observe(termEl.value)

  terminal.textarea?.addEventListener('focus', () => { terminalFocused = true })
  terminal.textarea?.addEventListener('blur', () => { terminalFocused = false })

  terminal.onSelectionChange(() => {
    const sel = terminal.getSelection()
    if (sel) navigator.clipboard.writeText(sel).catch(() => {})
  })

  termEl.value.addEventListener('contextmenu', onContextMenu)
  document.addEventListener('keydown', onDocumentKeydown, true)
  window.addEventListener('beforeunload', onBeforeUnload)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  clearTimeout(altWHintTimer)
  document.removeEventListener('keydown', onDocumentKeydown, true)
  window.removeEventListener('beforeunload', onBeforeUnload)
  termEl.value?.removeEventListener('contextmenu', onContextMenu)
})
</script>

<template>
  <div class="relative w-full h-full">
    <div ref="termEl" class="w-full h-full" />
    <div
      v-if="showAltWHint"
      class="absolute bottom-4 right-4 flex items-center gap-3 bg-slate-700 text-slate-100 text-sm px-4 py-2 rounded shadow-lg z-50"
    >
      <span>Tip: use <kbd class="bg-slate-600 px-1 rounded">Alt+W</kbd> to send Ctrl+W to the terminal.</span>
      <button @click="showAltWHint = false" class="text-slate-400 hover:text-white">✕</button>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Update useLabSession.js**

In `services/frontend/src/composables/useLabSession.js`:

Add `computed` to the import (already present — no change), then add `attemptId` after the `useTerminalTabs` destructure:

```js
const attemptId = computed(() => attemptsStore.lastAttempt?.id ?? null)
```

Update the `return` statement:

```js
return {
  lab, selectedTask, currentTask, error,
  tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
  attemptId,
}
```

- [ ] **Step 3: Update LabView.vue**

In `services/frontend/src/views/LabView.vue`, update the destructure from `useLabSession()`:

```js
const {
  lab, selectedTask, currentTask, error,
  tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
  attemptId,
} = useLabSession()
```

(Remove `secrets`.)

In the template, find `<LabConsole` and replace `:secrets="secrets"` with `:attempt-id="attemptId"`:

```html
<LabConsole
  :tabs="tabs"
  :active-tab-id="activeTabId"
  :limit-error="limitError"
  :attempt-id="attemptId"
  @select-tab="selectTab"
  @close-tab="closeTab"
  @move-tab="moveTab($event.from, $event.to)"
/>
```

- [ ] **Step 4: Update LabConsole.vue**

Replace the full `<script setup>` section of `services/frontend/src/components/lab/LabConsole.vue`:

```js
import { ref } from 'vue'
import ExecTerminalPanel from '@/components/lab/ExecTerminalPanel.vue'

const tabComponents = { exec: ExecTerminalPanel }

defineProps({
  tabs: { type: Array, required: true },
  activeTabId: { type: String, default: null },
  limitError: { type: String, default: null },
  attemptId: { type: String, default: null },
})

const emit = defineEmits(['select-tab', 'close-tab', 'move-tab'])

const dragFrom = ref(null)

function onDragStart(e, tabId) {
  dragFrom.value = tabId
  e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(e, tabId) {
  if (dragFrom.value && dragFrom.value !== tabId) {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
  }
}

function onDrop(e, tabId) {
  e.preventDefault()
  if (dragFrom.value && dragFrom.value !== tabId) {
    emit('move-tab', { from: dragFrom.value, to: tabId })
  }
  dragFrom.value = null
}

function onDragEnd() {
  dragFrom.value = null
}
```

In the template, update the `<component>` rendering to pass exec-specific props and remove the `secrets` gate:

```html
<!-- Terminal panels (v-show keeps WS alive when switching) -->
<div class="flex-1 overflow-hidden relative">
  <template v-if="tabs.length">
    <template v-for="tab in tabs" :key="tab.id">
      <component
        :is="tabComponents[tab.type]"
        v-if="tabComponents[tab.type]"
        v-show="tab.id === activeTabId"
        :asset-name="tab.serverId"
        :attempt-id="attemptId"
      />
    </template>
  </template>
  <div v-else class="flex items-center justify-center h-full px-6 text-center">
    <span v-if="limitError" class="text-xs text-amber-400">{{ limitError }}</span>
    <span v-else class="text-xl text-slate-600">No active terminal connection — click a protocol badge on a provisioned server to connect.</span>
  </div>
</div>
```

- [ ] **Step 5: Update terminal tabs tests — replace 'ssh' with 'exec'**

In `services/frontend/src/composables/__tests__/useTerminalTabs.spec.js`, replace all occurrences of `'ssh'` with `'exec'` and all occurrences of `'rdp'` with `'http'` (the type strings are arbitrary to the tab logic; we pick protocols from the actual schema):

```bash
sed -i "s/'ssh'/'exec'/g; s/'rdp'/'http'/g" \
  services/frontend/src/composables/__tests__/useTerminalTabs.spec.js
```

Also update the string in the one test that checks `tab.type`:
```
expect(tabs.value[0].type).toBe('exec')
```
(This is handled by the `sed` above.)

- [ ] **Step 6: Run frontend tests**

```bash
cd services/frontend && npm run test:unit -- --run
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add services/frontend/src/components/lab/ExecTerminalPanel.vue \
        services/frontend/src/composables/useLabSession.js \
        services/frontend/src/views/LabView.vue \
        services/frontend/src/components/lab/LabConsole.vue \
        services/frontend/src/composables/__tests__/useTerminalTabs.spec.js
git commit -m "feat(frontend): wire exec relay panel; remove SSH"
```

---

## Task 7: Update Skaffold and dev overlay

**Files:**
- Modify: `skaffold.yaml`
- Modify: `deploy/overlays/dev/kustomization.yaml`

**Interfaces:**
- Produces: `relay-exec` image built and substituted into operator env var `RELAY_EXEC_IMAGE`; `RELAY_EXEC_INGRESS_BASE_PATH=/relay/exec` set in operator deployment

- [ ] **Step 1: Update skaffold.yaml**

In `skaffold.yaml`, find and replace the relay-primitive artifact block:

```yaml
    - image: relay-primitive
      context: services/relay
      docker:
        dockerfile: cmd/relay-primitive/Dockerfile
```

Replace with:

```yaml
    - image: relay-exec
      context: services/relay
      docker:
        dockerfile: cmd/relay-exec/Dockerfile
```

- [ ] **Step 2: Update dev overlay**

In `deploy/overlays/dev/kustomization.yaml`:

1. Add `relay-exec` to the `images:` list (so Skaffold can substitute its digest):

```yaml
images:
  - name: frontend
  - name: backend
  - name: labenv-operator
  - name: attempt-controller
  - name: relay-exec
```

2. Update the operator env patch — replace the entire `value:` list in the patch to:

```yaml
          - name: RELAY_EXEC_IMAGE
            value: "relay-exec"
          - name: RELAY_INGRESS_CLASS
            value: "traefik"
          - name: RELAY_INGRESS_ANNOTATIONS
            value: "traefik.ingress.kubernetes.io/router.entrypoints=websecure"
          - name: RELAY_EXEC_INGRESS_BASE_PATH
            value: "/relay/exec"
```

(Skaffold replaces `relay-exec` with the full image + digest it just built.)

- [ ] **Step 3: Verify relay builds compile cleanly**

```bash
cd services/relay && go build ./cmd/relay-exec/ && echo "OK"
```

Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add skaffold.yaml deploy/overlays/dev/kustomization.yaml
git commit -m "chore: switch build artifact from relay-primitive to relay-exec"
```

---

## Self-Review Against Spec

| Spec requirement | Covered in |
|---|---|
| `SkipAuth bool` on Handler; bypasses header checks; uses `"anonymous"` | Task 1 |
| `RELAY_SKIP_AUTH` env var in relay-exec main; `RELAY_MY_ATTEMPT_ID`/`RELAY_MY_OWNER_ID` optional when skipping | Task 2 |
| relay-exec Dockerfile | Task 2 |
| Operator: deployment name `relay-exec`, labels `app: relay-exec`, env vars `RELAY_MY_ATTEMPT_ID=env.Name`, `RELAY_MY_OWNER_ID=env.Spec.OwnerId`, `RELAY_SKIP_AUTH=true`, `RELAY_MY_NAMESPACE=nsName` | Task 3 |
| Operator: readiness probe `/healthz` | Task 3 |
| Operator: ingress path `/relay/exec/<envName>` (default base path changed) | Task 3 |
| Operator: NetworkPolicy selects `app: relay-exec` | Task 3 |
| Operator: Service selector `app: relay-exec` | Task 3 |
| Operator tests updated | Task 3 |
| SSH composable, test, and TerminalPanel deleted | Task 4 |
| `useExecRelayConnection(attemptId, assetName)` → URL `/relay/exec/<attemptId>/<assetName>/` | Task 5 |
| First message: token only (no secret) | Task 5 |
| Resize framing: `\x01` + cols/rows uint16 LE | Task 5 |
| No healthz check | Task 5 |
| `ExecTerminalPanel.vue` with `assetName` and `attemptId` props | Task 6 |
| `useLabSession` returns `attemptId`; removes `secrets` | Task 6 |
| `LabView` passes `attemptId` to `LabConsole`; no `secrets` | Task 6 |
| `LabConsole` uses `exec: ExecTerminalPanel`; no `secrets` gate | Task 6 |
| Terminal tabs tests: `'ssh'` → `'exec'` | Task 6 |
| Skaffold switches artifact to `relay-exec` | Task 7 |
| Dev overlay `RELAY_EXEC_IMAGE=relay-exec`, `RELAY_EXEC_INGRESS_BASE_PATH=/relay/exec` | Task 7 |
