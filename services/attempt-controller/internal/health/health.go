package health

import (
	"net/http"
	"sync/atomic"
)

// Server exposes /healthz (liveness) and /readyz (readiness) over HTTP.
// Call SetReady(true) once the controller has successfully connected to the backend.
type Server struct {
	ready atomic.Bool
}

func (s *Server) SetReady(v bool) { s.ready.Store(v) }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.ready.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
		}
	})
	return mux
}
