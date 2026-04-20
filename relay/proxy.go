package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"
)

const (
	// sshOutBufSize is the capacity of the SSH→WebSocket channel.
	// 512 frames of 256 KB = 128 MB max queued; generous for fast producers
	// (e.g. cat large_file) while still bounding relay memory.
	sshOutBufSize = 512

	// backpressureDeadline is how long the SSH→WS pump waits for the channel
	// to drain before treating the client as stuck and dropping the connection.
	backpressureDeadline = 10 * time.Second

	// wsInChunkSize is the max bytes read per WebSocket message on the stdin path.
	// Matches maxMessageBytes in handler.go — kept separate to avoid coupling.
	wsInChunkSize = 256 * 1024

	// stdinRateLimit and stdinBurst tune the token-bucket on WebSocket→SSH writes.
	// 1 MB/s sustained with a 256 KB burst covers normal interactive use without
	// allowing a malicious client to flood the SSH stream.
	stdinRateLimit = rate.Limit(1 << 20) // 1 MB/s
	stdinBurst     = wsInChunkSize       // one full message burst
)

type proxyConfig struct {
	idleTimeout time.Duration
}

// runProxy bridges a WebSocket connection to an open SSH session's stdin/stdout.
// It blocks until the session ends, then returns. cancel() is called on any
// terminal error so that the caller's context is also cancelled.
//
// The SSH session must already have a shell running; caller owns Close on both
// session and ssh.Client.
func runProxy(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	sshStdin io.WriteCloser,
	sshStdout io.Reader,
	cfg proxyConfig,
	log *slog.Logger,
) {
	limiter := rate.NewLimiter(stdinRateLimit, stdinBurst)

	// sshOut carries chunks from the SSH stdout reader to the WebSocket writer.
	sshOut := make(chan []byte, sshOutBufSize)

	// Goroutine 1: SSH stdout → channel
	go func() {
		buf := make([]byte, wsInChunkSize)
		for {
			n, err := sshStdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])

				// Try to enqueue; if the channel is full wait up to backpressureDeadline
				// before dropping the connection.
				select {
				case sshOut <- chunk:
				case <-time.After(backpressureDeadline):
					log.Warn("backpressure: client too slow, dropping connection")
					cancel()
					_ = conn.Close(websocket.StatusGoingAway, "client too slow")
					return
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Debug("ssh stdout read error", "err", err)
				}
				cancel()
				return
			}
		}
	}()

	// Goroutine 2: channel → WebSocket (SSH stdout → client)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-sshOut:
				if !ok {
					return
				}
				if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
					log.Debug("ws write error", "err", err)
					cancel()
					return
				}
			}
		}
	}()

	// Goroutine 3: WebSocket → SSH stdin (client → server)
	// Owns the idle timer reset and rate limiting.
	idleTimer := time.NewTimer(cfg.idleTimeout)
	defer idleTimer.Stop()

	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				log.Debug("ws read error", "err", err)
				cancel()
				return
			}

			// Reset idle timer on every incoming frame.
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(cfg.idleTimeout)

			// Rate-limit stdin writes.
			if err := limiter.WaitN(ctx, min(len(data), stdinBurst)); err != nil {
				// ctx cancelled — exit cleanly.
				return
			}

			if _, err := sshStdin.Write(data); err != nil {
				log.Debug("ssh stdin write error", "err", err)
				cancel()
				return
			}
		}
	}()

	// Main loop: wait for idle timeout or context cancellation.
	for {
		select {
		case <-idleTimer.C:
			log.Info("idle timeout, closing connection")
			cancel()
			_ = conn.Close(websocket.StatusGoingAway, "idle timeout")
			return
		case <-ctx.Done():
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
