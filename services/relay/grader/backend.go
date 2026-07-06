package grader

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"sync"

	"github.com/coder/websocket"
)

// Backend implements relaybase.Backend for the grader relay type.
// It matches each task's regex Template against real-time terminal output
// forwarded by relay-exec (via Ingest) and reports sticky pass/fail grades
// to every connected frontend WebSocket client.
type Backend struct {
	Tasks []Task
	Log   *slog.Logger // defaults to slog.Default() if nil

	initOnce sync.Once
	regexes  map[string]*regexp.Regexp // compiled once, keyed by task ID; missing key = invalid/skipped

	mu      sync.Mutex
	assets  map[string]*assetBuffer
	grades  map[string]bool
	clients map[*websocket.Conn]struct{}
}

// NewBackend constructs a Backend with regexes pre-compiled and grading maps
// initialized. Prefer this over a bare &Backend{Tasks: ...} literal so tests
// and callers don't need to know about lazy initialization.
func NewBackend(tasks []Task, log *slog.Logger) *Backend {
	b := &Backend{Tasks: tasks, Log: log}
	b.init()
	return b
}

func (b *Backend) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

func (b *Backend) init() {
	b.initOnce.Do(func() {
		b.regexes = make(map[string]*regexp.Regexp, len(b.Tasks))
		b.assets = make(map[string]*assetBuffer)
		b.grades = make(map[string]bool, len(b.Tasks))
		b.clients = make(map[*websocket.Conn]struct{})
		for _, task := range b.Tasks {
			re, err := regexp.Compile(task.Template)
			if err != nil {
				b.logger().Error("invalid task template regex, task will never pass", "task_id", task.ID, "err", err)
				continue
			}
			b.regexes[task.ID] = re
		}
	})
}

// Grades returns a snapshot copy of the current grade map.
func (b *Backend) Grades() map[string]bool {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]bool, len(b.grades))
	for id := range b.regexes {
		out[id] = b.grades[id]
	}
	// Tasks with invalid regex never appear in b.regexes; still report them
	// as false so the frontend sees an entry for every task.
	for _, task := range b.Tasks {
		if _, ok := out[task.ID]; !ok {
			out[task.ID] = false
		}
	}
	return out
}

// Ingest reassembles chunk into asset's line buffer and re-runs matching for
// every not-yet-passed task against the relevant buffer(s).
func (b *Backend) Ingest(asset string, chunk []byte) {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()

	buf, ok := b.assets[asset]
	if !ok {
		buf = &assetBuffer{}
		b.assets[asset] = buf
	}
	if !buf.Ingest(chunk) {
		return
	}

	changed := false
	for _, task := range b.Tasks {
		if b.grades[task.ID] {
			continue // sticky: already passed
		}
		re, ok := b.regexes[task.ID]
		if !ok {
			continue // invalid regex, never matches
		}
		if task.Asset != "" {
			if task.Asset != asset {
				continue
			}
			if re.MatchString(buf.Joined()) {
				b.grades[task.ID] = true
				changed = true
			}
			continue
		}
		// lab-wide: check every asset's buffer
		for _, ab := range b.assets {
			if re.MatchString(ab.Joined()) {
				b.grades[task.ID] = true
				changed = true
				break
			}
		}
	}

	if changed {
		b.broadcastLocked()
	}
}

// broadcastLocked sends the current grade map to every connected client.
// Caller must hold b.mu.
func (b *Backend) broadcastLocked() {
	payload, err := json.Marshal(b.gradesLocked())
	if err != nil {
		b.logger().Error("failed to marshal grades for broadcast", "err", err)
		return
	}
	for conn := range b.clients {
		if err := conn.Write(context.Background(), websocket.MessageText, payload); err != nil {
			delete(b.clients, conn)
		}
	}
}

// gradesLocked returns the full grade map including invalid-regex tasks as
// false. Caller must hold b.mu.
func (b *Backend) gradesLocked() map[string]bool {
	out := make(map[string]bool, len(b.Tasks))
	for _, task := range b.Tasks {
		out[task.ID] = b.grades[task.ID]
	}
	return out
}

// Serve registers conn for broadcast, sends the current grade snapshot, then
// blocks reading (discarding) client messages until the connection closes,
// at which point conn is deregistered.
// assetName is unused — grading is attempt-scoped, not asset-scoped.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	b.init()
	log := b.logger().With("user_id", userID)

	b.mu.Lock()
	b.clients[conn] = struct{}{}
	payload, err := json.Marshal(b.gradesLocked())
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		log.Error("failed to send grade report", "err", err)
		b.mu.Lock()
		delete(b.clients, conn)
		b.mu.Unlock()
		return err
	}

	defer func() {
		b.mu.Lock()
		delete(b.clients, conn)
		b.mu.Unlock()
	}()

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return nil
		}
	}
}
