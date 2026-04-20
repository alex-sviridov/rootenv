package main

import (
	"sync"
	"testing"
)

func TestConnLimiter_enforces_max(t *testing.T) {
	l := newConnLimiter(2)

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
	l := newConnLimiter(1)

	if err := l.Acquire("u1"); err != nil {
		t.Fatal(err)
	}
	l.Release("u1")
	if err := l.Acquire("u1"); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
}

func TestConnLimiter_independent_users(t *testing.T) {
	l := newConnLimiter(1)

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
	l := newConnLimiter(max)

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
