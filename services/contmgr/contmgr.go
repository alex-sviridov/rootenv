package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	maxConcurrentOps = 10
	k8sOpTimeout     = 30 * time.Second
)

// pbDoer is the PocketBase operations contmgr needs.
type pbDoer interface {
	ListPendingAssets() ([]Asset, error)
	ListProvisioningAssets() ([]Asset, error)
	GetAsset(id string) (*Asset, error)
	GetAssetByNameAndAttempt(name, attemptID string) (*Asset, error)
	GetAssetConfig(assetID string) (*AssetConfig, error)
	GetKeysByAsset(assetID string) (*KeysRecord, error)
	PatchAsset(id string, fields map[string]any) error
	PatchAssetStatus(id, status string) error
	PatchAssetConfig(id string, fields map[string]any) error
	PatchKeys(id string, fields map[string]any) error
	ListAttemptsToDecommission() ([]AttemptRecord, error)
	ListActiveAssetsByAttempt(attemptID string) ([]Asset, error)
	ListDecommissioningAssets() ([]Asset, error)
	ListProvisionedAssetsByAttempt(attemptID string) ([]Asset, error)
	GetAttempt(attemptID string) (*AttemptRecord, error)
}

type Contmgr struct {
	pb             pbDoer
	k8s            k8sDoer
	infraNamespace string
	pullSecret     string
	imageRegistry  string
	runtimeClass   string
	needsReconn    atomic.Bool
}

func NewContmgr(pb *pbClient, k8s *K8sClient, infraNamespace, pullSecret, imageRegistry, runtimeClass string) *Contmgr {
	return &Contmgr{pb: pb, k8s: k8s, infraNamespace: infraNamespace, pullSecret: pullSecret, imageRegistry: imageRegistry, runtimeClass: runtimeClass}
}

// resolveImage prepends registry to single-component image names (no '/').
// Images with a '/' are assumed to already encode their source
// ("hermsi/alpine-sshd" → Docker Hub, "ghcr.io/foo/bar" → explicit registry)
// and are left untouched. No-op when registry is empty.
func resolveImage(image, registry string) string {
	if registry == "" || strings.Contains(image, "/") {
		return image
	}
	return registry + "/" + image
}

func (p *Contmgr) NeedsReconnect() bool { return p.needsReconn.Swap(false) }
func (p *Contmgr) SetPB(pb *pbClient)   { p.pb = pb }

// UpdateAssetStatusFromPod patches the PocketBase asset status based on a pod phase change.
// Called by PodStatusController on every pod event. phase is the pod phase string;
// empty string means the pod was deleted (maps to "stopped").
func (p *Contmgr) UpdateAssetStatusFromPod(ctx context.Context, assetName, attemptID, phase string) error {
	status := podPhaseToStatus(phase)
	asset, err := p.pb.GetAssetByNameAndAttempt(assetName, attemptID)
	if err != nil {
		slog.Debug("UpdateAssetStatusFromPod: asset not found", "asset", assetName, "attempt", attemptID, "err", err)
		return nil
	}
	if err := p.pb.PatchAssetStatus(asset.ID, status); err != nil {
		return fmt.Errorf("patch asset status: %w", err)
	}
	slog.Info("asset status updated", "asset", asset.ID, "status", status)
	return nil
}

func podPhaseToStatus(phase string) string {
	switch phase {
	case string(corev1.PodRunning):
		return "running"
	case string(corev1.PodPending):
		return "booting"
	default:
		return "stopped"
	}
}

// withK8s returns a child context with k8sOpTimeout applied.
func withK8s(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, k8sOpTimeout)
}

// cleanupAssetK8s deletes pod and service for an asset by their deterministic names.
// All deletes are best-effort — not-found is not an error.
func (p *Contmgr) cleanupAssetK8s(ctx context.Context, attemptID, assetName string) {
	ns := namespaceName(attemptID)
	pod := podName(assetName)
	svc := svcName(assetName)

	podCtx, podCancel := withK8s(ctx)
	defer podCancel()
	if err := p.k8s.DeletePod(podCtx, ns, pod); err != nil {
		slog.Warn("cleanup: delete pod", "pod", pod, "err", err)
	}

	svcCtx, svcCancel := withK8s(ctx)
	defer svcCancel()
	if err := p.k8s.DeleteService(svcCtx, ns, svc); err != nil {
		slog.Warn("cleanup: delete svc", "svc", svc, "err", err)
	}
}

func (p *Contmgr) ProvisionAsset(ctx context.Context, asset Asset) error {
	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "provisioning", "status": "booting"}); err != nil {
		return fmt.Errorf("mark provisioning: %w", err)
	}

	cfg, err := p.pb.GetAssetConfig(asset.ID)
	if err != nil {
		return fmt.Errorf("get asset config: %w", err)
	}
	def, err := cfg.Def()
	if err != nil {
		return fmt.Errorf("parse asset def: %w", err)
	}
	if err := def.validate(); err != nil {
		return fmt.Errorf("invalid asset def: %w", err)
	}

	attempt, err := p.pb.GetAttempt(asset.Attempt)
	if err != nil {
		return fmt.Errorf("get attempt: %w", err)
	}
	userID := attempt.User
	ns := namespaceName(asset.Attempt)

	userEmail := ""
	if attempt.Expand.User != nil {
		userEmail = attempt.Expand.User.Email
	}

	nsParams := NamespaceParams{
		Name:      ns,
		AttemptID: asset.Attempt,
		UserID:    userID,
		LabID:     attempt.Lab,
		UserEmail: userEmail,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ExpiresAt: attempt.ExpiresAt,
	}
	params := PodParams{
		Namespace:       ns,
		UserID:          userID,
		AttemptID:       asset.Attempt,
		AssetName:       asset.Name,
		Image:   resolveImage(def.Image, p.imageRegistry),
		SSHUser: def.SSHUser,
		CPU:             def.CPU,
		Memory:          def.Memory,
		Disk:            def.Disk,
		ImagePullSecret: p.pullSecret,
		RuntimeClass:    p.runtimeClass,
	}

	provisionErr := p.doProvision(ctx, asset, cfg, def, nsParams, params, ns)
	if provisionErr != nil {
		// Use a fresh context for cleanup so a shutdown signal doesn't prevent
		// removing partial k8s resources. PatchAsset uses the HTTP client's own
		// timeout and is unaffected by context cancellation.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), k8sOpTimeout)
		defer cleanupCancel()
		p.cleanupAssetK8s(cleanupCtx, asset.Attempt, asset.Name)
		if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "pending", "status": "stopped"}); err != nil {
			slog.Warn("reset asset to pending after provision failure", "asset", asset.ID, "err", err)
		}
		return provisionErr
	}
	return nil
}

func (p *Contmgr) doProvision(ctx context.Context, asset Asset, cfg *AssetConfig, def *AssetDef, nsParams NamespaceParams, params PodParams, ns string) error {
	nsCtx, nsCancel := withK8s(ctx)
	defer nsCancel()
	if err := p.k8s.EnsureNamespace(nsCtx, nsParams); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	rbCtx, rbCancel := withK8s(ctx)
	defer rbCancel()
	if err := p.k8s.EnsureRoleBinding(rbCtx, ns); err != nil {
		return fmt.Errorf("ensure role binding: %w", err)
	}

	netpolCtx, netpolCancel := withK8s(ctx)
	defer netpolCancel()
	if err := p.k8s.EnsureNetworkPolicy(netpolCtx, NetPolParams{
		Namespace:      ns,
		InfraNamespace: p.infraNamespace,
	}); err != nil {
		return fmt.Errorf("ensure network policy: %w", err)
	}

	podCtx, podCancel := withK8s(ctx)
	defer podCancel()
	if err := p.k8s.CreatePod(podCtx, params); err != nil {
		return fmt.Errorf("create pod: %w", err)
	}

	svcCtx, svcCancel := withK8s(ctx)
	defer svcCancel()
	if err := p.k8s.CreateService(svcCtx, params); err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	// Headless service named after the asset lets pods within the namespace
	// resolve each other by short name: "ping server-1" works because kube-dns
	// expands it to server-1.{namespace}.svc.cluster.local via search path.
	hlCtx, hlCancel := withK8s(ctx)
	defer hlCancel()
	if err := p.k8s.EnsureHeadlessService(hlCtx, ns, asset.Name); err != nil {
		return fmt.Errorf("ensure headless service: %w", err)
	}

	pName := podName(asset.Name)
	if err := p.k8s.WaitPodRunning(ctx, ns, pName); err != nil {
		return fmt.Errorf("wait pod running: %w", err)
	}

	if def.SSHUser != "" {
		privKeyPEM, pubKeyLine, err := GenerateKeypair()
		if err != nil {
			return fmt.Errorf("generate keypair: %w", err)
		}

		pubKeyStr := strings.TrimSpace(string(pubKeyLine))
		script := buildSSHSetupScript(def.SSHUser, pubKeyStr)
		execCtx, execCancel := withK8s(ctx)
		defer execCancel()
		if err := p.k8s.ExecInPod(execCtx, ns, pName, []string{"sh", "-c", script}); err != nil {
			return fmt.Errorf("setup ssh user: %w", err)
		}

		keysRecord, err := p.pb.GetKeysByAsset(asset.ID)
		if err != nil {
			return fmt.Errorf("get keys: %w", err)
		}
		if keysRecord.Secret == "" {
			return fmt.Errorf("keys record has empty secret for asset %s", asset.ID)
		}
		slog.Debug("encrypting key", "asset", asset.ID, "secret_len", len(keysRecord.Secret))

		ciphertext, err := EncryptPrivateKey(privKeyPEM, keysRecord.Secret)
		if err != nil {
			return fmt.Errorf("encrypt key: %w", err)
		}

		if err := p.pb.PatchKeys(keysRecord.ID, map[string]any{
			"key_encrypted": ciphertext,
		}); err != nil {
			return fmt.Errorf("patch keys: %w", err)
		}
	}

	if def.Setup != "" {
		setupCtx, setupCancel := withK8s(ctx)
		defer setupCancel()
		if err := p.k8s.ExecInPod(setupCtx, ns, pName, []string{"sh", "-c", def.Setup}); err != nil {
			return fmt.Errorf("run setup script: %w", err)
		}
	}

	host := svcDNS(svcName(asset.Name), ns)

	if err := p.pb.PatchAssetConfig(cfg.ID, map[string]any{
		"connection": map[string]any{
			"host": host,
			"port": 22,
			"user": def.SSHUser,
		},
		"configuration": map[string]any{
			"platform":  "container",
			"namespace": ns,
			"pod":       pName,
			"svc":       svcName(asset.Name),
		},
	}); err != nil {
		return fmt.Errorf("patch asset config provisioned: %w", err)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "provisioned", "status": "running"}); err != nil {
		return fmt.Errorf("patch asset state provisioned: %w", err)
	}

	slog.Info("provisioned", "asset", asset.ID, "pod", pName, "namespace", ns)
	return nil
}

// buildSSHSetupScript returns a POSIX sh one-liner that creates the SSH user
// (if missing) and writes the authorized key. It works on both Alpine/busybox
// (addgroup/adduser) and glibc-based images (groupadd/useradd), choosing the
// right toolchain at runtime. The password field is set to '*' so PAM account
// checks pass without enabling password login.
func buildSSHSetupScript(user, pubKey string) string {
	return fmt.Sprintf(
		"if command -v useradd >/dev/null 2>&1; then "+
			"getent group %[1]s || groupadd %[1]s; "+
			"id %[1]s || useradd -m -s /bin/bash -g %[1]s %[1]s; "+
			"usermod -p '*' %[1]s; "+
			"else "+
			"getent group %[1]s || addgroup %[1]s; "+
			"id %[1]s || adduser -D -s /bin/bash -G %[1]s -h /home/%[1]s %[1]s; "+
			"printf '%%s:*' %[1]q | chpasswd -e; "+
			"fi && "+
			"mkdir -p /home/%[1]s/.ssh && "+
			"chown %[1]s:%[1]s /home/%[1]s/.ssh && "+
			"chmod 700 /home/%[1]s/.ssh && "+
			"printf '%%s' %[2]q > /home/%[1]s/.ssh/authorized_keys && "+
			"chown %[1]s:%[1]s /home/%[1]s/.ssh/authorized_keys && "+
			"chmod 600 /home/%[1]s/.ssh/authorized_keys",
		user, pubKey,
	)
}

func (p *Contmgr) DecommissionAsset(ctx context.Context, asset Asset) error {
	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioning", "status": "stopped"}); err != nil {
		return fmt.Errorf("mark decommissioning: %w", err)
	}

	ns := namespaceName(asset.Attempt)

	podCtx, podCancel := withK8s(ctx)
	defer podCancel()
	if err := p.k8s.DeletePod(podCtx, ns, podName(asset.Name)); err != nil {
		return fmt.Errorf("delete pod: %w", err)
	}

	svcCtx, svcCancel := withK8s(ctx)
	defer svcCancel()
	if err := p.k8s.DeleteService(svcCtx, ns, svcName(asset.Name)); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	remaining, err := p.pb.ListProvisionedAssetsByAttempt(asset.Attempt)
	if err != nil {
		slog.Warn("could not check remaining assets for attempt; skipping namespace cleanup", "attempt", asset.Attempt, "err", err)
	} else if len(remaining) == 0 {
		nsCtx, nsCancel := withK8s(ctx)
		defer nsCancel()
		if err := p.k8s.DeleteNamespace(nsCtx, ns); err != nil {
			return fmt.Errorf("delete namespace: %w", err)
		}
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioned", "status": "stopped"}); err != nil {
		return fmt.Errorf("patch asset state: %w", err)
	}

	slog.Info("decommissioned", "asset", asset.ID, "namespace", ns)
	return nil
}

func (p *Contmgr) RunOnce(ctx context.Context) error {
	sem := semaphore.NewWeighted(maxConcurrentOps)
	eg, egCtx := errgroup.WithContext(ctx)

	// Build decommission set first (synchronous) so provision can skip those attempts.
	attemptsToDecommission, err := p.pb.ListAttemptsToDecommission()
	if err != nil {
		slog.Error("list attempts to decommission", "err", err)
		p.needsReconn.Store(true)
	}
	decommissionAttempts := make(map[string]bool, len(attemptsToDecommission))
	for _, a := range attemptsToDecommission {
		decommissionAttempts[a.ID] = true
	}

	// Provision pending assets, skipping any whose attempt is being decommissioned.
	assets, err := p.pb.ListPendingAssets()
	if err != nil {
		slog.Error("list pending assets", "err", err)
		p.needsReconn.Store(true)
	}
	for _, asset := range assets {
		asset := asset
		if decommissionAttempts[asset.Attempt] {
			continue
		}
		if err := sem.Acquire(egCtx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			if err := p.ProvisionAsset(egCtx, asset); err != nil {
				slog.Error("provision asset", "asset", asset.ID, "err", err)
			}
			return nil
		})
	}

	// Decommission all active assets for attempts where desired_state=decommissioned.
	for _, attempt := range attemptsToDecommission {
		attempt := attempt
		activeAssets, err := p.pb.ListActiveAssetsByAttempt(attempt.ID)
		if err != nil {
			slog.Error("list active assets for attempt", "attempt", attempt.ID, "err", err)
			continue
		}
		for _, asset := range activeAssets {
			asset := asset
			if err := sem.Acquire(egCtx, 1); err != nil {
				break
			}
			eg.Go(func() error {
				defer sem.Release(1)
				if asset.State == "decommissioned" || asset.State == "decommissioning" {
					return nil
				}
				if err := p.DecommissionAsset(egCtx, asset); err != nil {
					slog.Error("decommission asset", "asset", asset.ID, "attempt", attempt.ID, "err", err)
				}
				return nil
			})
		}
	}

	// Reset assets stuck in "provisioning" from a previous crashed cycle back to
	// "pending" so they are retried. Clean up any partial k8s resources first so
	// the retry starts from a known-clean state.
	stuckProvisioning, err := p.pb.ListProvisioningAssets()
	if err != nil {
		slog.Error("list provisioning assets", "err", err)
		p.needsReconn.Store(true)
	}
	for _, asset := range stuckProvisioning {
		asset := asset
		if decommissionAttempts[asset.Attempt] {
			// Will be handled by the decommission path above; don't reset to pending.
			continue
		}
		if err := sem.Acquire(egCtx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			slog.Info("resetting stuck provisioning asset", "asset", asset.ID)
			p.cleanupAssetK8s(egCtx, asset.Attempt, asset.Name)
			if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "pending", "status": "stopped"}); err != nil {
				slog.Error("reset stuck provisioning asset to pending", "asset", asset.ID, "err", err)
			}
			return nil
		})
	}

	// Resume assets stuck in "decommissioning" from a previous crashed cycle.
	stuck, err := p.pb.ListDecommissioningAssets()
	if err != nil {
		slog.Error("list decommissioning assets", "err", err)
		p.needsReconn.Store(true)
	}
	for _, asset := range stuck {
		asset := asset
		if err := sem.Acquire(egCtx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			slog.Info("resuming stuck decommission", "asset", asset.ID)
			if err := p.DecommissionAsset(egCtx, asset); err != nil {
				slog.Error("resume decommission asset", "asset", asset.ID, "err", err)
			}
			return nil
		})
	}

	return eg.Wait()
}
