package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// pbDoer is the PocketBase operations contmgr needs.
type pbDoer interface {
	ListPendingAssets() ([]Asset, error)
	ListProvisioningAssets() ([]Asset, error)
	GetAsset(id string) (*Asset, error)
	GetAssetConfig(assetID string) (*AssetConfig, error)
	GetKeysByAsset(assetID string) (*KeysRecord, error)
	PatchAsset(id string, fields map[string]any) error
	PatchAssetConfig(id string, fields map[string]any) error
	PatchKeys(id string, fields map[string]any) error
	ListPendingDecommissionCommands() ([]Command, error)
	ListDecommissioningAssets() ([]Asset, error)
	PatchCommand(id string, fields map[string]any) error
	ListProvisionedAssetsByAttempt(attemptID string) ([]Asset, error)
	GetAttempt(attemptID string) (*AttemptRecord, error)
}

type AttemptRecord struct {
	ID   string `json:"id"`
	User string `json:"user"`
}

type Contmgr struct {
	pb             pbDoer
	k8s            k8sDoer
	namespace      string
	infraNamespace string
	pullSecret     string
	needsReconn    atomic.Bool
}

func NewContmgr(pb *pbClient, k8s *K8sClient, namespace, infraNamespace, pullSecret string) *Contmgr {
	return &Contmgr{pb: pb, k8s: k8s, namespace: namespace, infraNamespace: infraNamespace, pullSecret: pullSecret}
}

func (p *Contmgr) NeedsReconnect() bool { return p.needsReconn.Swap(false) }
func (p *Contmgr) SetPB(pb *pbClient)   { p.pb = pb }

// cleanupAssetK8s deletes pod and service for an asset by their deterministic names.
// All deletes are best-effort — not-found is not an error.
func (p *Contmgr) cleanupAssetK8s(ctx context.Context, userID, attemptID, assetName string) {
	pod := podName(userID, attemptID, assetName)
	svc := svcName(userID, attemptID, assetName)
	if err := p.k8s.DeletePod(ctx, p.namespace, pod); err != nil {
		slog.Warn("cleanup: delete pod", "pod", pod, "err", err)
	}
	if err := p.k8s.DeleteService(ctx, p.namespace, svc); err != nil {
		slog.Warn("cleanup: delete svc", "svc", svc, "err", err)
	}
}

func (p *Contmgr) ProvisionAsset(ctx context.Context, asset Asset) error {
	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "provisioning"}); err != nil {
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

	attempt, err := p.pb.GetAttempt(asset.Attempt)
	if err != nil {
		return fmt.Errorf("get attempt: %w", err)
	}
	userID := attempt.User

	if err := p.k8s.EnsureNetworkPolicy(ctx, NetPolParams{
		Namespace:      p.namespace,
		UserID:         userID,
		AttemptID:      asset.Attempt,
		InfraNamespace: p.infraNamespace,
	}); err != nil {
		return fmt.Errorf("ensure network policy: %w", err)
	}

	privKeyPEM, pubKeyLine, err := GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}

	params := PodParams{
		Namespace:       p.namespace,
		UserID:          userID,
		AttemptID:       asset.Attempt,
		AssetName:       asset.Name,
		Image:           def.Image,
		SSHUser:         def.SSHUser,
		CPU:             def.CPU,
		Memory:          def.Memory,
		Disk:            def.Disk,
		ImagePullSecret: p.pullSecret,
	}

	provisionErr := p.doProvision(ctx, asset, cfg, def, params, pubKeyLine, privKeyPEM, userID)
	if provisionErr != nil {
		// Clean up any partial k8s resources so the next retry starts fresh.
		p.cleanupAssetK8s(ctx, userID, asset.Attempt, asset.Name)
		if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "pending"}); err != nil {
			slog.Warn("reset asset to pending after provision failure", "asset", asset.ID, "err", err)
		}
		return provisionErr
	}
	return nil
}

func (p *Contmgr) doProvision(ctx context.Context, asset Asset, cfg *AssetConfig, def *AssetDef, params PodParams, pubKeyLine []byte, privKeyPEM []byte, userID string) error {
	if err := p.k8s.CreatePod(ctx, params); err != nil {
		return fmt.Errorf("create pod: %w", err)
	}

	if err := p.k8s.CreateService(ctx, params); err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	pName := podName(userID, asset.Attempt, asset.Name)
	if err := p.k8s.WaitPodRunning(ctx, p.namespace, pName); err != nil {
		return fmt.Errorf("wait pod running: %w", err)
	}

	pubKeyStr := strings.TrimSpace(string(pubKeyLine))
	// chown uses numeric UID:GID (1000:1000) because the user may not yet exist
	// in /etc/passwd when the pod reaches Running phase — the container init
	// creates it asynchronously via SSH_USERS.
	script := fmt.Sprintf(
		"mkdir -p /home/%[1]s/.ssh && chown 1000:1000 /home/%[1]s/.ssh && chmod 700 /home/%[1]s/.ssh && printf '%%s' %[2]q > /home/%[1]s/.ssh/authorized_keys && chown 1000:1000 /home/%[1]s/.ssh/authorized_keys && chmod 600 /home/%[1]s/.ssh/authorized_keys",
		def.SSHUser, pubKeyStr,
	)
	if err := p.k8s.ExecInPod(ctx, p.namespace, pName, []string{"sh", "-c", script}); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}

	host := svcDNS(svcName(userID, asset.Attempt, asset.Name), p.namespace)

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

	if err := p.pb.PatchAssetConfig(cfg.ID, map[string]any{
		"connection": map[string]any{
			"host": host,
			"port": 22,
			"user": def.SSHUser,
		},
		"configuration": map[string]any{
			"platform": "container",
			"pod":      pName,
			"svc":      svcName(userID, asset.Attempt, asset.Name),
			"user_id":  userID,
		},
	}); err != nil {
		return fmt.Errorf("patch asset config provisioned: %w", err)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "provisioned"}); err != nil {
		return fmt.Errorf("patch asset state provisioned: %w", err)
	}

	slog.Info("provisioned", "asset", asset.ID, "pod", pName, "svc", svcName(userID, asset.Attempt, asset.Name))
	return nil
}

func (p *Contmgr) DecommissionAsset(ctx context.Context, asset Asset) error {
	cfg, err := p.pb.GetAssetConfig(asset.ID)
	if err != nil {
		return fmt.Errorf("get asset config: %w", err)
	}

	var cfgData struct {
		Pod    string `json:"pod"`
		Svc    string `json:"svc"`
		UserID string `json:"user_id"`
	}
	if len(cfg.Configuration) > 0 {
		_ = json.Unmarshal(cfg.Configuration, &cfgData)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioning"}); err != nil {
		return fmt.Errorf("mark decommissioning: %w", err)
	}

	if err := p.k8s.DeletePod(ctx, p.namespace, cfgData.Pod); err != nil {
		return fmt.Errorf("delete pod: %w", err)
	}

	if err := p.k8s.DeleteService(ctx, p.namespace, cfgData.Svc); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	remaining, err := p.pb.ListProvisionedAssetsByAttempt(asset.Attempt)
	if err != nil {
		slog.Warn("could not check remaining assets for attempt; skipping netpol cleanup", "attempt", asset.Attempt, "err", err)
	} else if len(remaining) == 0 {
		if err := p.k8s.DeleteNetworkPolicy(ctx, p.namespace, netpolName(cfgData.UserID, asset.Attempt)); err != nil {
			return fmt.Errorf("delete network policy: %w", err)
		}
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioned"}); err != nil {
		return fmt.Errorf("patch asset state: %w", err)
	}

	slog.Info("decommissioned", "asset", asset.ID, "pod", cfgData.Pod)
	return nil
}

func (p *Contmgr) RunOnce(ctx context.Context) error {
	const maxConcurrent = 10
	sem := semaphore.NewWeighted(maxConcurrent)
	eg, egCtx := errgroup.WithContext(ctx)

	assets, err := p.pb.ListPendingAssets()
	if err != nil {
		slog.Error("list provisioning assets", "err", err)
		p.needsReconn.Store(true)
	}
	for _, asset := range assets {
		asset := asset
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

	commands, err := p.pb.ListPendingDecommissionCommands()
	if err != nil {
		slog.Error("list decommission commands", "err", err)
		p.needsReconn.Store(true)
	}
	for _, cmd := range commands {
		cmd := cmd
		if err := sem.Acquire(egCtx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "running"}); err != nil {
				slog.Error("patch command running", "cmd", cmd.ID, "err", err)
				return nil
			}
			asset, err := p.pb.GetAsset(cmd.Asset)
			if err != nil {
				slog.Error("get asset for decommission", "cmd", cmd.ID, "asset", cmd.Asset, "err", err)
				return nil
			}
			if asset.State == "decommissioned" {
				slog.Info("skip decommission: asset already decommissioned", "asset", asset.ID)
				if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "done"}); err != nil {
					slog.Error("patch command done", "cmd", cmd.ID, "err", err)
				}
				return nil
			}
			if err := p.DecommissionAsset(egCtx, *asset); err != nil {
				slog.Error("decommission asset", "asset", asset.ID, "err", err)
				return nil
			}
			if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "done"}); err != nil {
				slog.Error("patch command done", "cmd", cmd.ID, "err", err)
			}
			return nil
		})
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
		if err := sem.Acquire(egCtx, 1); err != nil {
			break
		}
		eg.Go(func() error {
			defer sem.Release(1)
			slog.Info("resetting stuck provisioning asset", "asset", asset.ID)
			attempt, err := p.pb.GetAttempt(asset.Attempt)
			if err != nil {
				slog.Warn("get attempt for stuck asset", "asset", asset.ID, "err", err)
				// Still try to clean up with empty userID — pod/svc/pvc names won't match but that's safe.
			}
			userID := ""
			if attempt != nil {
				userID = attempt.User
			}
			p.cleanupAssetK8s(egCtx, userID, asset.Attempt, asset.Name)
			if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "pending"}); err != nil {
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
