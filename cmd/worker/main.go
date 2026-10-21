package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"netpulse/internal/ai"
	"netpulse/internal/app"
	"netpulse/internal/cache"
	"netpulse/internal/domain"
	"netpulse/internal/messaging"
	"netpulse/internal/metrics"
)

type invalid struct{ error }

func main() { app.Fatal(run()) }
func run() error {
	ctx, cancel := app.Context()
	defer cancel()
	c, s, err := app.Setup(ctx)
	if err != nil {
		return err
	}
	defer s.Pool.Close()
	bus, err := messaging.New(c, c.TelemetryTopic, c.Group)
	if err != nil {
		return err
	}
	defer bus.Client.Close()
	cc := cache.New(c.RedisAddr, c.RedisPassword, c.RedisDB, c.CacheTTL)
	defer cc.Client.Close()
	var provider ai.Provider = ai.Mock{}
	if c.AIProvider == "openai-compatible" {
		provider = ai.New(c.AIURL, c.AIKey, c.AIModel, c.AITimeout)
	}
	var wg sync.WaitGroup
	launch := func(fn func()) { wg.Add(1); go func() { defer wg.Done(); fn() }() }
	launch(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				job, stop := context.WithTimeout(ctx, 30*time.Second)
				err := s.Dispatch(job, bus)
				stop()
				if err != nil && ctx.Err() == nil {
					slog.Error("outbox dispatch failed", "error", err)
				}
			}
		}
	})
	launch(func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				job, stop := context.WithTimeout(ctx, 30*time.Second)
				if err := s.Maintenance(job); err != nil {
					slog.Error("maintenance failed", "error", err)
				}
				if err := s.ReapAnalysis(job); err != nil {
					slog.Error("analysis lease cleanup failed", "error", err)
				}
				for _, severity := range []string{"low", "medium", "high", "critical"} {
					var n int
					if err := s.Pool.QueryRow(job, "SELECT count(*) FROM incidents WHERE severity=$1 AND status<>'resolved'", severity).Scan(&n); err == nil {
						metrics.Open.WithLabelValues(severity).Set(float64(n))
					}
				}
				stop()
			}
		}
	})
	// PostgreSQL is the durable analysis queue. Kafka analysis requests are an audit/
	// integration stream; periodic claiming also repairs a lost notification.
	if c.AIEnabled {
		launch(func() {
			for ctx.Err() == nil {
				id, generation, err := s.ClaimAnalysis(ctx)
				if err != nil {
					if !errors.Is(err, pgx.ErrNoRows) {
						slog.Error("analysis claim failed", "error", err)
					}
					_ = messaging.Sleep(ctx, time.Second)
					continue
				}
				i, err := s.Incident(ctx, id)
				var result ai.Analysis
				if err == nil {
					var buckets []domain.Aggregate
					buckets, err = s.Telemetry(ctx, i.DeviceID, i.FirstSeen.Add(-15*time.Minute), i.LastSeen.Add(time.Minute))
					if len(buckets) > 30 {
						buckets = buckets[len(buckets)-30:]
					}
					if err == nil {
						started := time.Now()
						callCtx, stop := context.WithTimeout(ctx, c.AITimeout)
						metrics.AICalls.Inc()
						result, err = provider.Analyze(callCtx, ai.Context{Type: i.Type, Severity: i.Severity, Telemetry: buckets})
						stop()
						metrics.AIDuration.Observe(time.Since(started).Seconds())
					}
				}
				var b []byte
				if err != nil {
					metrics.AIFailed.Inc()
					slog.Warn("analysis unavailable", "incident_id", id)
				} else {
					b, _ = json.Marshal(result)
				}
				if ctx.Err() != nil {
					return
				}
				if saveErr := s.FinishAnalysis(ctx, id, generation, b, err != nil); saveErr != nil {
					slog.Error("analysis persistence failed", "incident_id", id, "error", saveErr)
				}
				_ = messaging.Sleep(ctx, 2*time.Second)
			}
		})
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		check, stop := context.WithTimeout(r.Context(), 3*time.Second)
		defer stop()
		if s.Pool.Ping(check) != nil || bus.Client.Ping(check) != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	launch(func() {
		if err := app.Serve(ctx, ":8081", mux); err != nil {
			slog.Error("metrics server failed", "error", err)
			cancel()
		}
	})
	err = bus.Consume(ctx, c.DLQTopic, func(ctx context.Context, b []byte) error {
		ctx, stop := context.WithTimeout(ctx, 15*time.Second)
		defer stop()
		started := time.Now()
		defer func() { metrics.Duration.Observe(time.Since(started).Seconds()) }()
		var e domain.Event
		if err := json.Unmarshal(b, &e); err != nil {
			return invalid{err}
		}
		if err := e.Validate(time.Now()); err != nil {
			return invalid{err}
		}
		changed, err := s.Process(ctx, e)
		if errors.Is(err, domain.ErrNotFound) {
			return invalid{fmt.Errorf("unknown device")}
		}
		if err != nil {
			return err
		}
		if changed {
			metrics.Processed.Inc()
			if d, err := s.Device(ctx, e.DeviceID); err == nil {
				cc.Set(ctx, d)
			}
		}
		return nil
	}, func(err error) bool { var v invalid; return errors.As(err, &v) })
	cancel()
	wg.Wait()
	return err
}
