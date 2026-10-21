//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/twmb/franz-go/pkg/kgo"

	"netpulse/internal/ai"
	"netpulse/internal/cache"
	"netpulse/internal/config"
	"netpulse/internal/database"
	"netpulse/internal/domain"
	"netpulse/internal/httpapi"
	"netpulse/internal/messaging"
)

func setup(t *testing.T) (context.Context, *database.Store, config.Config) {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.DatabaseURL, "netpulse_test") {
		t.Fatal("integration tests require a separate netpulse_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	s, err := database.Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Pool.Close)
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c.MinSamples = 3
	c.Recovery = 4 * time.Second
	c.Cooldown = time.Minute
	c.AIEnabled = true
	c.AIProvider = "mock"
	s.Config = c
	return ctx, s, c
}
func device(t *testing.T, ctx context.Context, s *database.Store) domain.Device {
	t.Helper()
	d, err := s.CreateDevice(ctx, domain.Device{Name: "integration-" + uuid.NewString(), Capacity: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func event(d domain.Device, at time.Time, latency float64) domain.Event {
	return domain.Event{ID: uuid.NewString(), Version: 1, DeviceID: d.ID, Timestamp: at, Metrics: domain.Metrics{Latency: latency, CPU: 30, Memory: 40, Signal: -65}}
}
func TestTransactionalLifecycle(t *testing.T) {
	ctx, s, c := setup(t)
	d := device(t, ctx, s)
	at := time.Now().UTC().Add(-30 * time.Second)
	for i := 0; i < 4; i++ {
		e := event(d, at.Add(time.Duration(i)*time.Second), 300)
		changed, err := s.Process(ctx, e)
		if err != nil || !changed {
			t.Fatalf("process: %v", err)
		}
		changed, err = s.Process(ctx, e)
		if err != nil || changed {
			t.Fatalf("duplicate: %v changed %v", err, changed)
		}
	}
	list, err := s.Incidents(ctx, database.Filter{Device: d.ID, From: at.Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 20})
	if err != nil || len(list) != 1 {
		t.Fatalf("dedup: %v %+v", err, list)
	}
	i := list[0]
	if i.Count != 2 {
		t.Fatalf("count = %d", i.Count)
	}
	if err = s.Transition(ctx, i.ID, "acknowledged"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Incident(ctx, i.ID)
	if got.Status != "acknowledged" {
		t.Fatal("acknowledge failed")
	}
	for _, seconds := range []int{5, 7, 10} {
		if _, err = s.Process(ctx, event(d, at.Add(time.Duration(seconds)*time.Second), 20)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = s.Incident(ctx, i.ID)
	if got.Status != "resolved" {
		t.Fatalf("recovery failed: %s", got.Status)
	}
	if !errors.Is(s.Transition(ctx, i.ID, "acknowledged"), domain.ErrConflict) {
		t.Fatal("reopened resolved incident")
	}
	// Cooldown suppresses a new incident even when the breach is sustained.
	for seconds := 11; seconds < 15; seconds++ {
		if _, err = s.Process(ctx, event(d, at.Add(time.Duration(seconds)*time.Second), 300)); err != nil {
			t.Fatal(err)
		}
	}
	list, err = s.Incidents(ctx, database.Filter{Device: d.ID, From: at.Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 20})
	if err != nil || len(list) != 1 {
		t.Fatal("cooldown failed", err)
	}
	latest, _ := s.Device(ctx, d.ID)
	old := event(d, at.Add(-time.Hour), 500)
	if _, err = s.Process(ctx, old); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Device(ctx, d.ID)
	if !after.LastSeen.Equal(*latest.LastSeen) {
		t.Fatal("historical event overwrote latest state")
	}
	cc := cache.New(c.RedisAddr, c.RedisPassword, c.RedisDB, 80*time.Millisecond)
	defer cc.Client.Close()
	if _, hit := cc.Get(ctx, d.ID); hit {
		t.Fatal("expected initial miss")
	}
	cc.Set(ctx, after)
	if _, hit := cc.Get(ctx, d.ID); !hit {
		t.Fatal("expected hit")
	}
	time.Sleep(100 * time.Millisecond)
	if _, hit := cc.Get(ctx, d.ID); hit {
		t.Fatal("expected expiration")
	}
	offline := cache.New("127.0.0.1:1", "", 0, time.Second)
	defer offline.Client.Close()
	if got, err := offline.Device(ctx, d.ID, s.Device); err != nil || got.ID != d.ID {
		t.Fatal("database fallback failed", err)
	}
	buckets, err := s.Telemetry(ctx, d.ID, at.Add(-time.Hour), time.Now())
	if err != nil || len(buckets) == 0 {
		t.Fatal("aggregation missing", err)
	}
	analysis, _ := ai.Mock{}.Analyze(ctx, ai.Context{Type: i.Type, Severity: i.Severity, Telemetry: buckets})
	raw, _ := json.Marshal(analysis)
	if _, err = ai.Parse(raw); err != nil {
		t.Fatal(err)
	}
	if err = s.RequestAnalysis(ctx, i.ID); err != nil {
		t.Fatal(err)
	}
	var requests int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM outbox WHERE topic=$1 AND key=$2", c.AnalysisTopic, i.ID).Scan(&requests); err != nil || requests != 1 {
		t.Fatal("analysis dedup failed", requests, err)
	}
}
func TestAPIAndKafka(t *testing.T) {
	ctx, s, c := setup(t)
	d := device(t, ctx, s)
	bus, err := messaging.New(c, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Client.Close()
	a := httpapi.API{Store: s, Bus: bus, Config: c}
	e := event(d, time.Now().UTC(), 40)
	raw, _ := json.Marshal(e)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/telemetry", strings.NewReader(string(raw))))
	if w.Code != 202 {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}
	consumer, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(c.Brokers, ",")...), kgo.ConsumeTopics(c.TelemetryTopic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	found := false
	for !found && ctx.Err() == nil {
		fetch := consumer.PollFetches(ctx)
		if errs := fetch.Errors(); len(errs) > 0 {
			t.Fatal(errs)
		}
		for _, r := range fetch.Records() {
			var got domain.Event
			if json.Unmarshal(r.Value, &got) == nil && got.ID == e.ID {
				if string(r.Key) != d.ID {
					t.Fatal("incorrect partition key")
				}
				if _, err = s.Process(ctx, got); err != nil {
					t.Fatal(err)
				}
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("event not consumed")
	}
	got, err := s.Device(ctx, d.ID)
	if err != nil || got.Latest == nil {
		t.Fatal("telemetry did not persist", err)
	}
	// API registration is persisted through its normal handler.
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/devices", strings.NewReader(`{"name":"api-integration","capacity_mbps":1000}`)))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
}
func TestDLQ(t *testing.T) {
	ctx, _, c := setup(t)
	consumer, err := messaging.New(c, c.TelemetryTopic, "integration-dlq-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Client.Close()
	producer, err := messaging.New(c, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Client.Close()
	key := uuid.NewString()
	payload := []byte(`{"poison":"` + key + `"}`)
	if err = producer.Publish(ctx, c.TelemetryTopic, key, payload); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- consumer.Consume(runCtx, c.DLQTopic, func(_ context.Context, b []byte) error {
			if strings.Contains(string(b), key) {
				return fmt.Errorf("poison")
			}
			return nil
		}, func(error) bool { return true })
	}()
	defer func() { cancel(); <-done }()
	reader, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(c.Brokers, ",")...), kgo.ConsumeTopics(c.DLQTopic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for ctx.Err() == nil {
		fetch := reader.PollFetches(ctx)
		for _, r := range fetch.Records() {
			if string(r.Key) == key {
				var envelope map[string]any
				if json.Unmarshal(r.Value, &envelope) != nil || envelope["reason"] == nil || envelope["original"] == nil {
					t.Fatal("invalid DLQ envelope")
				}
				return
			}
		}
	}
	t.Fatal("DLQ message missing")
}
func TestUnknownDeviceRollsBack(t *testing.T) {
	ctx, s, _ := setup(t)
	_, err := s.Process(ctx, event(domain.Device{ID: uuid.NewString()}, time.Now(), 300))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	var one int
	if err = s.Pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil && err != pgx.ErrNoRows {
		t.Fatal(err)
	}
}
