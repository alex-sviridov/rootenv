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
	b := grader.NewBackend([]grader.Task{
		{ID: "task1", Type: "term", Template: "echo hi"},
		{ID: "task2", Type: "term", Template: "echo bye"},
	}, nil)

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
	b := grader.NewBackend([]grader.Task{{ID: "task1", Type: "term", Template: "x"}}, nil)

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

func TestBackend_Ingest_marks_task_passed_on_regex_match(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: `chown\s+bob\s+/tmp/labfile`},
	}, nil)

	b.Ingest("main", []byte("$ chown bob /tmp/labfile\n"))

	grades := b.Grades()
	if !grades["1.1"] {
		t.Errorf("grades[1.1] = %v, want true after matching input", grades["1.1"])
	}
}

func TestBackend_Ingest_asset_scoped_task_ignores_other_assets(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: `chown\s+bob`},
	}, nil)

	b.Ingest("other-asset", []byte("chown bob /tmp/labfile\n"))

	grades := b.Grades()
	if grades["1.1"] {
		t.Error("grades[1.1] = true, want false — match was on the wrong asset")
	}
}

func TestBackend_Ingest_lab_wide_task_matches_any_asset(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.2", Type: "term", Template: "echo hi"},
	}, nil)

	b.Ingest("second-asset", []byte("echo hi\n"))

	grades := b.Grades()
	if !grades["1.2"] {
		t.Error("grades[1.2] = false, want true — lab-wide task should match any asset")
	}
}

func TestBackend_Ingest_grade_is_sticky(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "PASS_MARKER"},
	}, nil)

	b.Ingest("main", []byte("PASS_MARKER\n"))
	if !b.Grades()["1.1"] {
		t.Fatal("expected 1.1 to pass")
	}

	// Feed 10+ more lines so PASS_MARKER scrolls out of the ring buffer.
	for i := 0; i < 12; i++ {
		b.Ingest("main", []byte("filler\n"))
	}

	if !b.Grades()["1.1"] {
		t.Error("grades[1.1] = false, want true — grade must stay sticky even after buffer scrolls")
	}
}

func TestBackend_Ingest_skips_already_passed_tasks(t *testing.T) {
	// A task whose regex would also match "filler" is used to prove that once
	// passed, later non-matching input doesn't flip it back, and matching
	// isn't needlessly re-run (behavioral proxy: grade stays true).
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "one"},
	}, nil)
	b.Ingest("main", []byte("one\n"))
	b.Ingest("main", []byte("two\n"))
	if !b.Grades()["1.1"] {
		t.Error("grades[1.1] should remain true")
	}
}

func TestBackend_Ingest_invalid_regex_is_skipped_not_fatal(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "bad", Type: "term", Template: "(unclosed"},
		{ID: "good", Type: "term", Template: "hello"},
	}, nil)

	// Must not panic despite the invalid regex.
	b.Ingest("main", []byte("hello\n"))

	grades := b.Grades()
	if grades["bad"] {
		t.Error("grades[bad] = true, want false — invalid regex never matches")
	}
	if !grades["good"] {
		t.Error("grades[good] = false, want true")
	}
}

func TestBackend_broadcasts_grade_update_to_connected_client(t *testing.T) {
	b := grader.NewBackend([]grader.Task{
		{ID: "1.1", Type: "term", Asset: "main", Template: "DONE_MARKER"},
	}, nil)

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

	// consume bootstrap message
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read bootstrap: %v", err)
	}

	b.Ingest("main", []byte("DONE_MARKER\n"))

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	var grades map[string]bool
	if err := json.Unmarshal(msg, &grades); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !grades["1.1"] {
		t.Errorf("grades[1.1] = %v, want true after broadcast", grades["1.1"])
	}
}
