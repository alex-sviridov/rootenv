package exec_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/coder/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// recordingExecer captures what Serve passes to the underlying exec call.
type recordingExecer struct {
	stdinData  []byte
	resizes    []remotecommand.TerminalSize
	returnErr  error
	outputData []byte
}

func (e *recordingExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error {
	// Drain stdin and resize in background — don't block on either.
	go io.Copy(io.Discard, stdin)  //nolint
	go func() {
		for sz := range resize {
			e.resizes = append(e.resizes, sz)
		}
	}()
	if e.outputData != nil {
		_, _ = stdout.Write(e.outputData)
	}
	return e.returnErr
}

func newTestBackend(ex relayexec.Execer) (*relayexec.Backend, *httptest.Server) {
	b := &relayexec.Backend{Namespace: "test-ns", Execer: ex}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		_ = b.Serve(r.Context(), conn, "pod", "usr")
	}))
	return b, srv
}

func dialTest(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// TestBackend_exec_error_propagated verifies Serve returns the error from Exec.
func TestBackend_exec_error_propagated(t *testing.T) {
	want := errors.New("pod crashed")
	ex := &recordingExecer{returnErr: want}

	done := make(chan error, 1)
	b := &relayexec.Backend{Namespace: "ns", Execer: ex}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		done <- b.Serve(r.Context(), conn, "pod", "usr")
	}))
	defer srv.Close()

	conn := dialTest(t, srv)
	defer conn.CloseNow()

	select {
	case got := <-done:
		if !errors.Is(got, want) {
			t.Errorf("Serve error: got %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Serve to return")
	}
}

// TestBackend_stdin_forwarded verifies client input reaches the exec stdin.
func TestBackend_stdin_forwarded(t *testing.T) {
	// stdinExecer reads all stdin before returning so we can inspect it.
	ex := &stdinCaptureExecer{done: make(chan struct{})}
	_, srv := newTestBackend(ex)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ls -la\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.CloseNow()

	select {
	case <-ex.done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for exec to finish")
	}
	if !bytes.Contains(ex.stdinData, []byte("ls -la\n")) {
		t.Errorf("stdin not forwarded: got %q", ex.stdinData)
	}
}

// TestBackend_resize_decoded verifies resize frames are decoded and forwarded correctly.
func TestBackend_resize_decoded(t *testing.T) {
	resizeDone := make(chan []remotecommand.TerminalSize, 1)
	ex := &blockingExecer{resizeDone: resizeDone}

	_, srv := newTestBackend(ex)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// send resize: cols=120, rows=40  (\x01 + uint16LE cols + uint16LE rows)
	frame := []byte{0x01, 120, 0, 40, 0}
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	conn.CloseNow()

	select {
	case got := <-resizeDone:
		if len(got) == 0 {
			t.Fatal("no resize events received")
		}
		if got[0].Width != 120 || got[0].Height != 40 {
			t.Errorf("resize: got cols=%d rows=%d, want cols=120 rows=40", got[0].Width, got[0].Height)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for resize")
	}
}

// TestBackend_resize_burst_drops_extra verifies that when the resize channel is
// full, subsequent resize frames are dropped rather than blocking.
func TestBackend_resize_burst_drops_extra(t *testing.T) {
	// slowExecer never reads from resize until unblocked, so the channel fills.
	slow := &slowResizeExecer{unblock: make(chan struct{})}
	_, srv := newTestBackend(slow)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send 3 resize frames — channel capacity is 1, so 2 must be dropped without blocking.
	for i := range 3 {
		cols := uint16(80 + i)
		frame := []byte{0x01, byte(cols), 0, 24, 0}
		if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatalf("write resize %d: %v", i, err)
		}
	}

	// Unblock the execer first, then close — Serve must return promptly (no deadlock).
	close(slow.unblock)
	// Give the execer a moment to consume the pending resize before disconnecting.
	time.Sleep(20 * time.Millisecond)
	conn.CloseNow()

	select {
	case <-slow.done:
	case <-ctx.Done():
		t.Fatal("deadlock: Serve blocked on full resize channel")
	}
}

// TestBackend_short_resize_frame_treated_as_stdin verifies that a frame starting
// with \x01 but shorter than 5 bytes is forwarded to stdin, not parsed as resize.
func TestBackend_short_resize_frame_treated_as_stdin(t *testing.T) {
	ex := &stdinCaptureExecer{done: make(chan struct{})}
	_, srv := newTestBackend(ex)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// 3-byte frame starting with 0x01 — too short to be a resize frame.
	short := []byte{0x01, 0x02, 0x03}
	if err := conn.Write(ctx, websocket.MessageBinary, short); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.CloseNow()

	select {
	case <-ex.done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for exec to finish")
	}
	if !bytes.Equal(ex.stdinData, short) {
		t.Errorf("short \x01 frame not forwarded to stdin: got %q", ex.stdinData)
	}
}

// TestBackend_large_output_forwarded verifies chunks larger than 32 KB are
// forwarded in full across multiple WebSocket messages.
func TestBackend_large_output_forwarded(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 100*1024) // 100 KB
	ex := &fakeExecer{output: big}
	_, srv := newTestBackend(ex)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	var received []byte
	for len(received) < len(big) {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			break
		}
		received = append(received, msg...)
	}
	if !bytes.Equal(received, big) {
		t.Errorf("large output: got %d bytes, want %d bytes", len(received), len(big))
	}
}

// TestBackend_ws_disconnect_unblocks_serve verifies that closing the WebSocket
// causes Serve to return rather than hang waiting on exec.
func TestBackend_ws_disconnect_unblocks_serve(t *testing.T) {
	// hangExecer blocks until its context is cancelled.
	hang := &hangExecer{ready: make(chan struct{})}
	b := &relayexec.Backend{Namespace: "ns", Execer: hang}

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		b.Serve(r.Context(), conn, "pod", "usr") //nolint
		close(done)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Wait until exec goroutine has started before disconnecting.
	select {
	case <-hang.ready:
	case <-ctx.Done():
		t.Fatal("timeout waiting for exec to start")
	}

	conn.CloseNow()

	select {
	case <-done:
		// Serve returned after ws disconnect — good.
	case <-ctx.Done():
		t.Fatal("Serve did not return after WebSocket disconnect")
	}
}

// --- helpers ---

// blockingExecer drains resize then signals completion; used to capture resize events.
type blockingExecer struct {
	resizeDone chan []remotecommand.TerminalSize
}

func (e *blockingExecer) Exec(_ context.Context, _, _ string, stdin io.Reader, _, _ io.Writer, resize <-chan remotecommand.TerminalSize) error {
	go io.Copy(io.Discard, stdin) //nolint
	var sizes []remotecommand.TerminalSize
	for sz := range resize {
		sizes = append(sizes, sz)
	}
	e.resizeDone <- sizes
	return nil
}

// stdinCaptureExecer reads all stdin before returning and signals done when finished.
type stdinCaptureExecer struct {
	stdinData []byte
	done      chan struct{}
}

func (e *stdinCaptureExecer) Exec(_ context.Context, _, _ string, stdin io.Reader, _, _ io.Writer, resize <-chan remotecommand.TerminalSize) error {
	defer close(e.done)
	go func() {
		for range resize {
		}
	}()
	e.stdinData, _ = io.ReadAll(stdin)
	return nil
}

// slowResizeExecer blocks reading from resize until unblocked.
type slowResizeExecer struct {
	unblock chan struct{}
	done    chan struct{}
}

func (e *slowResizeExecer) Exec(ctx context.Context, _, _ string, stdin io.Reader, _, _ io.Writer, resize <-chan remotecommand.TerminalSize) error {
	e.done = make(chan struct{})
	defer close(e.done)
	go io.Copy(io.Discard, stdin) //nolint
	select {
	case <-e.unblock:
	case <-ctx.Done():
	}
	for range resize {
	}
	return nil
}

// hangExecer blocks until its context is cancelled; signals ready when entered.
type hangExecer struct {
	ready chan struct{}
}

func (e *hangExecer) Exec(ctx context.Context, _, _ string, stdin io.Reader, _, _ io.Writer, resize <-chan remotecommand.TerminalSize) error {
	go io.Copy(io.Discard, stdin) //nolint
	go func() {
		for range resize {
		}
	}()
	close(e.ready)
	<-ctx.Done()
	return ctx.Err()
}
