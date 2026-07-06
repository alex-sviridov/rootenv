package exec

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"time"
)

const forwarderChanCapacity = 256

type forwardMsg struct {
	Asset string `json:"asset"`
	Data  string `json:"data"`
}

// Forwarder fire-and-forget streams terminal output chunks to relay-grader's
// internal port. If relay-grader is unreachable, down, or slow, Send drops
// the chunk instead of blocking — relay-exec's terminal sessions must never
// be affected by grader availability.
type Forwarder struct {
	addr string
	ch   chan forwardMsg
	log  *slog.Logger

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

// NewForwarder starts a background connection/reconnect loop to addr. If addr
// is empty, the returned Forwarder is a no-op — Send returns immediately and
// nothing is ever dialed. This lets relay-exec run fine with grading disabled
// (e.g. local dev without a relay-grader instance).
func NewForwarder(addr string, log *slog.Logger) *Forwarder {
	if log == nil {
		log = slog.Default()
	}
	f := &Forwarder{
		addr: addr,
		ch:   make(chan forwardMsg, forwarderChanCapacity),
		log:  log,
		done: make(chan struct{}),
	}
	if addr != "" {
		f.wg.Add(1)
		go f.run()
	}
	return f
}

// Send enqueues data for forwarding, tagged with asset. Never blocks: if the
// internal channel is full (forwarder disconnected or backed up), the chunk
// is dropped and logged at Debug level.
func (f *Forwarder) Send(asset string, data []byte) {
	if f.addr == "" {
		return
	}
	select {
	case f.ch <- forwardMsg{Asset: asset, Data: string(data)}:
	default:
		f.log.Debug("dropping forwarder message: channel full or disconnected", "asset", asset)
	}
}

// Close stops the background reconnect loop and blocks until it has actually
// exited. Note: an in-flight net.DialTimeout call inside run() is not
// interrupted — Close() waits for it to return (up to its own timeout) rather
// than aborting it, but run() will not perform any further work afterward.
func (f *Forwarder) Close() {
	f.closeOnce.Do(func() { close(f.done) })
	f.wg.Wait()
}

func (f *Forwarder) run() {
	defer f.wg.Done()
	backoff := 500 * time.Millisecond
	const maxBackoff = 10 * time.Second

	for {
		select {
		case <-f.done:
			return
		default:
		}

		conn, err := net.DialTimeout("tcp", f.addr, 5*time.Second)
		if err != nil {
			f.log.Warn("forwarder: dial failed, will retry", "addr", f.addr, "err", err)
			select {
			case <-time.After(backoff):
			case <-f.done:
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = 500 * time.Millisecond
		f.drainInto(conn)
		_ = conn.Close()
	}
}

// drainInto writes queued messages to conn until a write fails or Close is
// called. A write failure returns so run() can reconnect; the message being
// written when the failure occurred is dropped (fire-and-forget, no retry).
func (f *Forwarder) drainInto(conn net.Conn) {
	w := bufio.NewWriter(conn)
	for {
		select {
		case <-f.done:
			return
		case msg := <-f.ch:
			payload, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			payload = append(payload, '\n')
			if _, err := w.Write(payload); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	}
}
