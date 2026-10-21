package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"netpulse/internal/app"
	"netpulse/internal/domain"
)

func main() { app.Fatal(run()) }
func run() error {
	base := flag.String("url", "http://localhost:8080", "API URL")
	count := flag.Int("devices", 10, "device count (1..200)")
	interval := flag.Duration("interval", 2*time.Second, "event interval")
	seed := flag.Int64("seed", 42, "random seed")
	scenario := flag.String("scenario", "normal", "normal, latency-spike, packet-loss, high-cpu, device-offline")
	target := flag.Int("target", 1, "one-based target device index")
	duration := flag.Duration("duration", 90*time.Second, "scenario duration after warmup")
	warmup := flag.Duration("warmup", 15*time.Second, "normal baseline before scenario")
	seedOnly := flag.Bool("seed-only", false, "register devices and exit")
	flag.Parse()
	if *count < 1 || *count > 200 || *target < 1 || *target > *count || *interval < 100*time.Millisecond || *duration <= 0 || *warmup < 0 {
		return errors.New("invalid simulator options")
	}
	switch *scenario {
	case "normal", "latency-spike", "packet-loss", "high-cpu", "device-offline":
	default:
		return errors.New("unknown scenario")
	}
	ctx, cancel := app.Context()
	defer cancel()
	client := &http.Client{Timeout: 25 * time.Second}
	call := func(method, path string, body any, out any) error {
		b, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, *base+path, bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if token := os.Getenv("API_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("API returned HTTP %d", resp.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
		}
		return nil
	}
	var existing struct {
		Items []domain.Device `json:"items"`
	}
	if err := call("GET", "/api/v1/devices?limit=200", nil, &existing); err != nil {
		return err
	}
	byName := map[string]domain.Device{}
	for _, d := range existing.Items {
		byName[d.Name] = d
	}
	devices := []domain.Device{}
	for i := 1; i <= *count; i++ {
		name := fmt.Sprintf("router-%03d", i)
		d, ok := byName[name]
		if !ok {
			if err := call("POST", "/api/v1/devices", map[string]any{"name": name, "location": []string{"Mumbai", "Singapore", "Frankfurt"}[(i-1)%3], "capacity_mbps": 1000}, &d); err != nil {
				return err
			}
		}
		devices = append(devices, d)
	}
	slog.Info("devices ready", "count", len(devices))
	if *seedOnly {
		return nil
	}
	rng := rand.New(rand.NewSource(*seed))
	start := time.Now()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	sent, failed := 0, 0
	defer func() { slog.Info("simulator stopped", "events_sent", sent, "errors", failed) }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			elapsed := time.Since(start)
			active := elapsed >= *warmup && elapsed < *warmup+*duration
			events := []domain.Event{}
			for i, d := range devices {
				if active && i == *target-1 && *scenario == "device-offline" {
					continue
				}
				m := domain.Metrics{Latency: 25 + rng.Float64()*20, Loss: rng.Float64() * .5, Bandwidth: 100 + rng.Float64()*50, CPU: 30 + rng.Float64()*20, Memory: 50 + rng.Float64()*10, Signal: -65 - rng.Float64()*5}
				if active && i == *target-1 {
					switch *scenario {
					case "latency-spike":
						m.Latency = 300 + rng.Float64()*80
					case "packet-loss":
						m.Loss = 10 + rng.Float64()*5
					case "high-cpu":
						m.CPU = 96 + rng.Float64()*4
					}
				}
				events = append(events, domain.Event{ID: uuid.NewString(), Version: 1, DeviceID: d.ID, Timestamp: time.Now().UTC(), Metrics: m})
			}
			for offset := 0; offset < len(events); offset += 100 {
				end := offset + 100
				if end > len(events) {
					end = len(events)
				}
				var err error
				for attempt := 0; attempt < 3; attempt++ {
					err = call("POST", "/api/v1/telemetry", events[offset:end], nil)
					if err == nil {
						break
					}
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(time.Duration(attempt+1) * time.Second):
					}
				}
				if err != nil {
					failed++
					slog.Warn("telemetry send failed", "error", err)
				} else {
					sent += end - offset
				}
			}
			if sent%100 == 0 {
				slog.Info("telemetry sent", "events", sent, "scenario_active", active)
			}
		}
	}
}
