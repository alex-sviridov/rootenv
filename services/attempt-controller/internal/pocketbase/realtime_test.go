package pocketbase

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadSSEEventTrimsDataLeadingSpace(t *testing.T) {
	input := "event:attempts/a1\ndata: {\"action\":\"update\"}\n\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	event, data, ok := readSSEEvent(scanner)
	if !ok {
		t.Fatal("ok = false")
	}
	if event != "attempts/a1" {
		t.Errorf("event = %q", event)
	}
	want := `{"action":"update"}`
	if string(data) != want {
		t.Errorf("data = %q, want %q", string(data), want)
	}
}

func TestSubscribeAttempts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})

	subscribed := make(chan struct{}, 1)
	mux.HandleFunc("/api/realtime", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			_, _ = fmt.Fprintf(w,"id:0\nevent:PB_CONNECT\ndata:%s\n\n", `{"clientId":"client123"}`)
			flusher.Flush()

			select {
			case <-subscribed:
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Second):
				return
			}

			rec := AttemptRecord{ID: "a1", UserId: "u1", Lab: "rhcsa-lab1", CurrentState: "provisioning", DesiredState: "provisioned"}
			data, _ := json.Marshal(map[string]any{"action": "update", "record": rec})
			_, _ = fmt.Fprintf(w,"id:1\nevent:attempts/a1\ndata:%s\n\n", data)
			flusher.Flush()

			<-r.Context().Done()
		case http.MethodPost:
			if r.Header.Get("Authorization") != "tok123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body struct {
				ClientID      string   `json:"clientId"`
				Subscriptions []string `json:"subscriptions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body.ClientID != "client123" {
				t.Errorf("clientId = %q", body.ClientID)
			}
			if len(body.Subscriptions) != 1 || body.Subscriptions[0] != "attempts/*?expand=lab" {
				t.Errorf("subscriptions = %v", body.Subscriptions)
			}
			w.WriteHeader(http.StatusNoContent)
			select {
			case subscribed <- struct{}{}:
			default:
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan struct {
		action string
		record AttemptRecord
	}, 1)

	go func() {
		_ = c.SubscribeAttempts(ctx, func(action string, rec AttemptRecord) {
			events <- struct {
				action string
				record AttemptRecord
			}{action, rec}
		}, func(context.Context) {})
	}()

	select {
	case ev := <-events:
		if ev.action != "update" {
			t.Errorf("action = %q", ev.action)
		}
		if ev.record.ID != "a1" || ev.record.CurrentState != "provisioning" {
			t.Errorf("record = %+v", ev.record)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeAttemptsCallsOnConnect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})
	mux.HandleFunc("/api/realtime", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w,"id:0\nevent:PB_CONNECT\ndata:%s\n\n", `{"clientId":"client123"}`)
			flusher.Flush()
			<-r.Context().Done()
		case http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	onConnectCalled := make(chan struct{}, 1)

	go func() {
		_ = c.SubscribeAttempts(ctx, func(string, AttemptRecord) {}, func(context.Context) {
			onConnectCalled <- struct{}{}
		})
	}()

	select {
	case <-onConnectCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for onConnect")
	}
}

func TestSubscribeRealtimeReauthsOn401(t *testing.T) {
	mux := http.NewServeMux()
	var authCalls int
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		token := fmt.Sprintf("tok%d", authCalls)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token})
	})

	var postCalls int
	mux.HandleFunc("/api/realtime", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w,"id:0\nevent:PB_CONNECT\ndata:%s\n\n", `{"clientId":"client123"}`)
			flusher.Flush()
			<-r.Context().Done()
		case http.MethodPost:
			postCalls++
			if r.Header.Get("Authorization") != "tok2" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		_ = c.SubscribeAttempts(ctx, func(string, AttemptRecord) {}, func(context.Context) {})
	}()

	deadline := time.After(2 * time.Second)
	for {
		if postCalls == 2 && c.currentToken() == "tok2" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("postCalls = %d, token = %q", postCalls, c.currentToken())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRunAttemptSubscriptionReconnectsAfterFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/collections/users/auth-with-password", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok123"})
	})

	var attempts int
	mux.HandleFunc("/api/realtime", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			attempts++
			if attempts == 1 {
				// First connection: send PB_CONNECT then close immediately to simulate failure.
				flusher := w.(http.Flusher)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprintf(w,"id:0\nevent:PB_CONNECT\ndata:%s\n\n", `{"clientId":"client1"}`)
				flusher.Flush()
				return
			}
			// Second connection: send PB_CONNECT then an event.
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w,"id:0\nevent:PB_CONNECT\ndata:%s\n\n", `{"clientId":"client2"}`)
			flusher.Flush()

			rec := AttemptRecord{ID: "a1", CurrentState: "provisioning"}
			data, _ := json.Marshal(map[string]any{"action": "update", "record": rec})
			_, _ = fmt.Fprintf(w,"id:1\nevent:attempts/a1\ndata:%s\n\n", data)
			flusher.Flush()

			<-r.Context().Done()
		case http.MethodPost:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c, err := NewClient(ts.URL, "svc@x.local", "pass", true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan struct {
		action string
		record AttemptRecord
	}, 1)

	var onConnectCalls int32

	go c.RunAttemptSubscription(ctx, func(action string, rec AttemptRecord) {
		events <- struct {
			action string
			record AttemptRecord
		}{action, rec}
	}, func(context.Context) {
		atomic.AddInt32(&onConnectCalls, 1)
	}, 10*time.Millisecond)

	select {
	case ev := <-events:
		if ev.action != "update" || ev.record.ID != "a1" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for event after reconnect")
	}

	if got := atomic.LoadInt32(&onConnectCalls); got != 2 {
		t.Errorf("onConnectCalls = %d, want 2 (initial connect + reconnect)", got)
	}
}
