package eventbroker

import (
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/metrics"
)

// recordEventPublished records an event publication metric
func recordEventPublished(event *Event) {
	if mp := metrics.GetProvider(); mp != nil {
		mp.RecordEventPublished(string(event.Source), event.Type)
	}
}

// recordEventProcessed records an event processing metric
func recordEventProcessed(event *Event, duration time.Duration) {
	if mp := metrics.GetProvider(); mp != nil {
		mp.RecordEventProcessed(string(event.Source), event.Type, string(event.Status), duration)
	}
}

// updateQueueSize updates the event queue size metric
func updateQueueSize(size int64) {
	if mp := metrics.GetProvider(); mp != nil {
		mp.UpdateEventQueueSize(size)
	}
}
