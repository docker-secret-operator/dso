package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Notification delivery metrics (internal/notify). The destination label is
// the notifier's SafeName (scheme://host only) -- never a full URL, which
// could embed credentials in its path or query.
var (
	NotificationAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_notification_attempts_total",
			Help: "Notification delivery attempts, by destination and event type",
		},
		[]string{"destination", "event_type"},
	)

	NotificationFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dso_notification_failures_total",
			Help: "Notification deliveries that exhausted retries or were rejected, by destination and event type",
		},
		[]string{"destination", "event_type"},
	)

	NotificationDroppedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dso_notification_dropped_total",
			Help: "Events dropped because the notification queue was full (delivery persistently slower than event production)",
		},
	)
)
