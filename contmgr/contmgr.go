package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"
)

// pbDoer is the PocketBase operations contmgr needs.
type pbDoer interface {
	ListPendingAssets() ([]Asset, error)
	GetAsset(id string) (*Asset, error)
	GetKeysByAsset(assetID string) (*KeysRecord, error)
	PatchAsset(id string, fields map[string]any) error
	PatchKeys(id string, fields map[string]any) error
	ListPendingDecommissionCommands() ([]Command, error)
	PatchCommand(id string, fields map[string]any) error
}

// dockerDoer is the Docker operations contmgr needs.
type dockerDoer interface {
	CreateAndStart(ctx context.Context, p ContainerParams) (string, int, error)
	Remove(ctx context.Context, containerID string) error
	CreateNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
}

type Contmgr struct {
	pb      pbDoer
	docker  dockerDoer
	hostIP  string
	waitSSH func(host string, port int) error
}

func NewContmgr(pb *pbClient, docker *DockerClient, hostIP string) *Contmgr {
	return &Contmgr{pb: pb, docker: docker, hostIP: hostIP, waitSSH: WaitSSH}
}

func (p *Contmgr) ProvisionAsset(ctx context.Context, asset Asset) error {
	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "provisioning"}); err != nil {
		return fmt.Errorf("mark provisioning: %w", err)
	}

	def, err := asset.Def()
	if err != nil {
		return fmt.Errorf("parse asset def: %w", err)
	}

	netName := networkName(asset.Attempt)
	if err := p.docker.CreateNetwork(ctx, netName); err != nil {
		return fmt.Errorf("create network: %w", err)
	}

	privKeyPEM, pubKeyLine, err := GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}

	containerID, hostPort, err := p.docker.CreateAndStart(ctx, ContainerParams{
		Image:         def.Image,
		SSHUser:       def.SSHUser,
		PublicKey:     pubKeyLine,
		CPU:           fmt.Sprintf("%v", def.CPU),
		Memory:        def.Memory,
		NetworkName:   netName,
		ContainerName: asset.Name,
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := p.waitSSH(p.hostIP, hostPort); err != nil {
		return fmt.Errorf("wait ssh: %w", err)
	}

	keysRecord, err := p.pb.GetKeysByAsset(asset.ID)
	if err != nil {
		return fmt.Errorf("get keys: %w", err)
	}

	ciphertext, err := EncryptPrivateKey(privKeyPEM, keysRecord.Secret)
	if err != nil {
		return fmt.Errorf("encrypt key: %w", err)
	}

	if err := p.pb.PatchKeys(keysRecord.ID, map[string]any{
		"key_encrypted": ciphertext,
	}); err != nil {
		return fmt.Errorf("patch keys: %w", err)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{
		"connection": map[string]any{
			"host": p.hostIP,
			"port": hostPort,
			"user": def.SSHUser,
		},
		"configuration": map[string]any{
			"platform": "container",
			"id":       containerID,
		},
	}); err != nil {
		return fmt.Errorf("patch asset connection: %w", err)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{
		"state": "provisioned",
	}); err != nil {
		return fmt.Errorf("patch asset state: %w", err)
	}

	slog.Info("provisioned", "asset", asset.ID, "container", containerID, "port", hostPort)
	return nil
}

func (p *Contmgr) DecommissionAsset(ctx context.Context, asset Asset) error {
	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioning"}); err != nil {
		return fmt.Errorf("mark decommissioning: %w", err)
	}

	var cfg struct {
		ID string `json:"id"`
	}
	if len(asset.Configuration) > 0 {
		_ = json.Unmarshal(asset.Configuration, &cfg)
	}

	if cfg.ID != "" {
		if err := p.docker.Remove(ctx, cfg.ID); err != nil {
			return fmt.Errorf("remove container: %w", err)
		}
	}

	if err := p.docker.RemoveNetwork(ctx, networkName(asset.Attempt)); err != nil {
		return fmt.Errorf("remove network: %w", err)
	}

	if err := p.pb.PatchAsset(asset.ID, map[string]any{"state": "decommissioned"}); err != nil {
		return fmt.Errorf("patch asset state: %w", err)
	}

	slog.Info("decommissioned", "asset", asset.ID, "container", cfg.ID)
	return nil
}

func (p *Contmgr) RunOnce(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	assets, err := p.pb.ListPendingAssets()
	if err != nil {
		slog.Error("list provisioning assets", "err", err)
	}
	for _, asset := range assets {
		asset := asset
		eg.Go(func() error {
			if err := p.ProvisionAsset(ctx, asset); err != nil {
				slog.Error("provision asset", "asset", asset.ID, "err", err)
			}
			return nil
		})
	}

	commands, err := p.pb.ListPendingDecommissionCommands()
	if err != nil {
		slog.Error("list decommission commands", "err", err)
	}
	for _, cmd := range commands {
		cmd := cmd
		eg.Go(func() error {
			if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "running"}); err != nil {
				slog.Error("patch command running", "cmd", cmd.ID, "err", err)
				return nil
			}
			asset, err := p.pb.GetAsset(cmd.Asset)
			if err != nil {
				slog.Error("get asset for decommission", "cmd", cmd.ID, "asset", cmd.Asset, "err", err)
				return nil
			}
			if asset.State == "decommissioning" || asset.State == "decommissioned" {
				slog.Info("skip decommission: asset already in terminal state", "asset", asset.ID, "state", asset.State)
				if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "done"}); err != nil {
					slog.Error("patch command done", "cmd", cmd.ID, "err", err)
				}
				return nil
			}
			if err := p.DecommissionAsset(ctx, *asset); err != nil {
				slog.Error("decommission asset", "asset", asset.ID, "err", err)
				return nil
			}
			if err := p.pb.PatchCommand(cmd.ID, map[string]any{"status": "done"}); err != nil {
				slog.Error("patch command done", "cmd", cmd.ID, "err", err)
			}
			return nil
		})
	}

	return eg.Wait()
}
