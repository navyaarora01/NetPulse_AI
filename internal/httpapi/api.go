package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"netpulse/internal/config"
	"netpulse/internal/database"
	"netpulse/internal/domain"
	"netpulse/internal/metrics"
)

type Repository interface {
	Summary(context.Context) (database.Summary, error)
	CreateDevice(context.Context, domain.Device) (domain.Device, error)
	Device(context.Context, string) (domain.Device, error)
	Devices(context.Context, string, int, int) ([]domain.Device, error)
	Telemetry(context.Context, string, time.Time, time.Time) ([]domain.Aggregate, error)
	Incident(context.Context, string) (domain.Incident, error)
	Incidents(context.Context, database.Filter) ([]domain.Incident, error)
	Transition(context.Context, string, string) error
	RequestAnalysis(context.Context, string) error
}
type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}
type DeviceCache interface {
	Device(context.Context, string, func(context.Context, string) (domain.Device, error)) (domain.Device, error)
}
type API struct {
	Store  Repository
	Bus    Publisher
	Cache  DeviceCache
	Config config.Config
	Ready  func(context.Context) error
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if a.Ready != nil && a.Ready(ctx) != nil {
			failure(w, 503, "not_ready", "Required dependencies are unavailable")
			return
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/v1/summary", func(w http.ResponseWriter, r *http.Request) {
		v, err := a.Store.Summary(r.Context())
		respond(w, v, err)
	})
	mux.HandleFunc("GET /api/v1/devices", a.devices)
	mux.HandleFunc("POST /api/v1/devices", a.createDevice)
	mux.HandleFunc("GET /api/v1/devices/{id}", a.device)
	mux.HandleFunc("GET /api/v1/devices/{id}/telemetry", a.telemetry)
	mux.HandleFunc("POST /api/v1/telemetry", a.ingest)
	mux.HandleFunc("GET /api/v1/incidents", a.incidents)
	mux.HandleFunc("GET /api/v1/incidents/{id}", a.incident)
	mux.HandleFunc("POST /api/v1/incidents/{id}/{action}", a.action)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { failure(w, 404, "not_found", "Endpoint not found") })
	var mu sync.Mutex
	tokens := 200.0
	last := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		start := time.Now()
		defer func() {
			if v := recover(); v != nil {
				slog.Error("request panic", "request_id", id)
				failure(w, 500, "internal_error", "Request failed")
			}
			slog.Info("request", "request_id", id, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
		}()
		origin := r.Header.Get("Origin")
		if origin != "" {
			if origin != a.Config.Origin {
				failure(w, 403, "origin_denied", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && a.Config.Token != "" {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(a.Config.Token)) != 1 {
				failure(w, 401, "unauthorized", "A valid bearer token is required")
				return
			}
		}
		if r.Method == "POST" {
			mu.Lock()
			now := time.Now()
			tokens = math.Min(200, tokens+now.Sub(last).Seconds()*100)
			last = now
			allowed := tokens >= 1
			if allowed {
				tokens--
			}
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", "1")
				failure(w, 429, "rate_limited", "Write rate exceeded")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		mux.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": w.Header().Get("X-Request-ID")}})
}
func respond(w http.ResponseWriter, v any, err error) {
	if err == nil {
		write(w, 200, v)
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		failure(w, 404, "not_found", "Resource not found")
	} else if errors.Is(err, domain.ErrConflict) {
		failure(w, 409, "conflict", "A resolved incident cannot be acknowledged")
	} else {
		slog.Error("repository operation failed", "error", err)
		failure(w, 503, "unavailable", "Service temporarily unavailable")
	}
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}
func validID(w http.ResponseWriter, r *http.Request) bool {
	if _, err := uuid.Parse(r.PathValue("id")); err != nil {
		failure(w, 400, "invalid_id", "ID must be a UUID")
		return false
	}
	return true
}
func page(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
	}
	if err != nil || limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return 0, 0, errors.New("limit must be 1..200 and offset 0..100000")
	}
	return limit, offset, nil
}
func times(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	var err error
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return from, to, err
		}
		from = to.Add(-24 * time.Hour)
	}
	if v := r.URL.Query().Get("from"); v != "" {
		from, err = time.Parse(time.RFC3339, v)
	}
	if err != nil || !to.After(from) || to.Sub(from) > 24*time.Hour {
		return from, to, errors.New("time range must be positive and at most 24 hours")
	}
	return from, to, nil
}
func enum(v string, allowed ...string) bool {
	if v == "" {
		return true
	}
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := page(r)
	status := r.URL.Query().Get("status")
	if err != nil || !enum(status, "online", "degraded", "offline") {
		failure(w, 400, "invalid_query", "Invalid pagination or status")
		return
	}
	items, err := a.Store.Devices(r.Context(), status, limit, offset)
	respond(w, map[string]any{"items": items, "limit": limit, "offset": offset}, err)
}
func (a *API) createDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string  `json:"name"`
		Location string  `json:"location"`
		Capacity float64 `json:"capacity_mbps"`
	}
	if err := decode(r, &input); err != nil || strings.TrimSpace(input.Name) == "" || len(input.Name) > 100 || len(input.Location) > 200 || input.Capacity < 0 || input.Capacity > 1e7 {
		failure(w, 400, "invalid_device", "Provide name (1..100 characters), location (up to 200), and nonnegative capacity_mbps")
		return
	}
	d, err := a.Store.CreateDevice(r.Context(), domain.Device{Name: strings.TrimSpace(input.Name), Location: input.Location, Capacity: input.Capacity})
	if err != nil {
		respond(w, nil, err)
		return
	}
	write(w, 201, d)
}
func (a *API) device(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r) {
		return
	}
	var d domain.Device
	var err error
	if a.Cache != nil {
		d, err = a.Cache.Device(r.Context(), r.PathValue("id"), a.Store.Device)
	} else {
		d, err = a.Store.Device(r.Context(), r.PathValue("id"))
	}
	respond(w, d, err)
}
func (a *API) telemetry(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r) {
		return
	}
	from, to, err := times(r)
	if err != nil {
		failure(w, 400, "invalid_range", err.Error())
		return
	}
	if _, err = a.Store.Device(r.Context(), r.PathValue("id")); err != nil {
		respond(w, nil, err)
		return
	}
	items, err := a.Store.Telemetry(r.Context(), r.PathValue("id"), from, to)
	respond(w, map[string]any{"items": items}, err)
}
func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, 413, "body_too_large", "Request body exceeds 1 MiB")
		return
	}
	raw = bytes.TrimSpace(raw)
	events := []domain.Event{}
	if len(raw) > 0 && raw[0] == '[' {
		err = json.Unmarshal(raw, &events)
	} else {
		var e domain.Event
		err = json.Unmarshal(raw, &e)
		events = append(events, e)
	}
	if err != nil || len(events) < 1 || len(events) > 100 {
		failure(w, 400, "invalid_event", "Expected one event or an array of 1..100 complete events")
		return
	}
	seen := map[string]bool{}
	for _, e := range events {
		if err = e.Validate(time.Now()); err != nil {
			failure(w, 400, "invalid_event", err.Error())
			return
		}
		if !seen[e.DeviceID] {
			if _, err = a.Store.Device(r.Context(), e.DeviceID); err != nil {
				respond(w, nil, err)
				return
			}
			seen[e.DeviceID] = true
		}
	}
	for _, e := range events {
		b, _ := json.Marshal(e)
		if err = a.Bus.Publish(r.Context(), a.Config.TelemetryTopic, e.DeviceID, b); err != nil {
			failure(w, 503, "publish_failed", "Publishing interrupted; retry the same event IDs safely")
			return
		}
		metrics.Received.Inc()
	}
	write(w, 202, map[string]int{"accepted": len(events)})
}
func (a *API) incidents(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := page(r)
	from, to, timeErr := times(r)
	q := r.URL.Query()
	if err != nil || timeErr != nil || !enum(q.Get("status"), "open", "acknowledged", "resolved") || !enum(q.Get("severity"), "low", "medium", "high", "critical") {
		failure(w, 400, "invalid_query", "Invalid filters, pagination or time range (maximum 24h)")
		return
	}
	if id := q.Get("device_id"); id != "" {
		if _, err = uuid.Parse(id); err != nil {
			failure(w, 400, "invalid_id", "device_id must be a UUID")
			return
		}
	}
	items, err := a.Store.Incidents(r.Context(), database.Filter{Status: q.Get("status"), Severity: q.Get("severity"), Device: q.Get("device_id"), From: from, To: to, Limit: limit, Offset: offset})
	respond(w, map[string]any{"items": items, "limit": limit, "offset": offset}, err)
}
func (a *API) incident(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r) {
		return
	}
	i, err := a.Store.Incident(r.Context(), r.PathValue("id"))
	respond(w, i, err)
}
func (a *API) action(w http.ResponseWriter, r *http.Request) {
	if !validID(w, r) {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "acknowledge":
		err = a.Store.Transition(r.Context(), r.PathValue("id"), "acknowledged")
	case "resolve":
		err = a.Store.Transition(r.Context(), r.PathValue("id"), "resolved")
	case "analyze":
		err = a.Store.RequestAnalysis(r.Context(), r.PathValue("id"))
	default:
		failure(w, 404, "not_found", "Unknown action")
		return
	}
	if err != nil {
		respond(w, nil, err)
		return
	}
	i, err := a.Store.Incident(r.Context(), r.PathValue("id"))
	respond(w, i, err)
}
