package relaybase

import (
	"sync"
	"testing"
)

func TestConnLimiter_enforces_max(t *testing.T) {
	l := NewConnLimiter(2)

	if err := l.Acquire("u1"); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := l.Acquire("u1"); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	if err := l.Acquire("u1"); err == nil {
		t.Fatal("third acquire should have failed")
	}
}

func TestConnLimiter_release_allows_reacquire(t *testing.T) {
	l := NewConnLimiter(1)

	if err := l.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	l.Release("u1")
	if err := l.Acquire("u1"); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
}

func TestConnLimiter_independent_users(t *testing.T) {
	l := NewConnLimiter(1)

	if err := l.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire("u2"); err != nil {
		t.Fatalf("different user should not be affected: %v", err)
	}
}

func TestConnLimiter_concurrent(t *testing.T) {
	const max = 5
	const goroutines = 50
	l := NewConnLimiter(max)

	var wg sync.WaitGroup
	acquired := make(chan struct{}, goroutines)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Acquire("u1") == nil {
				acquired <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(acquired)

	if n := len(acquired); n != max {
		t.Errorf("want %d successful acquires, got %d", max, n)
	}
}

func TestConnLimiter_total(t *testing.T) {
	l := NewConnLimiter(10)

	if got := l.Total(); got != 0 {
		t.Errorf("want total=0 initially, got %d", got)
	}

	_ = l.Acquire("u1")
	if got := l.Total(); got != 1 {
		t.Errorf("want total=1 after acquire, got %d", got)
	}

	_ = l.Acquire("u1")
	if got := l.Total(); got != 2 {
		t.Errorf("want total=2 after second acquire, got %d", got)
	}

	_ = l.Acquire("u2")
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
