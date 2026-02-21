package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	EventsEnqueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ifttt_events_enqueued_total",
		Help: "Total number of events placed on the processing queue.",
	})

	EventsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ifttt_events_processed_total",
		Help: "Total number of events fully processed by the engine.",
	})

	EventsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ifttt_events_dropped_total",
		Help: "Total number of events rejected due to a full queue.",
	})

	ScenariosMatched = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ifttt_scenarios_matched_total",
		Help: "Total number of scenario matches, labelled by scenario ID.",
	}, []string{"scenario_id"})

	ActionsExecuted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ifttt_actions_executed_total",
		Help: "Total number of actions executed, labelled by type and status.",
	}, []string{"action_type", "status"})

	EventProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ifttt_event_processing_duration_ms",
		Help:    "End-to-end event processing latency in milliseconds.",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
	})

	QueueUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ifttt_queue_utilization_ratio",
		Help: "Current event queue utilization (0–1).",
	})
)
