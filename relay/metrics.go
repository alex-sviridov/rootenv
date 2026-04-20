package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

// relayMetrics holds all Prometheus instruments for the relay service.
// A custom registry is used (not the default global) to prevent collisions
// in test environments where multiple registries might coexist.
type relayMetrics struct {
	activeConnections      prometheus.Gauge
	bytesIn                prometheus.Counter
	bytesOut               prometheus.Counter
	authFailures           *prometheus.CounterVec
	authzFailures          *prometheus.CounterVec
	connLimitRejections    prometheus.Counter
	wsAcceptErrors         prometheus.Counter
	backpressureDrops      prometheus.Counter
	connDuration           prometheus.Histogram
	connCloseReasons       *prometheus.CounterVec
	sshDialDuration        prometheus.Histogram
	sshDialErrors          prometheus.Counter
	sshShellsStarted       prometheus.Counter
}

// newRelayMetrics creates and registers all relay metrics on the given registry.
// Panics if registration fails (programmer error, not a runtime condition).
func newRelayMetrics(reg prometheus.Registerer) *relayMetrics {
	m := &relayMetrics{
		activeConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relay_active_connections",
			Help: "Current number of open WebSocket sessions.",
		}),
		bytesIn: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_bytes_in_total",
			Help: "Total bytes forwarded from WebSocket clients to SSH stdin.",
		}),
		bytesOut: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_bytes_out_total",
			Help: "Total bytes forwarded from SSH stdout to WebSocket clients.",
		}),
		authFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_auth_failures_total",
			Help: "Authentication failures, by reason.",
		}, []string{"reason"}),
		authzFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_authz_failures_total",
			Help: "Authorization failures, by reason.",
		}, []string{"reason"}),
		connLimitRejections: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_conn_limit_rejections_total",
			Help: "Number of connections rejected due to per-user connection limit.",
		}),
		wsAcceptErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_ws_accept_errors_total",
			Help: "Number of WebSocket upgrade failures.",
		}),
		backpressureDrops: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_backpressure_drops_total",
			Help: "Number of connections dropped because the client consumed output too slowly.",
		}),
		connDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "relay_conn_duration_seconds",
			Help:    "Duration of WebSocket sessions in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		connCloseReasons: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_conn_close_reasons_total",
			Help: "Number of connections closed, by reason.",
		}, []string{"reason"}),
		sshDialDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "relay_ssh_dial_duration_seconds",
			Help:    "Duration of SSH TCP+handshake dial in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		sshDialErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_ssh_dial_errors_total",
			Help: "Number of failed SSH dial attempts.",
		}),
		sshShellsStarted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relay_ssh_shells_started_total",
			Help: "Number of SSH shell sessions successfully started.",
		}),
	}

	reg.MustRegister(
		m.activeConnections,
		m.bytesIn,
		m.bytesOut,
		m.authFailures,
		m.authzFailures,
		m.connLimitRejections,
		m.wsAcceptErrors,
		m.backpressureDrops,
		m.connDuration,
		m.connCloseReasons,
		m.sshDialDuration,
		m.sshDialErrors,
		m.sshShellsStarted,
	)

	return m
}
