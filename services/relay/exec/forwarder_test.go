package exec_test

import (
	"bufio"
	"encoding/json"
	"net"
	"runtime"
	"testing"
	"time"

	relayexec "github.com/alexsviridov/linuxlab/relay/exec"
)

func TestForwarder_Send_never_blocks_when_unreachable(t *testing.T) {
	f := relayexec.NewForwarder("127.0.0.1:1", nil) // port 1: nothing listens there
	defer f.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			f.Send("main", []byte("line\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked for 2s while forwarder is unreachable — must be non-blocking")
	}
}

func TestForwarder_Send_noop_when_addr_empty(t *testing.T) {
	f := relayexec.NewForwarder("", nil)
	defer f.Close()

	done := make(chan struct{})
	go func() {
		f.Send("main", []byte("line\n"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Send blocked with empty addr — no-op forwarder must return immediately")
	}
}

func TestForwarder_Close_waits_for_background_goroutine_to_exit(t *testing.T) {
	before := runtime.NumGoroutine()

	// 10.255.255.1 is a non-routable address: net.DialTimeout will sit
	// blocked for the full 5s timeout instead of failing fast, so Close()
	// is very likely called while run() is inside DialTimeout.
	f := relayexec.NewForwarder("10.255.255.1:81", nil)

	// Give run() a moment to enter its first DialTimeout call.
	time.Sleep(50 * time.Millisecond)

	f.Close()

	// If Close() returned without waiting for run() to exit, the extra
	// goroutine could still be blocked in DialTimeout right now. Poll
	// briefly for the goroutine count to settle back to its pre-test level
	// to confirm run() has actually stopped, rather than trusting a single
	// racy snapshot.
	deadline := time.Now().Add(200 * time.Millisecond)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("goroutine count did not return to baseline after Close(): before=%d after=%d — run() may still be running", before, after)
	}
}

func TestForwarder_delivers_to_listening_server(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			received <- scanner.Text()
		}
	}()

	f := relayexec.NewForwarder(ln.Addr().String(), nil)
	defer f.Close()

	f.Send("main", []byte("hello\n"))

	select {
	case line := <-received:
		var msg map[string]string
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if msg["asset"] != "main" || msg["data"] != "hello\n" {
			t.Errorf("got %+v, want asset=main data=%q", msg, "hello\n")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwarder never delivered the message")
	}
}
