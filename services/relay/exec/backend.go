package exec

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"

	"github.com/coder/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// Execer abstracts kubectl exec so it can be faked in tests.
type Execer interface {
	Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error
}

// forwarder abstracts Forwarder.Send so Backend can be tested without a real
// network connection. *Forwarder satisfies this interface.
type forwarder interface {
	Send(asset string, data []byte)
}

// Backend implements relaybase.Backend using kubectl exec.
// assetName == pod name — the operator ensures this invariant.
type Backend struct {
	Namespace string
	Execer    Execer
	Forwarder forwarder    // optional; nil means grading is disabled for this instance
	Log       *slog.Logger // defaults to slog.Default() if nil
}

func (b *Backend) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// Serve proxies WebSocket ↔ kubectl exec stream for the named asset.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log := b.logger().With("asset", assetName, "user_id", userID, "namespace", b.Namespace)

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	resizeCh := make(chan remotecommand.TerminalSize, 1)

	execDone := make(chan error, 1)
	go func() {
		err := b.Execer.Exec(ctx, b.Namespace, assetName, stdinR, stdoutW, io.Discard, resizeCh)
		execDone <- err
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
				if b.Forwarder != nil {
					b.Forwarder.Send(assetName, buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket → stdin (with resize handling)
	go func() {
		defer cancel() // WS disconnect cancels the exec context
		defer func() { _ = stdinW.Close() }()
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
	if err != nil {
		log.Error("exec session ended with error", "err", err)
	}
	return err
}
