package exec_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
	"github.com/coder/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

type fakeExecer struct {
	output []byte
}

func (f *fakeExecer) Exec(ctx context.Context, namespace, podName string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan remotecommand.TerminalSize) error {
	if f.output != nil {
		_, _ = stdout.Write(f.output)
	}
	return nil
}

func TestBackend_routes_output_to_websocket(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "hello from pod" {
		t.Errorf("got %q, want %q", string(msg), "hello from pod")
	}
}

func TestBackend_resize_frame_does_not_crash(t *testing.T) {
	fb := &fakeExecer{}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
	}

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
		close(done)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// send resize frame: \x01 + cols (uint16 LE 80) + rows (uint16 LE 24)
	frame := []byte{0x01, 80, 0, 24, 0}
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.CloseNow()

	select {
	case <-done:
		// Serve returned cleanly
	case <-ctx.Done():
		t.Fatal("timeout waiting for Serve to return")
	}
}

type recordingForwarder struct {
	mu    sync.Mutex
	sends []struct {
		asset string
		data  string
	}
}

func (f *recordingForwarder) Send(asset string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, struct {
		asset string
		data  string
	}{asset, string(data)})
}

func TestBackend_forwards_stdout_chunks_to_forwarder(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	fwd := &recordingForwarder{}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
		Forwarder: fwd,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fwd.mu.Lock()
		n := len(fwd.sends)
		fwd.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fwd.mu.Lock()
	defer fwd.mu.Unlock()
	if len(fwd.sends) == 0 {
		t.Fatal("expected at least one forwarded chunk")
	}
	if fwd.sends[0].asset != "workstation" || fwd.sends[0].data != "hello from pod" {
		t.Errorf("got %+v, want asset=workstation data=%q", fwd.sends[0], "hello from pod")
	}
}

func TestBackend_nil_forwarder_does_not_panic(t *testing.T) {
	fb := &fakeExecer{output: []byte("hello from pod")}
	b := &relayexec.Backend{
		Namespace: "rootenv-lab-atm_123",
		Execer:    fb,
		// Forwarder intentionally nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "workstation", "usr_abc")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if _, msg, err := conn.Read(ctx); err != nil || string(msg) != "hello from pod" {
		t.Fatalf("read: %v, msg=%q", err, msg)
	}
}
