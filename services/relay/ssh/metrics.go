package ssh

import (
	"github.com/prometheus/client_golang/prometheus"
)

// SSHMetrics holds all Prometheus instruments for the relay-ssh service.
// All metrics carry ConstLabels{"type":"ssh"} so future relay types can
// register the same metric names without collision.
type SSHMetrics struct {
	activeConnections   prometheus.Gauge
	bytesIn             prometheus.Counter
	bytesOut            prometheus.Counter
	authFailures        *prometheus.CounterVec
	authzFailures       *prometheus.CounterVec
	connLimitRejections prometheus.Counter
	wsAcceptErrors      prometheus.Counter
	backpressureDrops   prometheus.Counter
	connDuration        prometheus.Histogram
	connCloseReasons    *prometheus.CounterVec
	sshDialDuration     prometheus.Histogram
	sshDialErrors       prometheus.Counter
	sshShellsStarted    prometheus.Counter
}

// NewSSHMetrics creates and registers all relay-ssh metrics on the given registry.
// Panics if registration fails (programmer error, not a runtime condition).
func NewSSHMetrics(reg prometheus.Registerer) *SSHMetrics {
	labels := prometheus.Labels{"type": "ssh"}
	m := &SSHMetrics{
		activeConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "relay_active_connections",
			Help:        "Current number of open WebSocket sessions.",
			ConstLabels: labels,
		}),
		bytesIn: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_bytes_in_total",
			Help:        "Total bytes forwarded from WebSocket clients to SSH stdin.",
			ConstLabels: labels,
		}),
		bytesOut: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_bytes_out_total",
			Help:        "Total bytes forwarded from SSH stdout to WebSocket clients.",
			ConstLabels: labels,
		}),
		authFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "relay_auth_failures_total",
			Help:        "Authentication failures, by reason.",
			ConstLabels: labels,
		}, []string{"reason"}),
		authzFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "relay_authz_failures_total",
			Help:        "Authorization failures, by reason.",
			ConstLabels: labels,
		}, []string{"reason"}),
		connLimitRejections: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_conn_limit_rejections_total",
			Help:        "Number of connections rejected due to per-user connection limit.",
			ConstLabels: labels,
		}),
		wsAcceptErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_ws_accept_errors_total",
			Help:        "Number of WebSocket upgrade failures.",
			ConstLabels: labels,
		}),
		backpressureDrops: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_backpressure_drops_total",
			Help:        "Number of connections dropped because the client consumed output too slowly.",
			ConstLabels: labels,
		}),
		connDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "relay_conn_duration_seconds",
			Help:        "Duration of WebSocket sessions in seconds.",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: labels,
		}),
		connCloseReasons: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "relay_conn_close_reasons_total",
			Help:        "Number of connections closed, by reason.",
			ConstLabels: labels,
		}, []string{"reason"}),
		sshDialDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "relay_ssh_dial_duration_seconds",
			Help:        "Duration of SSH TCP+handshake dial in seconds.",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: labels,
		}),
		sshDialErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_ssh_dial_errors_total",
			Help:        "Number of failed SSH dial attempts.",
			ConstLabels: labels,
		}),
		sshShellsStarted: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "relay_ssh_shells_started_total",
			Help:        "Number of SSH shell sessions successfully started.",
			ConstLabels: labels,
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

// RecordAuthFailure implements relaybase.AuthMetrics.
func (m *SSHMetrics) RecordAuthFailure(reason string) {
	m.authFailures.WithLabelValues(reason).Inc()
}

// RecordAuthzFailure implements relaybase.AuthMetrics.
func (m *SSHMetrics) RecordAuthzFailure(reason string) {
	m.authzFailures.WithLabelValues(reason).Inc()
}

// RecordConnLimitRejection implements relaybase.AuthMetrics.
func (m *SSHMetrics) RecordConnLimitRejection() {
	m.connLimitRejections.Inc()
}

// RecordWSAcceptError implements relaybase.AuthMetrics.
func (m *SSHMetrics) RecordWSAcceptError() {
	m.wsAcceptErrors.Inc()
}
