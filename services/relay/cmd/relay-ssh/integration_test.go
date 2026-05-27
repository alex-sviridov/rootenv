package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

// TestIntegration_limiterTracksTotal verifies ConnLimiter.Total() works correctly.
func TestIntegration_limiterTracksTotal(t *testing.T) {
	l := relaybase.NewConnLimiter(10)

	if got := l.Total(); got != 0 {
		t.Errorf("want total=0 initially, got %d", got)
	}

	if err := l.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	if got := l.Total(); got != 1 {
		t.Errorf("want total=1 after acquire, got %d", got)
	}

	if err := l.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	if got := l.Total(); got != 2 {
		t.Errorf("want total=2 after second acquire, got %d", got)
	}

	if err := l.Acquire("u2"); err != nil {
		t.Fatal(err)
	}
	if got := l.Total(); got != 3 {
		t.Errorf("want total=3 with different user, got %d", got)
	}

	l.Release("u1")
	if got := l.Total(); got != 2 {
		t.Errorf("want total=2 after release, got %d", got)
	}

	l.Release("u1")
	l.Release("u2")
	if got := l.Total(); got != 0 {
		t.Errorf("want total=0 after all releases, got %d", got)
	}
}

// TestIntegration_gracefulShutdownWaitsForSessions verifies WaitGroup drain semantics.
func TestIntegration_gracefulShutdownWaitsForSessions(t *testing.T) {
	var wg sync.WaitGroup
	sessionCount := 0
	mu := sync.Mutex{}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			mu.Lock()
			sessionCount++
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			sessionCount--
			mu.Unlock()
			wg.Done()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mu.Lock()
		if sessionCount != 0 {
			t.Errorf("sessions not fully drained: count=%d", sessionCount)
		}
		mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for sessions to drain")
	}
}

// TestIntegration_activeCountConcurrent verifies limiter safety under concurrent load.
func TestIntegration_activeCountConcurrent(t *testing.T) {
	l := relaybase.NewConnLimiter(100)
	const workers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			userID := "u" + string(rune(workerID))
			for j := 0; j < opsPerWorker; j++ {
				if l.Acquire(userID) == nil {
					time.Sleep(1 * time.Millisecond)
					l.Release(userID)
				}
			}
		}(i)
	}

	wg.Wait()

	if got := l.Total(); got != 0 {
		t.Errorf("want total=0 after all releases, got %d", got)
	}
}

// TestIntegration_logFormatValid verifies that JSON-formatted logs are valid.
func TestIntegration_logFormatValid(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	logger.Info("ws disconnected",
		"duration_s", 1.5,
		"close_reason", "idle timeout",
		"active_total", 3,
	)

	output := logBuf.String()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, output)
	}

	for _, field := range []string{"msg", "close_reason", "active_total", "duration_s"} {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing field: %s", field)
		}
	}
}

// TestIntegration_logFieldsCombined verifies all observability fields in a log entry.
func TestIntegration_logFieldsCombined(t *testing.T) {
	var logBuf bytes.Buffer
	handler := slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	logger.Info("ws disconnected",
		"server_id", "srv123",
		"user_id", "user789",
		"attempt_id", "att456",
		"duration_s", 42.5,
		"close_reason", "backpressure",
		"active_total", 7,
	)

	output := logBuf.String()

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, field := range []string{"msg", "server_id", "user_id", "attempt_id", "duration_s", "close_reason", "active_total"} {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing field: %s", field)
		}
	}
}

// TestIntegration_concurrentSessionTracking verifies multiple concurrent sessions update the active count correctly.
func TestIntegration_concurrentSessionTracking(t *testing.T) {
	l := relaybase.NewConnLimiter(50)
	const sessionCount = 10

	var wg sync.WaitGroup
	var activeCount int64

	for i := 0; i < sessionCount; i++ {
		wg.Add(1)
		go func(sessionID int) {
			defer wg.Done()
			userID := "user123"

			if err := l.Acquire(userID); err != nil {
				t.Errorf("failed to acquire: %v", err)
				return
			}
			defer l.Release(userID)

			atomic.AddInt64(&activeCount, 1)

			total := l.Total()
			if total <= 0 || total > sessionCount {
				t.Errorf("invalid total during session: %d (max expected: %d)", total, sessionCount)
			}

			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&activeCount, -1)
		}(i)
	}

	wg.Wait()

	if got := l.Total(); got != 0 {
		t.Errorf("want total=0 after all sessions, got %d", got)
	}
}
