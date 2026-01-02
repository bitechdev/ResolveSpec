package dbmanager

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// connectionsTotal tracks the total number of configured database connections
	connectionsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connections_total",
			Help: "Total number of configured database connections",
		},
		[]string{"type"},
	)

	// connectionStatus tracks connection health status (1=healthy, 0=unhealthy)
	connectionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_status",
			Help: "Connection status (1=healthy, 0=unhealthy)",
		},
		[]string{"name", "type"},
	)

	// connectionPoolSize tracks connection pool sizes
	connectionPoolSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_pool_size",
			Help: "Current connection pool size",
		},
		[]string{"name", "type", "state"}, // state: open, idle, in_use
	)

	// connectionWaitCount tracks how many times connections had to wait for availability
	connectionWaitCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_wait_count",
			Help: "Number of times connections had to wait for availability",
		},
		[]string{"name", "type"},
	)

	// connectionWaitDuration tracks total time connections spent waiting
	connectionWaitDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_wait_duration_seconds",
			Help: "Total time connections spent waiting for availability",
		},
		[]string{"name", "type"},
	)

	// reconnectAttempts tracks reconnection attempts and their outcomes
	reconnectAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dbmanager_reconnect_attempts_total",
			Help: "Total number of reconnection attempts",
		},
		[]string{"name", "type", "result"}, // result: success, failure
	)

	// connectionLifetimeClosed tracks connections closed due to max lifetime
	connectionLifetimeClosed = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_lifetime_closed_total",
			Help: "Total connections closed due to exceeding max lifetime",
		},
		[]string{"name", "type"},
	)

	// connectionIdleClosed tracks connections closed due to max idle time
	connectionIdleClosed = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dbmanager_connection_idle_closed_total",
			Help: "Total connections closed due to exceeding max idle time",
		},
		[]string{"name", "type"},
	)
)

// PublishMetrics publishes current metrics for all connections
func (m *connectionManager) PublishMetrics() {
	stats := m.Stats()

	// Count connections by type
	typeCount := make(map[DatabaseType]int)
	for _, connStats := range stats.ConnectionStats {
		typeCount[connStats.Type]++
	}

	// Update total connections gauge
	for dbType, count := range typeCount {
		connectionsTotal.WithLabelValues(string(dbType)).Set(float64(count))
	}

	// Update per-connection metrics
	for name, connStats := range stats.ConnectionStats {
		labels := prometheus.Labels{
			"name": name,
			"type": string(connStats.Type),
		}

		// Connection status
		status := float64(0)
		if connStats.Connected && connStats.HealthCheckStatus == "healthy" {
			status = 1
		}
		connectionStatus.With(labels).Set(status)

		// Pool size metrics (SQL databases only)
		if connStats.Type != DatabaseTypeMongoDB {
			connectionPoolSize.WithLabelValues(name, string(connStats.Type), "open").Set(float64(connStats.OpenConnections))
			connectionPoolSize.WithLabelValues(name, string(connStats.Type), "idle").Set(float64(connStats.Idle))
			connectionPoolSize.WithLabelValues(name, string(connStats.Type), "in_use").Set(float64(connStats.InUse))

			// Wait stats
			connectionWaitCount.With(labels).Set(float64(connStats.WaitCount))
			connectionWaitDuration.With(labels).Set(connStats.WaitDuration.Seconds())

			// Lifetime/idle closed
			connectionLifetimeClosed.With(labels).Set(float64(connStats.MaxLifetimeClosed))
			connectionIdleClosed.With(labels).Set(float64(connStats.MaxIdleClosed))
		}
	}
}

// RecordReconnectAttempt records a reconnection attempt
func RecordReconnectAttempt(name string, dbType DatabaseType, success bool) {
	result := "failure"
	if success {
		result = "success"
	}

	reconnectAttempts.WithLabelValues(name, string(dbType), result).Inc()
}
