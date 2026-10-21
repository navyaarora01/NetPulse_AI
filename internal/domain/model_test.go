package domain

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func valid() Event {
	return Event{ID: uuid.NewString(), DeviceID: uuid.NewString(), Version: 1, Timestamp: time.Now().UTC(), Metrics: Metrics{Latency: 40, Loss: .2, Bandwidth: 100, CPU: 40, Memory: 60, Signal: -65}}
}
func TestValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Event)
	}{{"id", func(e *Event) { e.ID = "no" }}, {"device", func(e *Event) { e.DeviceID = "router-001" }}, {"schema", func(e *Event) { e.Version = 2 }}, {"future", func(e *Event) { e.Timestamp = time.Now().Add(2 * time.Minute) }}, {"old", func(e *Event) { e.Timestamp = time.Now().Add(-25 * time.Hour) }}, {"percentage", func(e *Event) { e.Metrics.Loss = 101 }}, {"nan", func(e *Event) { e.Metrics.CPU = math.NaN() }}, {"infinite", func(e *Event) { e.Metrics.Latency = math.Inf(1) }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := valid()
			tc.change(&e)
			if e.Validate(time.Now()) == nil {
				t.Fatal("accepted invalid event")
			}
		})
	}
	if err := valid().Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestMissingMetrics(t *testing.T) {
	var m Metrics
	if json.Unmarshal([]byte(`{"latency_ms":1}`), &m) == nil {
		t.Fatal("missing fields accepted")
	}
}
func TestRoundTrip(t *testing.T) {
	e := valid()
	b, _ := json.Marshal(e)
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != e.ID || got.Metrics != e.Metrics {
		t.Fatal("round trip mismatch")
	}
}
