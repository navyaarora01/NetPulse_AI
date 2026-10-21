package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"netpulse/internal/config"
	"netpulse/internal/domain"
)

type fakeRepo struct {
	Repository
	d domain.Device
}

func (f fakeRepo) Device(context.Context, string) (domain.Device, error) { return f.d, nil }
func (f fakeRepo) Devices(context.Context, string, int, int) ([]domain.Device, error) {
	return []domain.Device{f.d}, nil
}

type fakeBus struct {
	calls int
	err   error
}

func (b *fakeBus) Publish(context.Context, string, string, []byte) error { b.calls++; return b.err }
func TestIngestion(t *testing.T) {
	b := &fakeBus{}
	a := API{Store: fakeRepo{d: domain.Device{ID: uuid.NewString()}}, Bus: b, Config: config.Config{TelemetryTopic: "test"}}
	e := domain.Event{ID: uuid.NewString(), DeviceID: uuid.NewString(), Version: 1, Timestamp: time.Now(), Metrics: domain.Metrics{Signal: -65}}
	raw, _ := json.Marshal(e)
	for _, tc := range []struct {
		body   string
		status int
	}{{string(raw), 202}, {`{}`, 400}, {`[]`, 400}, {`[null]`, 400}, {`not-json`, 400}, {strings.Repeat("x", 1<<20+1), 413}} {
		r := httptest.NewRequest("POST", "/api/v1/telemetry", strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
		if w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
	}
	if b.calls != 1 {
		t.Fatalf("published invalid requests: %d", b.calls)
	}
	b.err = errors.New("broker offline")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/telemetry", strings.NewReader(string(raw))))
	if w.Code != 503 || strings.Contains(w.Body.String(), "broker offline") {
		t.Fatal("publish failure not sanitized")
	}
}
func TestMiddlewareAndQueries(t *testing.T) {
	a := API{Store: fakeRepo{}, Config: config.Config{Token: "secret", Origin: "https://dashboard.example"}}
	h := a.Handler()
	for _, tc := range []struct {
		url, origin, token string
		status             int
	}{{"/healthz", "", "", 200}, {"/api/v1/devices", "", "", 401}, {"/api/v1/devices?limit=-1", "", "secret", 400}, {"/api/v1/devices?status=unknown", "", "secret", 400}, {"/api/v1/devices", "https://evil.example", "secret", 403}, {"/api/v1/devices", "https://dashboard.example", "secret", 200}, {"/api/v1/devices/not-a-uuid", "", "secret", 400}} {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s got %d want %d", tc.url, w.Code, tc.status)
		}
	}
}
func TestBoundedRanges(t *testing.T) {
	for _, path := range []string{"/?from=invalid", "/?from=2026-01-01T00:00:00Z&to=2026-01-03T00:00:00Z", "/?offset=999999999999999999999"} {
		r := httptest.NewRequest("GET", path, nil)
		if strings.Contains(path, "offset") {
			if _, _, err := page(r); err == nil {
				t.Fatal("overflow accepted")
			}
		} else if _, _, err := times(r); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}
