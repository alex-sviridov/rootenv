package ssh

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"github.com/coder/websocket"
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
	metrics     *SSHMetrics // nil-safe
}

// runProxy bridges a WebSocket connection to an open SSH session's stdin/stdout.
// It blocks until the session ends, then returns a reason string describing why
// the connection closed. cancel() is called on any terminal error so that the
// caller's context is also cancelled.
//
// The SSH session must already have a shell running; caller owns Close on both
// session and ssh.Client.
//
// Special message formats:
//   - \x00REFRESH\n<token> — token refresh request (not forwarded to SSH)
//   - \x01 + cols (uint16 LE) + rows (uint16 LE) — resize frame (forwarded to windowChangeFn)
func runProxy(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *websocket.Conn,
	sshStdin io.WriteCloser,
	sshStdout io.Reader,
	cfg proxyConfig,
	tokenRefreshChan chan string,
	resizeChan chan [2]uint16,
	windowChangeFn func(rows, cols int) error,
	log *slog.Logger,
) string {
	limiter := rate.NewLimiter(stdinRateLimit, stdinBurst)

	// sshOut carries chunks from the SSH stdout reader to the WebSocket writer.
	sshOut := make(chan []byte, sshOutBufSize)

	// closeReason tracks why the connection is closing.
	var closeReasonMu sync.Mutex
	closeReason := "normal"
	setCloseReason := func(reason string) {
		closeReasonMu.Lock()
		if closeReason == "normal" {
			closeReason = reason
		}
		closeReasonMu.Unlock()
	}

	// Goroutine 1: SSH stdout → channel
	go func() {
		buf := make([]byte, wsInChunkSize)
		for {
			n, err := sshStdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if cfg.metrics != nil {
					cfg.metrics.bytesOut.Add(float64(n))
				}

				// Try to enqueue; if the channel is full wait up to backpressureDeadline
				// before dropping the connection.
				select {
				case sshOut <- chunk:
				case <-time.After(backpressureDeadline):
					log.Warn("backpressure: client too slow, dropping connection")
					if cfg.metrics != nil {
						cfg.metrics.backpressureDrops.Inc()
					}
					setCloseReason("backpressure")
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
	// Also intercepts control messages (token refresh and resize frames).
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

			// Check for resize control frame: \x01 + cols (uint16 LE) + rows (uint16 LE)
			if len(data) == 5 && data[0] == 0x01 {
				cols := binary.LittleEndian.Uint16(data[1:3])
				rows := binary.LittleEndian.Uint16(data[3:5])
				if resizeChan != nil {
					select {
					case resizeChan <- [2]uint16{cols, rows}:
					default: // drop if channel full — last resize wins
					}
				}
				continue
			}

			// Check for token refresh control message: \x00REFRESH\n<token>
			if len(data) > 10 && data[0] == 0x00 && string(data[1:9]) == "REFRESH\n" {
				newToken := string(data[9:])
				select {
				case tokenRefreshChan <- newToken:
					log.Debug("token refresh requested")
				case <-ctx.Done():
					return
				}
				continue
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
			if cfg.metrics != nil {
				cfg.metrics.bytesIn.Add(float64(len(data)))
			}
		}
	}()

	// Goroutine 4: resize handler
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case dim := <-resizeChan:
				if windowChangeFn != nil {
					rows := int(dim[1])
					cols := int(dim[0])
					if err := windowChangeFn(rows, cols); err != nil {
						log.Debug("window change failed", "err", err)
					}
				}
			}
		}
	}()

	// Main loop: wait for idle timeout or context cancellation.
	for {
		select {
		case <-idleTimer.C:
			log.Info("idle timeout, closing connection")
			setCloseReason("idle timeout")
			cancel()
			_ = conn.Close(websocket.StatusGoingAway, "idle timeout")
			return closeReason
		case <-ctx.Done():
			closeReasonMu.Lock()
			reason := closeReason
			closeReasonMu.Unlock()
			return reason
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
