package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexsviridov/linuxlab/relay/grader"
	"github.com/alexsviridov/linuxlab/relay/pkg/relaybase"
)

type config struct {
	port           string
	namespace      string
	attemptID      string
	ownerID        string
	tasksFile      string
	skipAuth       bool
	allowedOrigins []string
	logLevel       slog.Level
}

func loadConfig() (config, bool) {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}

	skipAuth := os.Getenv("RELAY_SKIP_AUTH") == "true"

	attemptID := os.Getenv("RELAY_MY_ATTEMPT_ID")
	if attemptID == "" && !skipAuth {
		slog.Error("RELAY_MY_ATTEMPT_ID is required (set RELAY_SKIP_AUTH=true to skip)")
		return config{}, false
	}
	ownerID := os.Getenv("RELAY_MY_OWNER_ID")
	if ownerID == "" && !skipAuth {
		slog.Error("RELAY_MY_OWNER_ID is required (set RELAY_SKIP_AUTH=true to skip)")
		return config{}, false
	}
	namespace := os.Getenv("RELAY_MY_NAMESPACE")
	if namespace == "" {
		slog.Error("RELAY_MY_NAMESPACE is required")
		return config{}, false
	}
	tasksFile := os.Getenv("RELAY_TASKS_FILE")
	if tasksFile == "" {
		slog.Error("RELAY_TASKS_FILE is required")
		return config{}, false
	}

	port := os.Getenv("RELAY_LISTEN_PORT")
	if port == "" {
		port = "8080"
	}

	var origins []string
	if raw := os.Getenv("RELAY_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}

	return config{
		port:           port,
		namespace:      namespace,
		attemptID:      attemptID,
		ownerID:        ownerID,
		tasksFile:      tasksFile,
		skipAuth:       skipAuth,
		allowedOrigins: origins,
		logLevel:       level,
	}, true
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		r, err := http.Get("http://localhost:8080/healthz")
		if err != nil || r.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, ok := loadConfig()
	if !ok {
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.logLevel})))

	tasks, err := grader.LoadTasks(cfg.tasksFile)
	if err != nil {
		slog.Error("failed to load tasks", "err", err, "path", cfg.tasksFile)
		os.Exit(1)
	}

	backend := grader.Backend{
		Tasks: tasks,
		Log:   slog.Default().With("namespace", cfg.namespace),
	}
	limiter := relaybase.NewConnLimiter(16)

	var wg sync.WaitGroup
	handler := &relaybase.Handler{
		Backend:        &backend,
		Limiter:        limiter,
		AttemptID:      cfg.attemptID,
		OwnerID:        cfg.ownerID,
		SkipAuth:       cfg.skipAuth,
		AllowedOrigins: cfg.allowedOrigins,
		WG:             &wg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/relay/grade/{attemptID}/", handler)

	srv := &http.Server{
		Addr:    ":" + cfg.port,
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("relay-grader starting", "port", cfg.port, "skip_auth", cfg.skipAuth, "attempt_id", cfg.attemptID, "namespace", cfg.namespace, "tasks", len(tasks))
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("all sessions drained")
	case <-shutdownCtx.Done():
		slog.Warn("shutdown timeout: sessions still active")
	}
}
