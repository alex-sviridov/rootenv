package exec

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Execer abstracts kubectl exec so it can be faked in tests.
type Execer interface {
	Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error
}

// Backend implements relaybase.Backend using kubectl exec.
// assetName == pod name — the operator ensures this invariant.
type Backend struct {
	Namespace string
	Execer    Execer
}

// Serve proxies WebSocket ↔ kubectl exec stream for the named asset.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log := slog.With("asset", assetName, "user_id", userID)

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	resizeCh := make(chan remotecommand.TerminalSize, 1)

	execDone := make(chan error, 1)
	go func() {
		execDone <- b.Execer.Exec(ctx, b.Namespace, assetName, stdinR, stdoutW, io.Discard, resizeCh)
		_ = stdoutW.Close()
	}()

	// stdout → WebSocket
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → stdin (with resize handling)
	go func() {
		defer stdinW.Close()
		defer close(resizeCh)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			// resize frame: \x01 + cols (uint16 LE) + rows (uint16 LE)
			if len(data) == 5 && data[0] == 0x01 {
				cols := binary.LittleEndian.Uint16(data[1:3])
				rows := binary.LittleEndian.Uint16(data[3:5])
				log.Debug("resize", "cols", cols, "rows", rows)
				select {
				case resizeCh <- remotecommand.TerminalSize{Width: cols, Height: rows}:
				default: // drop if buffer full (last resize wins during burst)
				}
				continue
			}
			if _, err := stdinW.Write(data); err != nil {
				return
			}
		}
	}()

	err := <-execDone
	<-stdoutDone
	return err
}

// chanSizeQueue implements remotecommand.TerminalSizeQueue over a channel.
type chanSizeQueue struct{ ch <-chan remotecommand.TerminalSize }

func (q *chanSizeQueue) Next() *remotecommand.TerminalSize {
	sz, ok := <-q.ch
	if !ok {
		return nil
	}
	return &sz
}

// KubeExecer implements Execer using a real Kubernetes client.
type KubeExecer struct {
	client *kubernetes.Clientset
	config *rest.Config
}

// NewKubeExecer creates an Execer using the in-cluster ServiceAccount.
func NewKubeExecer() (*KubeExecer, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubeExecer{client: cs, config: cfg}, nil
}

func (k *KubeExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error {
	req := k.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/sh"},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.config, http.MethodPost, req.URL())
	if err != nil {
		return err
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: &chanSizeQueue{ch: resize},
	})
}
