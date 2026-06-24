package exec_test

import (
	"context"
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
