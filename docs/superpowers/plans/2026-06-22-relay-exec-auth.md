# relay-exec Ingress Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Traefik ForwardAuth to every relay-exec ingress route so the relay-authenticator validates the user's PocketBase session cookie and sets `X-User-Id` before the WebSocket reaches relay-exec.

**Architecture:** The browser sets a `pb_auth` cookie (containing the existing PocketBase token) before opening the WebSocket. Traefik calls the shared `relay-authenticator` in `rootenv-infra` via a `ForwardAuth` Middleware CRD. The authenticator extracts the attempt ID from `X-Forwarded-Uri`, validates the cookie token against PocketBase, verifies attempt ownership, and returns `X-User-Id`. The relay-exec lab namespace never contacts PocketBase.

**Tech Stack:** Go 1.24 (relay-authenticator, labenv-operator), Vue.js (frontend), Kubernetes NetworkPolicy, Traefik Middleware CRD, Kustomize

## Global Constraints

- No PocketBase access from inside the lab namespace
- Middleware name is `kube-system-relay-auth-middleware@kubernetescrd` everywhere — no per-overlay override
- All Go tests use `testing` stdlib (relay-authenticator); operator tests use Ginkgo/Gomega
- Security context on all new pods: `runAsNonRoot`, `readOnlyRootFilesystem`, drop ALL caps
- Module path for relay-authenticator: `github.com/alexsviridov/linuxlab/relay-authenticator`
- Module path for labenv-operator: `github.com/alex-sviridov/rootenv/services/labenv-operator`

---

### Task 1: Update relay-authenticator handler to use cookie + X-Forwarded-Uri

**Files:**
- Modify: `services/relay-authenticator/internal/auth/handler.go`
- Modify: `services/relay-authenticator/internal/auth/handler_test.go`

**Interfaces:**
- Consumes: `PocketBase` interface (`ValidateToken(token string) (string, error)`, `GetAttempt(token, attemptID string) (string, error)`) — unchanged
- Produces: `Handler.ServeHTTP` — same signature; now reads cookie `pb_auth` and header `X-Forwarded-Uri` instead of `Authorization` and `X-Attempt-Id`

- [ ] **Step 1: Replace the existing tests**

Replace the full contents of `services/relay-authenticator/internal/auth/handler_test.go`:

```go
package auth_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/auth"
)

type fakePB struct {
	userID      string
	validateErr error
	attemptErr  error
}

func (f *fakePB) ValidateToken(token string) (string, error) {
	return f.userID, f.validateErr
}

func (f *fakePB) GetAttempt(token, attemptID string) (string, error) {
	if f.attemptErr != nil {
		return "", f.attemptErr
	}
	return f.userID, nil
}

func makeReq(cookie, forwardedURI string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "pb_auth", Value: cookie})
	}
	if forwardedURI != "" {
		req.Header.Set("X-Forwarded-Uri", forwardedURI)
	}
	return req
}

func TestHandler_success(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if w.Header().Get("X-User-Id") != "usr_abc" {
		t.Errorf("want X-User-Id usr_abc, got %q", w.Header().Get("X-User-Id"))
	}
	if w.Header().Get("X-Attempt-Id") != "atm_123" {
		t.Errorf("want X-Attempt-Id atm_123, got %q", w.Header().Get("X-Attempt-Id"))
	}
}

func TestHandler_missing_cookie(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_missing_forwarded_uri(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandler_unparseable_uri(t *testing.T) {
	pb := &fakePB{userID: "usr_abc"}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/not/the/right/pattern/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandler_invalid_token(t *testing.T) {
	pb := &fakePB{validateErr: fmt.Errorf("unauthorized")}
	h := auth.NewHandler(pb)

	req := makeReq("badtok", "/relay/exec/atm_123/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestHandler_forbidden_attempt(t *testing.T) {
	pb := &fakePB{userID: "usr_abc", attemptErr: fmt.Errorf("forbidden")}
	h := auth.NewHandler(pb)

	req := makeReq("tok123", "/relay/exec/atm_other/server-0/")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd services/relay-authenticator && go test ./internal/auth/...
```

Expected: FAIL — tests call the old header-based interface.

- [ ] **Step 3: Rewrite handler.go**

Replace the full contents of `services/relay-authenticator/internal/auth/handler.go`:

```go
package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

// PocketBase is the interface the handler needs.
type PocketBase interface {
	ValidateToken(token string) (string, error)
	GetAttempt(token, attemptID string) (string, error)
}

type Handler struct {
	pb PocketBase
}

func NewHandler(pb PocketBase) *Handler {
	return &Handler{pb: pb}
}

// parseAttemptID extracts the attempt ID from a Traefik X-Forwarded-Uri value.
// Expected pattern: /relay/exec/<attemptId>/...
func parseAttemptID(uri string) (string, bool) {
	parts := strings.FieldsFunc(uri, func(r rune) bool { return r == '/' })
	for i, p := range parts {
		if p == "exec" && i+1 < len(parts) {
			return parts[i+1], true
		}
	}
	return "", false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("pb_auth")
	if err != nil {
		http.Error(w, "missing pb_auth cookie", http.StatusUnauthorized)
		return
	}
	token := cookie.Value

	forwardedURI := r.Header.Get("X-Forwarded-Uri")
	if forwardedURI == "" {
		http.Error(w, "missing X-Forwarded-Uri", http.StatusBadRequest)
		return
	}

	attemptID, ok := parseAttemptID(forwardedURI)
	if !ok {
		http.Error(w, "cannot parse attempt ID from X-Forwarded-Uri", http.StatusBadRequest)
		return
	}

	userID, err := h.pb.ValidateToken(token)
	if err != nil {
		slog.Warn("token validation failed", "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := h.pb.GetAttempt(token, attemptID); err != nil {
		slog.Warn("attempt access denied", "attempt_id", attemptID, "err", err)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("X-User-Id", userID)
	w.Header().Set("X-Attempt-Id", attemptID)
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd services/relay-authenticator && go test ./internal/auth/...
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add services/relay-authenticator/internal/auth/handler.go \
        services/relay-authenticator/internal/auth/handler_test.go
git commit -m "feat(relay-authenticator): read token from cookie, attempt from X-Forwarded-Uri"
```

---

### Task 2: Add /readyz endpoint to relay-authenticator

**Files:**
- Modify: `services/relay-authenticator/cmd/main.go`

**Interfaces:**
- Produces: `GET /readyz` → 200 if PocketBase `/api/health` returns 200, 503 otherwise

- [ ] **Step 1: Replace cmd/main.go**

Replace the full contents of `services/relay-authenticator/cmd/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/auth"
	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/pbclient"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	pbURL := os.Getenv("INGAUTH_POCKETBASE_URL")
	if pbURL == "" {
		slog.Error("INGAUTH_POCKETBASE_URL is required")
		os.Exit(1)
	}
	tlsVerify := os.Getenv("INGAUTH_POCKETBASE_TLS_VERIFY") != "false"

	port := os.Getenv("INGAUTH_PORT")
	if port == "" {
		port = "8080"
	}

	pb := pbclient.New(pbURL, tlsVerify)
	handler := auth.NewHandler(pb)

	readyzClient := &http.Client{Timeout: 3 * time.Second}
	healthURL := strings.TrimRight(pbURL, "/") + "/api/health"

	mux := http.NewServeMux()
	mux.Handle("/auth", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		resp, err := readyzClient.Get(healthURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			http.Error(w, "pocketbase unreachable", http.StatusServiceUnavailable)
			return
		}
		resp.Body.Close()
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("relay-authenticator starting", "port", port, "pb_url", pbURL, "tls_verify", tlsVerify)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
	slog.Info("shutdown complete")
}
```

- [ ] **Step 2: Add missing import**

The file above uses `strings.TrimRight` — add `"strings"` to the import block. The full import block should be:

```go
import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/auth"
	"github.com/alexsviridov/linuxlab/relay-authenticator/internal/pbclient"
)
```

- [ ] **Step 3: Verify it compiles**

```bash
cd services/relay-authenticator && go build ./...
```

Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add services/relay-authenticator/cmd/main.go
git commit -m "feat(relay-authenticator): add /readyz probe that checks PocketBase reachability"
```

---

### Task 3: Deploy relay-authenticator to rootenv-infra

**Files:**
- Create: `deploy/base/61-relay-authenticator.yaml`
- Modify: `deploy/base/kustomization.yaml`

**Interfaces:**
- Produces: Service `relay-authenticator.rootenv-infra.svc.cluster.local:8080` reachable by Traefik in `kube-system`

- [ ] **Step 1: Create the manifest**

Create `deploy/base/61-relay-authenticator.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: relay-authenticator
  namespace: rootenv-infra
  labels:
    app: relay-authenticator
spec:
  replicas: 1
  selector:
    matchLabels:
      app: relay-authenticator
  template:
    metadata:
      labels:
        app: relay-authenticator
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: relay-authenticator
        image: relay-authenticator
        imagePullPolicy: IfNotPresent
        env:
        - name: INGAUTH_POCKETBASE_URL
          value: http://backend-svc.rootenv-infra.svc.cluster.local:8090
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            memory: "32Mi"
            cpu: "50m"
          limits:
            memory: "64Mi"
            cpu: "100m"
---
apiVersion: v1
kind: Service
metadata:
  name: relay-authenticator
  namespace: rootenv-infra
spec:
  selector:
    app: relay-authenticator
  ports:
  - port: 8080
    targetPort: 8080
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: relay-authenticator
  namespace: rootenv-infra
spec:
  podSelector:
    matchLabels:
      app: relay-authenticator
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kube-system
    ports:
    - protocol: TCP
      port: 8080
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: rootenv-infra
      podSelector:
        matchLabels:
          app: backend
    ports:
    - protocol: TCP
      port: 8090
```

- [ ] **Step 2: Add to kustomization**

In `deploy/base/kustomization.yaml`, add `61-relay-authenticator.yaml` after `60-relay-middleware.yaml` in the `resources` list:

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
  - 50-attempt-controller-secrets.yaml
  - 51-attempt-controller-serviceaccount.yaml
  - 53-attempt-controller.yaml
  - 60-relay-middleware.yaml
  - 61-relay-authenticator.yaml
```

- [ ] **Step 3: Verify kustomize renders cleanly**

```bash
kubectl kustomize deploy/base 2>&1 | head -20
```

Expected: YAML output with no errors

- [ ] **Step 4: Commit**

```bash
git add deploy/base/61-relay-authenticator.yaml deploy/base/kustomization.yaml
git commit -m "feat(deploy): add relay-authenticator deployment, service, and network policy"
```

---

### Task 4: Add Traefik ForwardAuth Middleware CRD

**Files:**
- Create: `deploy/base/62-relay-auth-middleware.yaml`
- Modify: `deploy/base/kustomization.yaml`

**Interfaces:**
- Produces: Traefik `Middleware` named `relay-auth-middleware` in `kube-system`, referenced as `kube-system-relay-auth-middleware@kubernetescrd`

- [ ] **Step 1: Create the middleware manifest**

Create `deploy/base/62-relay-auth-middleware.yaml`:

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

- [ ] **Step 2: Add to kustomization**

In `deploy/base/kustomization.yaml`, add `62-relay-auth-middleware.yaml` after `61-relay-authenticator.yaml`:

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
  - 50-attempt-controller-secrets.yaml
  - 51-attempt-controller-serviceaccount.yaml
  - 53-attempt-controller.yaml
  - 60-relay-middleware.yaml
  - 61-relay-authenticator.yaml
  - 62-relay-auth-middleware.yaml
```

- [ ] **Step 3: Verify kustomize renders cleanly**

```bash
kubectl kustomize deploy/base 2>&1 | head -20
```

Expected: YAML output with no errors

- [ ] **Step 4: Commit**

```bash
git add deploy/base/62-relay-auth-middleware.yaml deploy/base/kustomization.yaml
git commit -m "feat(deploy): add Traefik ForwardAuth middleware for relay-exec routes"
```

---

### Task 5: Wire middleware annotation into the operator

**Files:**
- Modify: `services/labenv-operator/internal/controller/relay.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller_test.go`

**Interfaces:**
- `loadRelayConfig()` now always includes `traefik.ingress.kubernetes.io/router.middlewares: kube-system-relay-auth-middleware@kubernetescrd` in `ingressAnnotations`, merged with any annotations from `RELAY_INGRESS_ANNOTATIONS`

- [ ] **Step 1: Update the test to assert the middleware annotation**

In `labenvironment_controller_test.go`, find the `It("sets annotations from config"` block inside `Describe("ensureRelayIngress"` and add an assertion for the middleware key:

```go
It("sets annotations from config", func() {
    cfg := relayConfig{
        ingressBasePath: "/relay/exec",
        ingressAnnotations: map[string]string{
            "traefik.ingress.kubernetes.io/router.entrypoints": "websecure",
            "traefik.ingress.kubernetes.io/router.middlewares": "kube-system-relay-auth-middleware@kubernetescrd",
        },
    }
    r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
    Expect(r.ensureRelayIngress(ctx, env, nsName, cfg)).To(Succeed())

    var ing networkingv1.Ingress
    Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "relay"}, &ing)).To(Succeed())
    Expect(ing.Annotations).To(HaveKeyWithValue(
        "traefik.ingress.kubernetes.io/router.entrypoints", "websecure",
    ))
    Expect(ing.Annotations).To(HaveKeyWithValue(
        "traefik.ingress.kubernetes.io/router.middlewares", "kube-system-relay-auth-middleware@kubernetescrd",
    ))
})
```

Also add a new `It` block in `Describe("loadRelayConfig"` to assert the middleware default:

```go
It("includes the relay auth middleware annotation by default", func() {
    os.Setenv("RELAY_IMAGE", "img:tag")
    cfg, err := loadRelayConfig()
    Expect(err).NotTo(HaveOccurred())
    Expect(cfg.ingressAnnotations).To(HaveKeyWithValue(
        "traefik.ingress.kubernetes.io/router.middlewares",
        "kube-system-relay-auth-middleware@kubernetescrd",
    ))
})
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd services/labenv-operator && go test ./internal/controller/... 2>&1 | tail -20
```

Expected: FAIL on the new assertions.

- [ ] **Step 3: Update loadRelayConfig in relay.go**

In `services/labenv-operator/internal/controller/relay.go`, replace the `loadRelayConfig` function:

```go
func loadRelayConfig() (relayConfig, error) {
	image := os.Getenv("RELAY_IMAGE")
	if image == "" {
		return relayConfig{}, fmt.Errorf("RELAY_IMAGE env var is required")
	}

	basePath := os.Getenv("RELAY_INGRESS_BASE_PATH")
	if basePath == "" {
		basePath = "/relay/exec"
	}

	// start with the hardcoded auth middleware — always required
	annotations := map[string]string{
		"traefik.ingress.kubernetes.io/router.middlewares": "kube-system-relay-auth-middleware@kubernetescrd",
	}
	if raw := os.Getenv("RELAY_INGRESS_ANNOTATIONS"); raw != "" {
		for _, token := range strings.Split(raw, ",") {
			k, v, ok := strings.Cut(token, "=")
			if !ok || strings.TrimSpace(k) == "" {
				continue
			}
			annotations[strings.TrimSpace(k)] = v
		}
	}

	return relayConfig{
		image:              image,
		ingressClass:       os.Getenv("RELAY_INGRESS_CLASS"),
		ingressBasePath:    basePath,
		ingressAnnotations: annotations,
	}, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd services/labenv-operator && go test ./internal/controller/... 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add services/labenv-operator/internal/controller/relay.go \
        services/labenv-operator/internal/controller/labenvironment_controller_test.go
git commit -m "feat(labenv-operator): hardcode relay-auth-middleware annotation on relay ingress"
```

---

### Task 6: Set pb_auth cookie in the frontend before WebSocket connect

**Files:**
- Modify: `services/frontend/src/composables/useExecRelayConnection.js`

**Interfaces:**
- Before every `new WebSocket(url)`, the `pb_auth` cookie is set with the current PocketBase token

- [ ] **Step 1: Update connect() in useExecRelayConnection.js**

In `services/frontend/src/composables/useExecRelayConnection.js`, find the `connect()` function and add the cookie line immediately before `ws = new WebSocket(url)`:

```js
function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/exec/${attemptId}/${assetName}/`
    document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
    ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    // ... rest unchanged
```

- [ ] **Step 2: Verify tests still pass**

```bash
cd services/frontend && npm test -- --run 2>&1 | tail -20
```

Expected: all tests pass (the cookie line has no effect in test environments)

- [ ] **Step 3: Commit**

```bash
git add services/frontend/src/composables/useExecRelayConnection.js
git commit -m "feat(frontend): set pb_auth cookie before WebSocket upgrade for Traefik ForwardAuth"
```

---

### Task 7: Add relay-authenticator image to skaffold and dev overlay

**Files:**
- Modify: `skaffold.yaml`
- Modify: `deploy/overlays/dev/kustomization.yaml`

**Interfaces:**
- Skaffold builds and pushes `relay-authenticator` image from `services/relay-authenticator/`
- Dev overlay resolves the image tag

- [ ] **Step 1: Add relay-authenticator to skaffold.yaml**

In `skaffold.yaml`, add after the `relay-exec` artifact in the `build.artifacts` list:

```yaml
    - image: relay-authenticator
      context: services/relay-authenticator
      docker:
        dockerfile: Dockerfile
```

- [ ] **Step 3: Add image to dev overlay kustomization**

In `deploy/overlays/dev/kustomization.yaml`, add to the `images` list:

```yaml
- name: relay-authenticator
```

- [ ] **Step 4: Verify kustomize renders cleanly**

```bash
kubectl kustomize deploy/overlays/dev 2>&1 | grep -A3 "relay-authenticator"
```

Expected: the Deployment appears with `image: relay-authenticator`

- [ ] **Step 5: Commit**

```bash
git add skaffold.yaml deploy/overlays/dev/kustomization.yaml
git commit -m "feat(deploy): add relay-authenticator to skaffold and dev overlay"
```
