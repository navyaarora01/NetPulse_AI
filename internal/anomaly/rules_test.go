package anomaly

import (
	"testing"
	"time"

	"netpulse/internal/config"
	"netpulse/internal/domain"
)

func TestThresholdAndWindow(t *testing.T) {
	c := config.Config{Latency: 150, Loss: 5, CPU: 90, Memory: 90, Bandwidth: 90, MinSamples: 3, Window: time.Minute}
	now := time.Now()
	sample := func(v float64, at time.Time) Sample { return Sample{at, domain.Metrics{Latency: v}} }
	tests := []struct {
		name string
		s    []Sample
		want int
	}{{"insufficient", []Sample{sample(200, now)}, 0}, {"boundary", []Sample{sample(150, now), sample(150, now), sample(150, now)}, 0}, {"spike", []Sample{sample(40, now), sample(40, now), sample(200, now)}, 0}, {"sustained", []Sample{sample(200, now.Add(-4*time.Second)), sample(200, now.Add(-2*time.Second)), sample(200, now)}, 1}, {"expired", []Sample{sample(200, now.Add(-2*time.Minute)), sample(200, now), sample(200, now)}, 0}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(Evaluate(tc.s, 1000, c)); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
func TestCapacityAndSeverity(t *testing.T) {
	c := config.Config{Latency: 150, Loss: 5, CPU: 90, Memory: 90, Bandwidth: 90}
	m := domain.Metrics{Bandwidth: 950}
	if !Healthy(m, 0, c) {
		t.Fatal("unknown capacity must not trigger")
	}
	if Healthy(m, 1000, c) {
		t.Fatal("configured capacity ignored")
	}
	if Severity(Rule{Value: 300, Threshold: 150}, config.Config{SeverityHigh: 1.25, SeverityCritical: 2}) != "critical" {
		t.Fatal("severity boundary")
	}
}
