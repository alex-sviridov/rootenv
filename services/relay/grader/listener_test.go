package grader_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
)

func TestListenAndServeInternal_ingests_valid_lines(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "MARKER"},
	}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = grader.ServeInternalListener(ctx, ln, b, nil) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg, _ := json.Marshal(map[string]string{"asset": "main", "data": "MARKER\n"})
	if _, err := conn.Write(append(msg, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Grades()["1.1"] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("grades[1.1] never became true after valid NDJSON ingestion")
}

func TestListenAndServeInternal_skips_malformed_line_without_closing_connection(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "MARKER"},
	}, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = grader.ServeInternalListener(ctx, ln, b, nil) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("not valid json\n")); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	msg, _ := json.Marshal(map[string]string{"asset": "main", "data": "MARKER\n"})
	if _, err := conn.Write(append(msg, '\n')); err != nil {
		t.Fatalf("write valid after malformed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Grades()["1.1"] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("connection should survive a malformed line and still ingest the next valid one")
}
