package grader

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
)

type forwardMsg struct {
	Asset string `json:"asset"`
	Data  string `json:"data"`
}

// ServeInternalListener accepts connections from relay-exec forwarders on ln
// and feeds every well-formed NDJSON line into backend.Ingest. Malformed
// lines are logged and skipped — never close the connection or crash the
// listener over one bad message. Returns when ctx is cancelled.
func ServeInternalListener(ctx context.Context, ln net.Listener, backend *Backend, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go handleForwarderConn(conn, backend, log)
	}
}

func handleForwarderConn(conn net.Conn, backend *Backend, log *slog.Logger) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var msg forwardMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Debug("skipping malformed forwarder line", "err", err)
			continue
		}
		if msg.Asset == "" {
			continue
		}
		backend.Ingest(msg.Asset, []byte(msg.Data))
	}
}
