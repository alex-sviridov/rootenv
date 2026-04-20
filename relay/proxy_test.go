package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// proxyHarness holds all the moving parts of a pipeProxy session.
type proxyHarness struct {
	client     *websocket.Conn
	sshIn      *syncBuf    // bytes the proxy forwarded to sshStdin
	sshStdoutW *io.PipeWriter // write here to simulate SSH output
	cancel     context.CancelFunc
}

// newProxyHarness wires runProxy to in-memory pipes and returns a harness.
// The idle timeout is configurable; SSH stdout is controlled via harness.sshStdoutW.
func newProxyHarness(t *testing.T, cfg proxyConfig) *proxyHarness {
	t.Helper()
	return newProxyHarnessWithFn(t, cfg, nil)
}

// newProxyHarnessWithFn is like newProxyHarness but accepts a custom windowChangeFn for testing.
func newProxyHarnessWithFn(t *testing.T, cfg proxyConfig, windowChangeFn func(rows, cols int) error) *proxyHarness {
	t.Helper()

	sshStdoutR, sshStdoutW := io.Pipe()
	sshStdinR, sshStdinW := io.Pipe()

	buf := &syncBuf{}
	go func() { io.Copy(buf, sshStdinR) }() //nolint:errcheck

	// proxyCtx is owned by the server handler; we expose cancel to the harness
	// so tests can shut down cleanly.
	proxyCtx, proxyCancel := context.WithCancel(context.Background())

	proxying := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		close(proxying)
		tokenRefreshChan := make(chan string, 1)
		resizeChan := make(chan [2]uint16, 1)
		if windowChangeFn == nil {
			windowChangeFn = func(rows, cols int) error { return nil }
		}
		_ = runProxy(proxyCtx, proxyCancel, c, sshStdinW, sshStdoutR, cfg, tokenRefreshChan, resizeChan, windowChangeFn, slog.Default())
	}))
	t.Cleanup(func() {
		proxyCancel()
		sshStdoutW.Close()
		srv.Close()
	})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}

	// Wait until the handler has called runProxy so goroutines are running.
	select {
	case <-proxying:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not start in time")
	}

	return &proxyHarness{
		client:     client,
		sshIn:      buf,
		sshStdoutW: sshStdoutW,
		cancel:     proxyCancel,
	}
}

// syncBuf is a thread-safe bytes.Buffer.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, b.buf.Len())
	copy(cp, b.buf.Bytes())
	return cp
}

// TestProxy_sshOutputReachesClient verifies that data written to sshStdout
// arrives at the WebSocket client.
func TestProxy_sshOutputReachesClient(t *testing.T) {
	want := []byte("hello from ssh\n")

	h := newProxyHarness(t, proxyConfig{idleTimeout: 5 * time.Second})
	defer h.cancel()

	// Write SSH output after the proxy is running.
	if _, err := h.sshStdoutW.Write(want); err != nil {
		t.Fatalf("sshStdout write: %v", err)
	}

	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()

	_, got, err := h.client.Read(ctx)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestProxy_clientInputReachesSSH verifies that data written by the client
// reaches sshStdin.
func TestProxy_clientInputReachesSSH(t *testing.T) {
	input := []byte("ls -la\n")

	h := newProxyHarness(t, proxyConfig{idleTimeout: 5 * time.Second})
	defer h.cancel()

	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()

	if err := h.client.Write(ctx, websocket.MessageBinary, input); err != nil {
		t.Fatalf("client write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Equal(h.sshIn.Bytes(), input) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("want sshStdin %q, got %q", input, h.sshIn.Bytes())
}

// TestProxy_idleTimeout verifies the connection is closed after the idle duration
// with no WebSocket activity.
func TestProxy_idleTimeout(t *testing.T) {
	h := newProxyHarness(t, proxyConfig{idleTimeout: 100 * time.Millisecond})

	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()

	_, _, err := h.client.Read(ctx)
	if err == nil {
		t.Fatal("expected connection to be closed after idle timeout")
	}
}

// TestProxy_loadConfig_idleTimeout checks that RELAY_IDLE_TIMEOUT env parses correctly.
func TestProxy_loadConfig_idleTimeout(t *testing.T) {
	t.Setenv("RELAY_IDLE_TIMEOUT", "15m")
	cfg := loadConfig()
	if cfg.idleTimeout != 15*time.Minute {
		t.Errorf("want 15m, got %v", cfg.idleTimeout)
	}
}

func TestProxy_loadConfig_idleTimeout_default(t *testing.T) {
	t.Setenv("RELAY_IDLE_TIMEOUT", "")
	cfg := loadConfig()
	if cfg.idleTimeout != 30*time.Minute {
		t.Errorf("want 30m default, got %v", cfg.idleTimeout)
	}
}

func TestProxy_loadConfig_privateKeyPath(t *testing.T) {
	t.Setenv("RELAY_PRIVATE_KEY_PATH", "/etc/relay/id_ed25519")
	cfg := loadConfig()
	if cfg.privateKeyPath != "/etc/relay/id_ed25519" {
		t.Errorf("want /etc/relay/id_ed25519, got %s", cfg.privateKeyPath)
	}
}

// TestProxy_resizeFrameCallsWindowChange verifies that a resize control frame
// calls the windowChangeFn with the correct dimensions and is not forwarded to sshStdin.
func TestProxy_resizeFrameCallsWindowChange(t *testing.T) {
	var (
		mu        sync.Mutex
		callCount int
		gotRows   int
		gotCols   int
	)
	windowChangeFn := func(rows, cols int) error {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		gotRows = rows
		gotCols = cols
		return nil
	}

	h := newProxyHarnessWithFn(t, proxyConfig{idleTimeout: 5 * time.Second}, windowChangeFn)
	defer h.cancel()

	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()

	// Send a resize frame: \x01 + cols (80, uint16 LE) + rows (24, uint16 LE)
	resizeFrame := []byte{0x01, 80, 0, 24, 0}
	if err := h.client.Write(ctx, websocket.MessageBinary, resizeFrame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// Give the resize handler a moment to process.
	time.Sleep(100 * time.Millisecond)

	// Verify windowChangeFn was called with the correct dimensions.
	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("want 1 call to windowChangeFn, got %d", callCount)
	}
	if gotRows != 24 {
		t.Errorf("want rows=24, got %d", gotRows)
	}
	if gotCols != 80 {
		t.Errorf("want cols=80, got %d", gotCols)
	}

	// Verify the frame was not forwarded to sshStdin.
	if len(h.sshIn.Bytes()) > 0 {
		t.Errorf("want sshStdin empty, got %q", h.sshIn.Bytes())
	}
}

// TestProxy_resizeFrameIgnoredWhenInvalid verifies that frames that are not exactly
// 5 bytes are treated as stdin data, not resize frames.
func TestProxy_resizeFrameIgnoredWhenInvalid(t *testing.T) {
	var callCount int
	windowChangeFn := func(rows, cols int) error {
		callCount++
		return nil
	}

	h := newProxyHarnessWithFn(t, proxyConfig{idleTimeout: 5 * time.Second}, windowChangeFn)
	defer h.cancel()

	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()

	// Send an incomplete resize frame (should be treated as stdin).
	incompleteFrame := []byte{0x01, 80, 0}
	if err := h.client.Write(ctx, websocket.MessageBinary, incompleteFrame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// Give it a moment to process.
	time.Sleep(100 * time.Millisecond)

	// Verify windowChangeFn was not called.
	if callCount != 0 {
		t.Errorf("want 0 calls to windowChangeFn, got %d", callCount)
	}

	// Verify the frame was forwarded to sshStdin.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Equal(h.sshIn.Bytes(), incompleteFrame) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("want sshStdin %q, got %q", incompleteFrame, h.sshIn.Bytes())
}
