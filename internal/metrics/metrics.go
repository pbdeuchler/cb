package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all the metrics for the Claude Bot service
type Metrics struct {
	// Session metrics
	SessionsCreated   prometheus.Counter
	SessionsEnded     prometheus.Counter
	SessionDuration   prometheus.Histogram
	ActiveSessions    prometheus.Gauge

	// Command metrics
	CommandsProcessed *prometheus.CounterVec
	CommandDuration   *prometheus.HistogramVec

	// Error metrics
	ErrorsTotal *prometheus.CounterVec

	// Claude process metrics
	ClaudeProcesses prometheus.Gauge
	ClaudeErrors    prometheus.Counter

	// Repository metrics
	RepositoryOperations *prometheus.CounterVec
	RepositoryDuration   *prometheus.HistogramVec

	// Slack metrics
	SlackEvents    *prometheus.CounterVec
	SlackMessages  prometheus.Counter
	SlackErrors    prometheus.Counter

	// Database metrics
	DatabaseOperations *prometheus.CounterVec
	DatabaseDuration   *prometheus.HistogramVec
	DatabaseErrors     prometheus.Counter
}

// NewMetrics creates and registers all metrics
func NewMetrics() *Metrics {
	return &Metrics{
		// Session metrics
		SessionsCreated: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_sessions_created_total",
			Help: "Total number of Claude Code sessions created",
		}),
		SessionsEnded: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_sessions_ended_total",
			Help: "Total number of Claude Code sessions ended",
		}),
		SessionDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "cb_session_duration_seconds",
			Help:    "Duration of Claude Code sessions in seconds",
			Buckets: prometheus.ExponentialBuckets(60, 2, 10), // 1 min to ~17 hours
		}),
		ActiveSessions: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "cb_active_sessions",
			Help: "Number of currently active Claude Code sessions",
		}),

		// Command metrics
		CommandsProcessed: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cb_commands_processed_total",
			Help: "Total number of commands processed",
		}, []string{"command", "status"}),
		CommandDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cb_command_duration_seconds",
			Help:    "Duration of command processing in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"command"}),

		// Error metrics
		ErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cb_errors_total",
			Help: "Total number of errors by type and component",
		}, []string{"error_type", "component"}),

		// Claude process metrics
		ClaudeProcesses: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "cb_claude_processes",
			Help: "Number of running Claude Code processes",
		}),
		ClaudeErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_claude_errors_total",
			Help: "Total number of Claude process errors",
		}),

		// Repository metrics
		RepositoryOperations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cb_repository_operations_total",
			Help: "Total number of repository operations",
		}, []string{"operation", "status"}),
		RepositoryDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cb_repository_operation_duration_seconds",
			Help:    "Duration of repository operations in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),

		// Slack metrics
		SlackEvents: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cb_slack_events_total",
			Help: "Total number of Slack events received",
		}, []string{"event_type"}),
		SlackMessages: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_slack_messages_total",
			Help: "Total number of Slack messages sent",
		}),
		SlackErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_slack_errors_total",
			Help: "Total number of Slack API errors",
		}),

		// Database metrics
		DatabaseOperations: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "cb_database_operations_total",
			Help: "Total number of database operations",
		}, []string{"operation", "status"}),
		DatabaseDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cb_database_operation_duration_seconds",
			Help:    "Duration of database operations in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation"}),
		DatabaseErrors: promauto.NewCounter(prometheus.CounterOpts{
			Name: "cb_database_errors_total",
			Help: "Total number of database errors",
		}),
	}
}

// RecordSessionCreated records a session creation
func (m *Metrics) RecordSessionCreated() {
	m.SessionsCreated.Inc()
	m.ActiveSessions.Inc()
}

// RecordSessionEnded records a session ending with its duration
func (m *Metrics) RecordSessionEnded(duration time.Duration) {
	m.SessionsEnded.Inc()
	m.ActiveSessions.Dec()
	m.SessionDuration.Observe(duration.Seconds())
}

// RecordCommand records command processing
func (m *Metrics) RecordCommand(command, status string, duration time.Duration) {
	m.CommandsProcessed.WithLabelValues(command, status).Inc()
	m.CommandDuration.WithLabelValues(command).Observe(duration.Seconds())
}

// RecordError records an error by type and component
func (m *Metrics) RecordError(errorType, component string) {
	m.ErrorsTotal.WithLabelValues(errorType, component).Inc()
}

// RecordClaudeProcess records Claude process metrics
func (m *Metrics) RecordClaudeProcessStarted() {
	m.ClaudeProcesses.Inc()
}

func (m *Metrics) RecordClaudeProcessStopped() {
	m.ClaudeProcesses.Dec()
}

func (m *Metrics) RecordClaudeError() {
	m.ClaudeErrors.Inc()
}

// RecordRepositoryOperation records repository operations
func (m *Metrics) RecordRepositoryOperation(operation, status string, duration time.Duration) {
	m.RepositoryOperations.WithLabelValues(operation, status).Inc()
	m.RepositoryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordSlackEvent records Slack events
func (m *Metrics) RecordSlackEvent(eventType string) {
	m.SlackEvents.WithLabelValues(eventType).Inc()
}

func (m *Metrics) RecordSlackMessage() {
	m.SlackMessages.Inc()
}

func (m *Metrics) RecordSlackError() {
	m.SlackErrors.Inc()
}

// RecordDatabaseOperation records database operations
func (m *Metrics) RecordDatabaseOperation(operation, status string, duration time.Duration) {
	m.DatabaseOperations.WithLabelValues(operation, status).Inc()
	m.DatabaseDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

func (m *Metrics) RecordDatabaseError() {
	m.DatabaseErrors.Inc()
}

// Timer is a helper for measuring operation duration
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// Duration returns the elapsed time since the timer was created
func (t *Timer) Duration() time.Duration {
	return time.Since(t.start)
}

// ObserveSeconds observes the elapsed time in seconds for a histogram
func (t *Timer) ObserveSeconds(histogram prometheus.Histogram) {
	histogram.Observe(t.Duration().Seconds())
}