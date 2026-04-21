package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type DockerClient struct {
	cli *client.Client
}

type ContainerParams struct {
	Image         string
	SSHUser       string
	PublicKey     []byte
	CPU           string // e.g. "1"
	Memory        string // e.g. "512MB"
	NetworkName   string // Docker network to join
	ContainerName string // DNS name inside the network
}

func newDockerClient(host string) (*DockerClient, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}
	return &DockerClient{cli: cli}, nil
}

// CreateAndStart creates, starts a container and returns (containerID, hostPort, error).
func (d *DockerClient) CreateAndStart(ctx context.Context, p ContainerParams) (string, int, error) {
	hostPort, err := grabFreePort()
	if err != nil {
		return "", 0, fmt.Errorf("grab free port: %w", err)
	}

	memBytes, err := parseMemory(p.Memory)
	if err != nil {
		return "", 0, fmt.Errorf("parse memory %q: %w", p.Memory, err)
	}

	port22, err := dockernetwork.ParsePort("22/tcp")
	if err != nil {
		return "", 0, err
	}

	portBindings := dockernetwork.PortMap{
		port22: []dockernetwork.PortBinding{{HostIP: netip.AddrFrom4([4]byte{0, 0, 0, 0}), HostPort: fmt.Sprintf("%d", hostPort)}},
	}
	exposedPorts := dockernetwork.PortSet{port22: struct{}{}}

	// Best-effort pull; container create will fail if image is truly missing.
	_ = pullImage(ctx, d.cli, p.Image)

	networkingConfig := &dockernetwork.NetworkingConfig{}
	if p.NetworkName != "" {
		networkingConfig.EndpointsConfig = map[string]*dockernetwork.EndpointSettings{
			p.NetworkName: {Aliases: []string{p.ContainerName}},
		}
	}

	resp, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        p.Image,
			Hostname:     p.ContainerName,
			ExposedPorts: exposedPorts,
			Env:          []string{"SSH_USERS=" + p.SSHUser + ":1000:1000"},
		},
		HostConfig: &container.HostConfig{
			PortBindings: portBindings,
			Resources: container.Resources{
				NanoCPUs: parseCPU(p.CPU),
				Memory:   memBytes,
			},
		},
		NetworkingConfig: networkingConfig,
	})
	if err != nil {
		return "", 0, fmt.Errorf("container create: %w", err)
	}

	if _, err := d.cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return "", 0, fmt.Errorf("container start: %w", err)
	}

	pubKeyStr := strings.TrimSpace(string(p.PublicKey))
	script := fmt.Sprintf(
		"mkdir -p /conf.d/authorized_keys && printf '%%s' %q > /conf.d/authorized_keys/%s",
		pubKeyStr, p.SSHUser,
	)
	if err := execInContainer(ctx, d.cli, resp.ID, []string{"sh", "-c", script}); err != nil {
		return "", 0, fmt.Errorf("write authorized_keys: %w", err)
	}

	return resp.ID, hostPort, nil
}

// CreateNetwork creates a Docker bridge network with the given name (idempotent — ignores already-exists).
func (d *DockerClient) CreateNetwork(ctx context.Context, name string) error {
	_, err := d.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge"})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("network create %q: %w", name, err)
	}
	return nil
}

// RemoveNetwork removes a Docker network by name.
func (d *DockerClient) RemoveNetwork(ctx context.Context, name string) error {
	_, err := d.cli.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("network remove %q: %w", name, err)
	}
	return nil
}

// networkName returns the Docker network name for an attempt.
func networkName(attemptID string) string {
	return "lab-" + attemptID
}

// Remove stops (if running) and removes a container.
func (d *DockerClient) Remove(ctx context.Context, containerID string) error {
	_, _ = d.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{})
	if _, err := d.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("container remove: %w", err)
	}
	return nil
}

// WaitSSH polls TCP until SSH port responds or times out.
func WaitSSH(host string, port int) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for range 30 {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("SSH not ready at %s after 30 attempts", addr)
}

func grabFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func execInContainer(ctx context.Context, cli *client.Client, containerID string, cmd []string) error {
	exec, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	_, err = cli.ExecStart(ctx, exec.ID, client.ExecStartOptions{})
	return err
}

func pullImage(ctx context.Context, cli *client.Client, img string) error {
	resp, err := cli.ImagePull(ctx, img, client.ImagePullOptions{})
	if err != nil {
		return err
	}
	return resp.Wait(ctx)
}

// parseCPU converts a CPU string like "1" or "0.5" to NanoCPUs.
func parseCPU(cpu string) int64 {
	if cpu == "" {
		return 0
	}
	var v float64
	fmt.Sscanf(cpu, "%f", &v)
	return int64(v * 1e9)
}

// parseMemory converts strings like "512MB", "1GB", "256m" to bytes.
func parseMemory(mem string) (int64, error) {
	if mem == "" {
		return 0, nil
	}
	mem = strings.TrimSpace(mem)
	upper := strings.ToUpper(mem)
	units := map[string]int64{
		"GB": 1 << 30,
		"MB": 1 << 20,
		"KB": 1 << 10,
		"G":  1 << 30,
		"M":  1 << 20,
		"K":  1 << 10,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(upper, suffix) {
			var v int64
			fmt.Sscanf(mem[:len(mem)-len(suffix)], "%d", &v)
			return v * mult, nil
		}
	}
	var v int64
	if _, err := fmt.Sscanf(mem, "%d", &v); err != nil {
		return 0, fmt.Errorf("unrecognized memory format: %q", mem)
	}
	return v, nil
}
