package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// connLimiter tracks active WebSocket connections per user and enforces a cap.
// It is per-relay-instance only; the relay is stateless and horizontally scalable,
// so the limit applies within a single instance, not globally across all instances.
type connLimiter struct {
	max     int
	mu      sync.Mutex
	counts  map[string]*int64
}

func newConnLimiter(max int) *connLimiter {
	return &connLimiter{max: max, counts: make(map[string]*int64)}
}

// Acquire increments the connection count for userID and returns an error if the
// limit is already reached. The caller must call Release when the connection closes.
func (l *connLimiter) Acquire(userID string) error {
	l.mu.Lock()
	counter, ok := l.counts[userID]
	if !ok {
		var n int64
		counter = &n
		l.counts[userID] = counter
	}
	l.mu.Unlock()

	for {
		cur := atomic.LoadInt64(counter)
		if int(cur) >= l.max {
			return fmt.Errorf("connection limit reached (%d)", l.max)
		}
		if atomic.CompareAndSwapInt64(counter, cur, cur+1) {
			return nil
		}
	}
}

// Release decrements the connection count for userID.
func (l *connLimiter) Release(userID string) {
	l.mu.Lock()
	counter, ok := l.counts[userID]
	l.mu.Unlock()
	if ok {
		atomic.AddInt64(counter, -1)
	}
}

// Total returns the total number of active connections across all users.
func (l *connLimiter) Total() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var total int64
	for _, counter := range l.counts {
		total += atomic.LoadInt64(counter)
	}
	return int(total)
}
