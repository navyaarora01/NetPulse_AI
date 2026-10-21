// Package anomaly evaluates deterministic rules; it never consults an LLM.
package anomaly

import (
	"time"

	"netpulse/internal/config"
	"netpulse/internal/domain"
)

type Rule struct {
	Type, Title      string
	Value, Threshold float64
}

func Rules(m domain.Metrics, capacity float64, c config.Config) []Rule {
	r := []Rule{{"high_latency", "Sustained high latency", m.Latency, c.Latency}, {"packet_loss", "Elevated packet loss", m.Loss, c.Loss}, {"high_cpu", "High CPU usage", m.CPU, c.CPU}, {"high_memory", "High memory usage", m.Memory, c.Memory}}
	if capacity > 0 {
		r = append(r, Rule{"high_bandwidth", "High bandwidth utilization", 100 * m.Bandwidth / capacity, c.Bandwidth})
	}
	return r
}
func Severity(r Rule, c config.Config) string {
	if r.Value >= r.Threshold*c.SeverityCritical {
		return "critical"
	}
	if r.Value >= r.Threshold*c.SeverityHigh {
		return "high"
	}
	return "medium"
}

type Sample struct {
	At      time.Time
	Metrics domain.Metrics
}

// Evaluate requires the most recent N observations to breach within the rolling window.
func Evaluate(samples []Sample, capacity float64, c config.Config) []Rule {
	if len(samples) < c.MinSamples {
		return nil
	}
	last := samples[len(samples)-1]
	start := len(samples) - c.MinSamples
	if samples[start].At.Before(last.At.Add(-c.Window)) {
		return nil
	}
	result := []Rule{}
	for j, r := range Rules(last.Metrics, capacity, c) {
		breach := true
		for _, s := range samples[start:] {
			v := Rules(s.Metrics, capacity, c)[j]
			if v.Value <= v.Threshold {
				breach = false
				break
			}
		}
		if breach {
			result = append(result, r)
		}
	}
	return result
}
func Healthy(m domain.Metrics, capacity float64, c config.Config) bool {
	for _, r := range Rules(m, capacity, c) {
		if r.Value > r.Threshold {
			return false
		}
	}
	return true
}
