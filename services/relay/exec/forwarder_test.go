package exec_test

import (
	"bufio"
	"encoding/json"
	"net"
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

func TestForwarder_delivers_to_listening_server(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

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
