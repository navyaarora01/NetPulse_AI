package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type Metrics struct {
	Latency   float64 `json:"latency_ms"`
	Loss      float64 `json:"packet_loss_pct"`
	Bandwidth float64 `json:"bandwidth_mbps"`
	CPU       float64 `json:"cpu_usage_pct"`
	Memory    float64 `json:"memory_usage_pct"`
	Signal    float64 `json:"signal_strength_dbm"`
}

// UnmarshalJSON rejects missing measurements, rather than silently treating them as zero.
func (m *Metrics) UnmarshalJSON(b []byte) error {
	type plain Metrics
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for _, k := range []string{"latency_ms", "packet_loss_pct", "bandwidth_mbps", "cpu_usage_pct", "memory_usage_pct", "signal_strength_dbm"} {
		if v, ok := fields[k]; !ok || string(v) == "null" {
			return fmt.Errorf("metrics.%s is required", k)
		}
	}
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*m = Metrics(p)
	return nil
}

type Event struct {
	ID        string    `json:"event_id"`
	Version   int       `json:"schema_version"`
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Metrics   Metrics   `json:"metrics"`
}

func (e Event) Validate(now time.Time) error {
	if _, err := uuid.Parse(e.ID); err != nil {
		return errors.New("event_id must be a UUID")
	}
	if _, err := uuid.Parse(e.DeviceID); err != nil {
		return errors.New("device_id must be a registered UUID")
	}
	if e.Version != 1 {
		return errors.New("schema_version must be 1")
	}
	if e.Timestamp.IsZero() || e.Timestamp.After(now.Add(time.Minute)) || e.Timestamp.Before(now.Add(-24*time.Hour)) {
		return errors.New("timestamp must be within the last 24 hours and no more than 1 minute in the future")
	}
	values := []struct {
		name        string
		v, min, max float64
	}{{"latency_ms", e.Metrics.Latency, 0, 60000}, {"packet_loss_pct", e.Metrics.Loss, 0, 100}, {"bandwidth_mbps", e.Metrics.Bandwidth, 0, 1e7}, {"cpu_usage_pct", e.Metrics.CPU, 0, 100}, {"memory_usage_pct", e.Metrics.Memory, 0, 100}, {"signal_strength_dbm", e.Metrics.Signal, -200, 0}}
	for _, v := range values {
		if math.IsNaN(v.v) || math.IsInf(v.v, 0) || v.v < v.min || v.v > v.max {
			return fmt.Errorf("%s must be between %g and %g", v.name, v.min, v.max)
		}
	}
	return nil
}

type Device struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Location string     `json:"location"`
	Status   string     `json:"status"`
	Capacity float64    `json:"capacity_mbps"`
	Created  time.Time  `json:"created_at"`
	Updated  time.Time  `json:"updated_at"`
	LastSeen *time.Time `json:"last_seen_at"`
	Latest   *Metrics   `json:"latest"`
}
type Incident struct {
	ID             string          `json:"id"`
	DeviceID       string          `json:"device_id"`
	Type           string          `json:"type"`
	Severity       string          `json:"severity"`
	Status         string          `json:"status"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	FirstSeen      time.Time       `json:"first_seen_at"`
	LastSeen       time.Time       `json:"last_seen_at"`
	Count          int             `json:"occurrence_count"`
	Created        time.Time       `json:"created_at"`
	Updated        time.Time       `json:"updated_at"`
	Resolved       *time.Time      `json:"resolved_at"`
	AnalysisStatus string          `json:"analysis_status"`
	Analysis       json.RawMessage `json:"analysis"`
}
type Aggregate struct {
	Timestamp time.Time `json:"timestamp"`
	Samples   int       `json:"samples"`
	Metrics   Metrics   `json:"metrics"`
}

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("state conflict")

// UnmarshalJSON requires the metrics object even though a zero-valued Metrics
// struct can otherwise look like a legitimate all-zero measurement.
func (e *Event) UnmarshalJSON(b []byte) error {
	type plain Event
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	if v, ok := fields["metrics"]; !ok || string(v) == "null" {
		return errors.New("metrics object is required")
	}
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = Event(p)
	return nil
}
