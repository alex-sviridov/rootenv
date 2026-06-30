package exec

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/tools/remotecommand"
)

// podExecURL builds the Kubernetes API URL for pod exec without importing
// k8s.io/client-go/kubernetes, k8s.io/client-go/kubernetes/scheme, or k8s.io/api/core/v1.
// Those packages register every API group type at init time, bloating the binary by ~10 MB.
func podExecURL(host, namespace, podName string) *url.URL {
	host = strings.TrimRight(host, "/")
	u, _ := url.Parse(host + "/api/v1/namespaces/" + namespace + "/pods/" + podName + "/exec")
	q := url.Values{
		"command": {"/bin/sh"},
		"stdin":   {"true"},
		"stdout":  {"true"},
		"stderr":  {"true"},
		"tty":     {"true"},
	}
	u.RawQuery = q.Encode()
	return u
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

// KubeExecer implements Execer using a real Kubernetes in-cluster client.
// transport and upgrader are built once at startup and reused across sessions.
type KubeExecer struct {
	host      string
	transport http.RoundTripper
	upgrader  spdy.Upgrader
}

// NewKubeExecer creates an Execer using the in-cluster ServiceAccount.
// It builds the TLS transport once so each Exec call skips TLS re-initialization.
func NewKubeExecer() (*KubeExecer, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	return &KubeExecer{host: cfg.Host, transport: transport, upgrader: upgrader}, nil
}

func (k *KubeExecer) newExecutor(u *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewSPDYExecutorForTransports(k.transport, k.upgrader, "POST", u)
}

func (k *KubeExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error {
	exec, err := k.newExecutor(podExecURL(k.host, namespace, podName))
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
