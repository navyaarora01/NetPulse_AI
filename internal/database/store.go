package database

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"netpulse/internal/config"
	"netpulse/internal/domain"
	"netpulse/migrations"
)

type Store struct {
	Pool   *pgxpool.Pool
	Config config.Config
}

func Open(ctx context.Context, c config.Config) (*Store, error) {
	p, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{p, c}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(942172)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	var applied bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=1)").Scan(&applied); err != nil {
		return err
	}
	if applied {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, migrations.Schema); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES(1)"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const deviceCols = "id::text,name,location,status,capacity_mbps,created_at,updated_at,last_seen_at,latest"

func scanDevice(row pgx.Row) (domain.Device, error) {
	var d domain.Device
	var b []byte
	err := row.Scan(&d.ID, &d.Name, &d.Location, &d.Status, &d.Capacity, &d.Created, &d.Updated, &d.LastSeen, &b)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err == nil && len(b) > 0 {
		err = json.Unmarshal(b, &d.Latest)
	}
	return d, err
}
func (s *Store) CreateDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	return scanDevice(s.Pool.QueryRow(ctx, "INSERT INTO devices(id,name,location,capacity_mbps) VALUES($1,$2,$3,$4) RETURNING "+deviceCols, d.ID, d.Name, d.Location, d.Capacity))
}
func (s *Store) Device(ctx context.Context, id string) (domain.Device, error) {
	return scanDevice(s.Pool.QueryRow(ctx, "SELECT "+deviceCols+" FROM devices WHERE id=$1", id))
}
func (s *Store) Devices(ctx context.Context, status string, limit, offset int) ([]domain.Device, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+deviceCols+" FROM devices WHERE ($1='' OR status=$1) ORDER BY created_at,id LIMIT $2 OFFSET $3", status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) Telemetry(ctx context.Context, id string, from, to time.Time) ([]domain.Aggregate, error) {
	rows, err := s.Pool.Query(ctx, `SELECT bucket,samples,latency/samples,loss/samples,bandwidth/samples,cpu/samples,memory/samples,signal/samples FROM telemetry_aggregates WHERE device_id=$1 AND bucket >= $2 AND bucket <= $3 ORDER BY bucket LIMIT 1441`, id, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Aggregate{}
	for rows.Next() {
		var a domain.Aggregate
		err = rows.Scan(&a.Timestamp, &a.Samples, &a.Metrics.Latency, &a.Metrics.Loss, &a.Metrics.Bandwidth, &a.Metrics.CPU, &a.Metrics.Memory, &a.Metrics.Signal)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const incidentCols = "i.id::text,i.device_id::text,i.type,i.severity,i.status,i.title,i.description,i.first_seen_at,i.last_seen_at,i.occurrence_count,i.created_at,i.updated_at,i.resolved_at,COALESCE(a.status,'unavailable'),a.result"

func scanIncident(row pgx.Row) (domain.Incident, error) {
	var i domain.Incident
	err := row.Scan(&i.ID, &i.DeviceID, &i.Type, &i.Severity, &i.Status, &i.Title, &i.Description, &i.FirstSeen, &i.LastSeen, &i.Count, &i.Created, &i.Updated, &i.Resolved, &i.AnalysisStatus, &i.Analysis)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return i, err
}
func (s *Store) Incident(ctx context.Context, id string) (domain.Incident, error) {
	return scanIncident(s.Pool.QueryRow(ctx, "SELECT "+incidentCols+" FROM incidents i LEFT JOIN incident_analyses a ON a.incident_id=i.id WHERE i.id=$1", id))
}

type Filter struct {
	Status, Severity, Device string
	From, To                 time.Time
	Limit, Offset            int
}

func (s *Store) Incidents(ctx context.Context, f Filter) ([]domain.Incident, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+incidentCols+` FROM incidents i LEFT JOIN incident_analyses a ON a.incident_id=i.id WHERE ($1='' OR i.status=$1) AND ($2='' OR i.severity=$2) AND ($3='' OR i.device_id::text=$3) AND i.created_at >= $4 AND i.created_at <= $5 ORDER BY i.created_at DESC,i.id LIMIT $6 OFFSET $7`, f.Status, f.Severity, f.Device, f.From, f.To, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Incident{}
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func enqueue(ctx context.Context, tx pgx.Tx, topic, key string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO outbox(topic,key,payload) VALUES($1,$2,$3)", topic, key, b)
	return err
}
func (s *Store) Transition(ctx context.Context, id, status string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var old, device string
	err = tx.QueryRow(ctx, "SELECT status,device_id::text FROM incidents WHERE id=$1 FOR UPDATE", id).Scan(&old, &device)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if old == status {
		return nil
	}
	if old == "resolved" {
		return domain.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE incidents SET status=$2,updated_at=now(),resolved_at=CASE WHEN $2='resolved' THEN now() ELSE NULL END WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if err = enqueue(ctx, tx, s.Config.IncidentTopic, device, map[string]any{"event_id": uuid.NewString(), "schema_version": 1, "incident_id": id, "status": status}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) RequestAnalysis(ctx context.Context, id string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists string
	if err = tx.QueryRow(ctx, "SELECT id::text FROM incidents WHERE id=$1 FOR UPDATE", id).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return err
	}
	status := "pending"
	if !s.Config.AIEnabled {
		status = "unavailable"
	}
	tag, err := tx.Exec(ctx, `INSERT INTO incident_analyses(incident_id,status) VALUES($1,$2) ON CONFLICT(incident_id) DO UPDATE SET status=EXCLUDED.status,attempts=0,updated_at=now(),generation=incident_analyses.generation+1 WHERE incident_analyses.status IN ('failed','unavailable')`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 && s.Config.AIEnabled {
		if err = enqueue(ctx, tx, s.Config.AnalysisTopic, id, map[string]any{"schema_version": 1, "incident_id": id}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type Summary struct {
	Devices   map[string]int `json:"devices"`
	Incidents map[string]int `json:"incidents"`
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	out := Summary{map[string]int{"online": 0, "degraded": 0, "offline": 0, "total": 0}, map[string]int{"low": 0, "medium": 0, "high": 0, "critical": 0}}
	rows, err := s.Pool.Query(ctx, "SELECT status,count(*) FROM devices GROUP BY status")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var status string
		var n int
		if err = rows.Scan(&status, &n); err != nil {
			rows.Close()
			return out, err
		}
		out.Devices[status] = n
		out.Devices["total"] += n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.Pool.Query(ctx, "SELECT severity,count(*) FROM incidents WHERE status<>'resolved' GROUP BY severity")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var severity string
		var n int
		if err = rows.Scan(&severity, &n); err != nil {
			return out, err
		}
		out.Incidents[severity] = n
	}
	return out, rows.Err()
}
