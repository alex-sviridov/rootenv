# attempt-controller upstream (K8s → PocketBase) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an upstream reconciler goroutine to `attempt-controller` that watches `LabEnvironment` K8s status changes and writes `attempts.current_state`, `attempts.expires_at`, and `attempts.assets` back to PocketBase.

**Architecture:** A new `internal/upstream` package contains a `Reconciler` that uses `client-go/tools/cache.ListWatch` + `NewInformer` to watch `LabEnvironment` CRs. On startup it reconciles all existing resources; on each Add/Update/Delete event it PATCHes the linked PocketBase `attempts` record. It runs as a goroutine alongside the existing downstream goroutine. The `pocketbase.Client` gains a `PatchAttempt` method with the same 401-retry pattern as `get`.

**Tech Stack:** Go 1.24, `k8s.io/client-go v0.32.5` (`tools/cache`, `dynamic`), `k8s.io/apimachinery`, standard `net/http` for PocketBase PATCH, `net/http/httptest` for tests.

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/pocketbase/pbclient.go` | Modify | Add `PatchAttempt` method |
| `internal/pocketbase/pbclient_test.go` | Modify | Test `PatchAttempt` (success, 401-retry) |
| `internal/upstream/reconcile.go` | Create | `Reconciler` struct, phase/asset mapping, `ReconcileLabEnv`, `ReconcileDelete` |
| `internal/upstream/reconcile_test.go` | Create | Unit tests for all mapping logic and PATCH calls |
| `internal/upstream/watcher.go` | Create | `Run()` — `ListWatch` + `NewInformer` wiring |
| `cmd/main.go` | Modify | Launch upstream goroutine |

---

### Task 1: Add `PatchAttempt` to the PocketBase client

**Files:**
- Modify: `internal/pocketbase/pbclient.go`
- Modify: `internal/pocketbase/pbclient_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/pocketbase/pbclient_test.go`:

```go
func TestPatchAttemptSuccess(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok1"})
	})
	mux.HandleFunc("/api/collections/attempts/records/a1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "a1"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := c.PatchAttempt(context.Background(), "a1", map[string]any{"current_state": "provisioned"}); err != nil {
		t.Fatalf("PatchAttempt: %v", err)
	}
	if gotBody["current_state"] != "provisioned" {
		t.Errorf("body current_state = %v", gotBody["current_state"])
	}
}

func TestPatchAttemptReauthsOn401(t *testing.T) {
	var authCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"token": fmt.Sprintf("tok%d", authCalls)})
	})
	mux.HandleFunc("/api/collections/attempts/records/a1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "tok2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "a1"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := c.PatchAttempt(context.Background(), "a1", map[string]any{"current_state": "provisioned"}); err != nil {
		t.Fatalf("PatchAttempt: %v", err)
	}
	if authCalls != 2 {
		t.Errorf("authCalls = %d, want 2", authCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd services/attempt-controller && go test ./internal/pocketbase/... -run TestPatchAttempt -v
```

Expected: `FAIL` — `c.PatchAttempt undefined`

- [ ] **Step 3: Implement `PatchAttempt`**

Add to `internal/pocketbase/pbclient.go` (after `GetAttempt`):

```go
// PatchAttempt sends a PATCH request to update fields on the given attempt record.
// Only the keys present in patch are sent; omit a key to leave that field unchanged.
func (c *Client) PatchAttempt(ctx context.Context, id string, patch map[string]any) error {
	resp, err := c.doPatch(ctx, "/api/collections/attempts/records/"+id, patch)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if err := c.reauth(); err != nil {
			return fmt.Errorf("PATCH attempt %s: reauth: %w", id, err)
		}
		resp, err = c.doPatch(ctx, "/api/collections/attempts/records/"+id, patch)
		if err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PATCH attempt %s: status %d", id, resp.StatusCode)
	}
	return nil
}

func (c *Client) doPatch(ctx context.Context, path string, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.currentToken())
	return c.httpClient.Do(req)
}
```

Add `"bytes"` to the import block (it's already present via `doSubscribeRealtime` in `realtime.go`, but `pbclient.go` needs it explicitly if not already imported). Check the import block — if `bytes` is missing, add it.

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd services/attempt-controller && go test ./internal/pocketbase/... -v
```

Expected: all `PASS`

- [ ] **Step 5: Commit**

```bash
cd services/attempt-controller
git add internal/pocketbase/pbclient.go internal/pocketbase/pbclient_test.go
git commit -m "feat(attempt-controller): add PatchAttempt to PocketBase client"
```

---

### Task 2: Create `internal/upstream/reconcile.go` with phase/asset mapping

**Files:**
- Create: `internal/upstream/reconcile.go`
- Create: `internal/upstream/reconcile_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/upstream/reconcile_test.go`:

```go
package upstream

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// stubWriter records calls to PatchAttempt.
type stubWriter struct {
	id    string
	patch map[string]any
}

func (s *stubWriter) PatchAttempt(_ context.Context, id string, patch map[string]any) error {
	s.id = id
	s.patch = patch
	return nil
}

func mustUnstructured(obj map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: obj}
}

func TestPhaseMapping(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{"Pending", "provisioning"},
		{"Degraded", "provisioning"},
		{"Ready", "provisioned"},
		{"Terminating", "decommissioning"},
	}
	for _, tc := range cases {
		if got := phaseToState(tc.phase); got != tc.want {
			t.Errorf("phaseToState(%q) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestAssetPhaseMapping(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{"Running", "provisioned"},
		{"Succeeded", "provisioned"},
		{"Pending", "provisioning"},
		{"Terminating", "decommissioning"},
		{"Unknown", "pending"},
		{"", "pending"},
	}
	for _, tc := range cases {
		if got := assetPhaseToState(tc.phase); got != tc.want {
			t.Errorf("assetPhaseToState(%q) = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestReconcileLabEnvReady(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	expiresAt := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	obj := mustUnstructured(map[string]any{
		"apiVersion": "lab.rootenv.io/v1alpha1",
		"kind":       "LabEnvironment",
		"metadata": map[string]any{
			"name":            "abc123",
			"resourceVersion": "42",
		},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": expiresAt.Format(time.RFC3339),
			"assets": []any{
				map[string]any{
					"name":      "workstation",
					"phase":     "Running",
					"ready":     true,
					"protocols": []any{"ssh"},
				},
			},
		},
	})

	r.ReconcileLabEnv(context.Background(), obj)

	if w.id != "abc123" {
		t.Fatalf("id = %q, want abc123", w.id)
	}
	if w.patch["current_state"] != "provisioned" {
		t.Errorf("current_state = %v", w.patch["current_state"])
	}
	// expires_at should be present on first reconcile
	if _, ok := w.patch["expires_at"]; !ok {
		t.Error("expires_at missing from first patch")
	}
	// assets should be serialised JSON
	assetsJSON, ok := w.patch["assets"]
	if !ok {
		t.Fatal("assets missing from patch")
	}
	var assets []map[string]any
	b, _ := json.Marshal(assetsJSON)
	_ = json.Unmarshal(b, &assets)
	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d", len(assets))
	}
	if assets[0]["name"] != "workstation" {
		t.Errorf("assets[0].name = %v", assets[0]["name"])
	}
	if assets[0]["state"] != "provisioned" {
		t.Errorf("assets[0].state = %v", assets[0]["state"])
	}
	if assets[0]["status"] != "poweredon" {
		t.Errorf("assets[0].status = %v", assets[0]["status"])
	}
	protos, _ := assets[0]["protocols"].([]any)
	if len(protos) != 1 || protos[0] != "ssh" {
		t.Errorf("assets[0].protocols = %v", assets[0]["protocols"])
	}
}

func TestReconcileLabEnvSkipsDuplicateResourceVersion(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{
			"name":            "abc123",
			"resourceVersion": "42",
		},
		"status": map[string]any{"phase": "Ready"},
	})

	r.ReconcileLabEnv(context.Background(), obj)
	w.id = ""   // reset
	w.patch = nil

	r.ReconcileLabEnv(context.Background(), obj) // same resourceVersion
	if w.id != "" {
		t.Error("expected second call to be skipped, but PatchAttempt was called")
	}
}

func TestReconcileLabEnvSkipsExpiresAtAfterFirstWrite(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": "2026-06-16T12:00:00Z",
			"assets":    []any{},
		},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if _, ok := w.patch["expires_at"]; !ok {
		t.Fatal("expires_at missing on first write")
	}

	// second reconcile with new resourceVersion
	obj.SetResourceVersion("2")
	w.patch = nil
	r.ReconcileLabEnv(context.Background(), obj)
	if _, ok := w.patch["expires_at"]; ok {
		t.Error("expires_at should not be written a second time")
	}
}

func TestReconcileDeleteSetsDecommissioned(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "5"},
	})

	r.ReconcileDelete(context.Background(), obj)

	if w.id != "abc123" {
		t.Fatalf("id = %q, want abc123", w.id)
	}
	if w.patch["current_state"] != "decommissioned" {
		t.Errorf("current_state = %v", w.patch["current_state"])
	}
}

func TestReconcileLabEnvEmptyPhaseSkips(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status":   map[string]any{"phase": ""},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if w.id != "" {
		t.Error("expected empty phase to skip PATCH, but PatchAttempt was called")
	}
}

func TestReconcileLabEnvSetsExpiresAtFromMetav1Time(t *testing.T) {
	w := &stubWriter{}
	r := NewReconciler(w)

	ts := metav1.NewTime(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC))
	obj := mustUnstructured(map[string]any{
		"metadata": map[string]any{"name": "abc123", "resourceVersion": "1"},
		"status": map[string]any{
			"phase":     "Ready",
			"expiresAt": ts.UTC().Format(time.RFC3339),
			"assets":    []any{},
		},
	})
	r.ReconcileLabEnv(context.Background(), obj)
	if w.patch["expires_at"] != "2026-06-16 12:00:00.000Z" {
		t.Errorf("expires_at = %v", w.patch["expires_at"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd services/attempt-controller && go test ./internal/upstream/... -v
```

Expected: `FAIL` — package `upstream` not found

- [ ] **Step 3: Implement `internal/upstream/reconcile.go`**

Create `internal/upstream/reconcile.go`:

```go
package upstream

import (
	"context"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PocketBaseWriter is the subset of pocketbase.Client the upstream needs.
type PocketBaseWriter interface {
	PatchAttempt(ctx context.Context, id string, patch map[string]any) error
}

// Reconciler syncs LabEnvironment status back to PocketBase attempts.
// It is not safe for concurrent use — Run() must be the only caller.
type Reconciler struct {
	pb              PocketBaseWriter
	lastRV          map[string]string // attemptID → last synced resourceVersion
	expiresAtWritten map[string]bool  // attemptIDs for which expires_at has been written
}

func NewReconciler(pb PocketBaseWriter) *Reconciler {
	return &Reconciler{
		pb:               pb,
		lastRV:           make(map[string]string),
		expiresAtWritten: make(map[string]bool),
	}
}

// ReconcileLabEnv handles an Add or Update event for a LabEnvironment.
func (r *Reconciler) ReconcileLabEnv(ctx context.Context, obj *unstructured.Unstructured) {
	id := obj.GetName()
	rv := obj.GetResourceVersion()

	if r.lastRV[id] == rv {
		return
	}

	status, _, _ := unstructured.NestedMap(obj.Object, "status")
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase == "" {
		return // status not yet set by operator
	}

	patch := map[string]any{
		"current_state": phaseToState(phase),
		"assets":        r.buildAssets(status),
	}

	if !r.expiresAtWritten[id] {
		expiresAt, _, _ := unstructured.NestedString(obj.Object, "status", "expiresAt")
		if expiresAt != "" {
			t, err := time.Parse(time.RFC3339, expiresAt)
			if err == nil {
				// PocketBase date format: "2006-01-02 15:04:05.000Z"
				patch["expires_at"] = t.UTC().Format("2006-01-02 15:04:05.000Z")
			}
		}
	}

	if err := r.pb.PatchAttempt(ctx, id, patch); err != nil {
		log.Printf("upstream: attempt %s: patch failed: %v", id, err)
		return
	}

	r.lastRV[id] = rv
	if _, ok := patch["expires_at"]; ok {
		r.expiresAtWritten[id] = true
	}
	log.Printf("upstream: attempt %s: synced phase=%s", id, phase)
}

// ReconcileDelete handles a Delete event — sets current_state to decommissioned.
func (r *Reconciler) ReconcileDelete(ctx context.Context, obj *unstructured.Unstructured) {
	id := obj.GetName()
	patch := map[string]any{"current_state": "decommissioned"}
	if err := r.pb.PatchAttempt(ctx, id, patch); err != nil {
		log.Printf("upstream: attempt %s: delete patch failed: %v", id, err)
		return
	}
	delete(r.lastRV, id)
	delete(r.expiresAtWritten, id)
	log.Printf("upstream: attempt %s: marked decommissioned", id)
}

// phaseToState maps LabEnvironment.Status.Phase to attempts.current_state.
func phaseToState(phase string) string {
	switch phase {
	case "Ready":
		return "provisioned"
	case "Terminating":
		return "decommissioning"
	default: // Pending, Degraded, or anything unexpected
		return "provisioning"
	}
}

// assetPhaseToState maps AssetStatus.Phase to the assets.state select value.
func assetPhaseToState(phase string) string {
	switch phase {
	case "Running", "Succeeded":
		return "provisioned"
	case "Pending":
		return "provisioning"
	case "Terminating":
		return "decommissioning"
	default:
		return "pending"
	}
}

// buildAssets converts status.assets ([]AssetStatus) into the JSON value for
// attempts.assets. Each element has name, state, status, and protocols.
func (r *Reconciler) buildAssets(status map[string]any) []map[string]any {
	raw, _ := status["assets"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		a, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := a["name"].(string)
		phase, _ := a["phase"].(string)

		protocols := []string{}
		if rawProtos, ok := a["protocols"].([]any); ok {
			for _, p := range rawProtos {
				if s, ok := p.(string); ok {
					protocols = append(protocols, s)
				}
			}
		}

		result = append(result, map[string]any{
			"name":      name,
			"state":     assetPhaseToState(phase),
			"status":    "poweredon",
			"protocols": protocols,
		})
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd services/attempt-controller && go test ./internal/upstream/... -v
```

Expected: all `PASS`

- [ ] **Step 5: Commit**

```bash
cd services/attempt-controller
git add internal/upstream/reconcile.go internal/upstream/reconcile_test.go
git commit -m "feat(attempt-controller): add upstream Reconciler with phase/asset mapping"
```

---

### Task 3: Create `internal/upstream/watcher.go`

**Files:**
- Create: `internal/upstream/watcher.go`

No unit test for this file — the `ListWatch` and `NewInformer` are thin wiring around tested client-go types. Integration is covered by the existing system (skaffold dev loop).

- [ ] **Step 1: Create `internal/upstream/watcher.go`**

```go
package upstream

import (
	"context"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
)

const resyncPeriod = 5 * time.Minute

// Run lists all existing LabEnvironment CRs, reconciles each, then watches for
// changes and reconciles on every Add/Update/Delete event until ctx is cancelled.
// It is intended to run as a goroutine.
func (r *Reconciler) Run(ctx context.Context, dyn dynamic.Interface) {
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return dyn.Resource(k8s.LabEnvironmentGVR).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			return dyn.Resource(k8s.LabEnvironmentGVR).Watch(ctx, opts)
		},
	}

	_, informer := cache.NewInformer(
		lw,
		&unstructured.Unstructured{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				r.ReconcileLabEnv(ctx, u)
			},
			UpdateFunc: func(_, newObj any) {
				u, ok := newObj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				r.ReconcileLabEnv(ctx, u)
			},
			DeleteFunc: func(obj any) {
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					// tombstone — extract the object
					if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
						u, ok = d.Obj.(*unstructured.Unstructured)
						if !ok {
							return
						}
					} else {
						return
					}
				}
				r.ReconcileDelete(ctx, u)
			},
		},
	)

	log.Println("upstream: starting LabEnvironment watcher")
	informer.Run(ctx.Done())
	log.Println("upstream: LabEnvironment watcher stopped")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd services/attempt-controller && go build ./...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
cd services/attempt-controller
git add internal/upstream/watcher.go
git commit -m "feat(attempt-controller): add upstream watcher using client-go ListWatch informer"
```

---

### Task 4: Wire upstream into `cmd/main.go`

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Add upstream goroutine to `main.go`**

Open `cmd/main.go`. Add the import and the goroutine launch. The full updated file:

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/config"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/downstream"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/pocketbase"
	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/upstream"
)

const (
	subscriptionReconnectBackoff = 5 * time.Second
	fullResyncInterval           = 5 * time.Minute
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config error:", err)
	}

	pb, err := pocketbase.NewClient(cfg.PbURL, cfg.PbEmail, cfg.PbPassword, cfg.TlsVerify)
	if err != nil {
		log.Fatal("PocketBase auth failed:", err)
	}
	log.Printf("connected to PocketBase at %s", cfg.PbURL)

	dyn, err := k8s.NewClient()
	if err != nil {
		log.Fatal("k8s client failed:", err)
	}

	rec := downstream.NewReconciler(dyn)

	var firstConnect atomic.Bool
	firstConnect.Store(true)

	go pb.RunAttemptSubscription(ctx, func(action string, pbRec pocketbase.AttemptRecord) {
		if pbRec.DesiredState != downstream.DesiredStateDecommissioned {
			full, err := pb.GetAttempt(ctx, pbRec.ID)
			if errors.Is(err, pocketbase.ErrNotFound) {
				log.Printf("attempt %s: not found in PocketBase, removing LabEnvironment", pbRec.ID)
				rec.ReconcileAttempt(ctx, downstream.Attempt{
					ID:                 pbRec.ID,
					DesiredState:       downstream.DesiredStateDecommissioned,
					DecommissionReason: "attempt-not-found-in-pocketbase",
				})
				return
			}
			if err != nil {
				log.Printf("attempt %s: failed to fetch attempt: %v", pbRec.ID, err)
				return
			}
			pbRec = full
		}

		a, err := pbRec.ToAttempt()
		if err != nil {
			log.Printf("attempt %s: %v", pbRec.ID, err)
			return
		}
		if a.DesiredState == downstream.DesiredStateDecommissioned && a.DecommissionReason == "" {
			a.DecommissionReason = "desired-state-decommissioned"
		}
		rec.ReconcileAttempt(ctx, a)
	}, func(ctx context.Context) {
		if firstConnect.Swap(false) {
			return
		}
		rec.ResyncAttempts(ctx, pb)
	}, subscriptionReconnectBackoff)

	upRec := upstream.NewReconciler(pb)
	go upRec.Run(ctx, dyn)

	ticker := time.NewTicker(fullResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rec.ResyncAttempts(ctx, pb)
		}
	}
}
```

- [ ] **Step 2: Build and verify no errors**

```bash
cd services/attempt-controller && go build ./...
```

Expected: no errors

- [ ] **Step 3: Run the full test suite**

```bash
cd services/attempt-controller && go test ./... -v
```

Expected: all `PASS`

- [ ] **Step 4: Commit**

```bash
cd services/attempt-controller
git add cmd/main.go
git commit -m "feat(attempt-controller): wire upstream reconciler goroutine in main"
```

---

### Task 5: Update backend memory and schema docs

**Files:**
- Modify: `.claude/memory/memory-backend.md`

- [ ] **Step 1: Update the `attempts` table entry in `.claude/memory/memory-backend.md`**

Add the two missing fields to the `attempts` table:

```markdown
| expires_at | date | set by upstream reconciler when LabEnvironment.Status.ExpiresAt first appears; written once |
| assets | json | array of `{name, state, status, protocols}` written by upstream reconciler from LabEnvironment.Status.Assets |
```

Also update the Hooks section to add a note:

```markdown
- `attempts` upstream sync — `attempt-controller` upstream reconciler watches `LabEnvironment` status and PATCHes `current_state`, `expires_at` (once), and `assets` on the attempt record; the service account `svc_role=attempt-controller` bypasses the hook's field-protection guard
```

- [ ] **Step 2: Commit**

```bash
git add .claude/memory/memory-backend.md
git commit -m "docs: update memory-backend with attempts.expires_at and assets fields"
```
