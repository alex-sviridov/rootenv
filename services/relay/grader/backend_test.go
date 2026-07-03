package grader_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
	"github.com/coder/websocket"
)

func TestBackend_sends_task_grades(t *testing.T) {
	b := &grader.Backend{
		Tasks: []grader.Task{
			{ID: "task1", Type: "term", Template: "echo hi"},
			{ID: "task2", Type: "term", Template: "echo bye"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "", "usr_abc")
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

	var grades map[string]bool
	if err := json.Unmarshal(msg, &grades); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	want := map[string]bool{"task1": false, "task2": false}
	if len(grades) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(grades), len(want), grades)
	}
	for id, grade := range want {
		if got, ok := grades[id]; !ok || got != grade {
			t.Errorf("grades[%q] = %v, %v; want %v, true", id, got, ok, grade)
		}
	}
}

func TestBackend_stays_open_until_client_closes(t *testing.T) {
	b := &grader.Backend{Tasks: []grader.Task{{ID: "task1", Type: "term", Template: "x"}}}

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		_ = b.Serve(r.Context(), conn, "", "usr_abc")
		close(done)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// consume the initial grade message
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read initial message: %v", err)
	}

	// Serve must not have returned yet — connection should still be open.
	select {
	case <-done:
		t.Fatal("Serve returned before client closed the connection")
	case <-time.After(200 * time.Millisecond):
		// expected: still open
	}

	_ = conn.CloseNow()

	select {
	case <-done:
		// expected: Serve returns after client disconnects
	case <-ctx.Done():
		t.Fatal("timeout waiting for Serve to return after client close")
	}
}
