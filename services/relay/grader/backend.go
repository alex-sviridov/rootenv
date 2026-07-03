package grader

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/coder/websocket"
)

// Backend implements relaybase.Backend for the grader relay type.
// It reports the current grade (always false in this bootstrap) for every
// loaded task, then holds the connection open until the client disconnects.
type Backend struct {
	Tasks []Task
	Log   *slog.Logger // defaults to slog.Default() if nil
}

func (b *Backend) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// Serve sends the initial grade report, then idles until the connection closes.
// assetName is unused — grading is attempt-scoped, not asset-scoped.
func (b *Backend) Serve(ctx context.Context, conn *websocket.Conn, assetName, userID string) error {
	log := b.logger().With("user_id", userID)

	grades := make(map[string]bool, len(b.Tasks))
	for _, task := range b.Tasks {
		grades[task.ID] = false
	}

	payload, err := json.Marshal(grades)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		log.Error("failed to send grade report", "err", err)
		return err
	}

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return nil
		}
	}
}
