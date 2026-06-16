package pocketbase

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type realtimeEvent struct {
	Action string        `json:"action"`
	Record AttemptRecord `json:"record"`
}

// RunAttemptSubscription calls SubscribeAttempts in a loop, reconnecting after
// backoff whenever the connection fails, until ctx is cancelled. onConnect is
// invoked after every successful (re)connection.
func (c *Client) RunAttemptSubscription(ctx context.Context, handler func(action string, record AttemptRecord), onConnect func(ctx context.Context), backoff time.Duration) {
	for {
		err := c.SubscribeAttempts(ctx, handler, onConnect)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("realtime subscription error: %v; reconnecting in %s", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// SubscribeAttempts opens a PocketBase realtime SSE connection and subscribes to
// changes on the attempts collection, invoking handler for every create/update/delete
// event until ctx is cancelled. onConnect is invoked once the subscription is
// established.
func (c *Client) SubscribeAttempts(ctx context.Context, handler func(action string, record AttemptRecord), onConnect func(ctx context.Context)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/realtime", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/realtime: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		event, data, ok := readSSEEvent(scanner)
		if !ok {
			return scanner.Err()
		}

		switch event {
		case "PB_CONNECT":
			var connect struct {
				ClientID string `json:"clientId"`
			}
			if err := json.Unmarshal(data, &connect); err != nil {
				return fmt.Errorf("parse PB_CONNECT: %w", err)
			}
			if err := c.subscribeRealtime(ctx, connect.ClientID, "attempts/*?expand=lab"); err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}
			onConnect(ctx)
		default:
			if !strings.HasPrefix(event, "attempts/") {
				continue
			}
			var ev realtimeEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				return fmt.Errorf("parse event: %w", err)
			}
			handler(ev.Action, ev.Record)
		}
	}
}

func (c *Client) subscribeRealtime(ctx context.Context, clientID string, subscriptions ...string) error {
	resp, err := c.doSubscribeRealtime(ctx, clientID, subscriptions)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		if err := c.reauth(); err != nil {
			return fmt.Errorf("POST /api/realtime: reauth: %w", err)
		}
		resp, err = c.doSubscribeRealtime(ctx, clientID, subscriptions)
		if err != nil {
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("POST /api/realtime: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) doSubscribeRealtime(ctx context.Context, clientID string, subscriptions []string) (*http.Response, error) {
	body, err := json.Marshal(map[string]any{
		"clientId":      clientID,
		"subscriptions": subscriptions,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/realtime", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.currentToken())

	return c.httpClient.Do(req)
}

// readSSEEvent reads a single SSE frame (lines until a blank line) and returns
// its event name and data payload. ok is false when the stream ended.
func readSSEEvent(scanner *bufio.Scanner) (event string, data []byte, ok bool) {
	var dataLines []string
	sawAny := false
	for scanner.Scan() {
		line := scanner.Text()
		sawAny = true
		switch {
		case line == "":
			if event == "" && len(dataLines) == 0 {
				continue
			}
			return event, []byte(strings.Join(dataLines, "\n")), true
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if sawAny && (event != "" || len(dataLines) > 0) {
		return event, []byte(strings.Join(dataLines, "\n")), true
	}
	return "", nil, false
}
