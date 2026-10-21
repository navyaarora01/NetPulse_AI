package metrics

import "github.com/prometheus/client_golang/prometheus"

var Received = counter("telemetry_events_received_total", "Accepted telemetry events")
var Processed = counter("telemetry_events_processed_total", "Persisted telemetry events")
var Failed = counter("telemetry_events_failed_total", "Failed processing attempts")
var Created = counter("incidents_created_total", "Incidents created")
var Resolved = counter("incidents_resolved_total", "Incidents resolved")
var AICalls = counter("ai_analysis_requests_total", "Analysis attempts")
var AIFailed = counter("ai_analysis_failures_total", "Analysis failures")
var PublishFailed = counter("kafka_publish_failures_total", "Kafka publish failures")
var DLQ = counter("dlq_events_total", "Messages sent to dead letter queues")
var Duration = histogram("telemetry_processing_duration_seconds", "Processing duration")
var AIDuration = histogram("ai_analysis_duration_seconds", "Provider duration")
var Cache = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "netpulse_cache_operations_total", Help: "Cache operations"}, []string{"result"})
var Open = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "netpulse_open_incidents", Help: "Active incidents"}, []string{"severity"})

func init() { prometheus.MustRegister(Cache, Open) }
func counter(n, h string) prometheus.Counter {
	v := prometheus.NewCounter(prometheus.CounterOpts{Name: "netpulse_" + n, Help: h})
	prometheus.MustRegister(v)
	return v
}
func histogram(n, h string) prometheus.Histogram {
	v := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "netpulse_" + n, Help: h})
	prometheus.MustRegister(v)
	return v
}
