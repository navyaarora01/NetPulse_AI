package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"netpulse/internal/anomaly"
	"netpulse/internal/domain"
)

// Process serializes per-device updates and commits the ledger, aggregates, incident state
// and outbox atomically. Historical events aggregate but cannot overwrite current health.
func (s *Store) Process(ctx context.Context, e domain.Event) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	d, err := scanDevice(tx.QueryRow(ctx, "SELECT "+deviceCols+" FROM devices WHERE id=$1 FOR UPDATE", e.DeviceID))
	if err != nil {
		return false, err
	}
	b, _ := json.Marshal(e.Metrics)
	tag, err := tx.Exec(ctx, "INSERT INTO telemetry_events(event_id,device_id,timestamp,metrics) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", e.ID, e.DeviceID, e.Timestamp, b)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	m := e.Metrics
	_, err = tx.Exec(ctx, `INSERT INTO telemetry_aggregates(device_id,bucket,samples,latency,loss,bandwidth,cpu,memory,signal) VALUES($1,date_trunc('minute',$2::timestamptz),1,$3,$4,$5,$6,$7,$8) ON CONFLICT(device_id,bucket) DO UPDATE SET samples=telemetry_aggregates.samples+1,latency=telemetry_aggregates.latency+EXCLUDED.latency,loss=telemetry_aggregates.loss+EXCLUDED.loss,bandwidth=telemetry_aggregates.bandwidth+EXCLUDED.bandwidth,cpu=telemetry_aggregates.cpu+EXCLUDED.cpu,memory=telemetry_aggregates.memory+EXCLUDED.memory,signal=telemetry_aggregates.signal+EXCLUDED.signal`, e.DeviceID, e.Timestamp, m.Latency, m.Loss, m.Bandwidth, m.CPU, m.Memory, m.Signal)
	if err != nil {
		return false, err
	}
	if d.LastSeen != nil && !e.Timestamp.After(*d.LastSeen) {
		return true, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `SELECT timestamp,metrics FROM (SELECT timestamp,metrics FROM telemetry_events WHERE device_id=$1 AND timestamp >= $2 AND timestamp <= $3 ORDER BY timestamp DESC,event_id LIMIT $4) t ORDER BY timestamp`, e.DeviceID, e.Timestamp.Add(-s.Config.Window), e.Timestamp, s.Config.MinSamples)
	if err != nil {
		return false, err
	}
	samples := []anomaly.Sample{}
	for rows.Next() {
		var a anomaly.Sample
		var raw []byte
		if err = rows.Scan(&a.At, &raw); err != nil {
			rows.Close()
			return false, err
		}
		if err = json.Unmarshal(raw, &a.Metrics); err != nil {
			rows.Close()
			return false, err
		}
		samples = append(samples, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	rules := anomaly.Evaluate(samples, d.Capacity, s.Config)
	for _, r := range rules {
		if err = s.upsertIncident(ctx, tx, e.DeviceID, r.Type, r.Title, anomaly.Severity(r, s.Config), fmt.Sprintf("Observed %.2f; configured threshold %.2f", r.Value, r.Threshold), e.Timestamp); err != nil {
			return false, err
		}
	}
	healthy := anomaly.Healthy(e.Metrics, d.Capacity, s.Config)
	_, err = tx.Exec(ctx, `UPDATE devices SET last_seen_at=$2,latest=$3,updated_at=now(),healthy_since=CASE WHEN $4 THEN COALESCE(healthy_since,$2) ELSE NULL END WHERE id=$1`, e.DeviceID, e.Timestamp, b, healthy)
	if err != nil {
		return false, err
	}
	// Recovery requires continuously normal observations spanning the recovery window.
	// A heartbeat gap resets recovery rather than counting downtime as healthy time.
	if d.LastSeen != nil && e.Timestamp.Sub(*d.LastSeen) > s.Config.Offline {
		_, err = tx.Exec(ctx, "UPDATE devices SET healthy_since=CASE WHEN $2 THEN $3::timestamptz ELSE NULL END WHERE id=$1", d.ID, healthy, e.Timestamp)
		if err != nil {
			return false, err
		}
	}
	recovered, err := tx.Query(ctx, `UPDATE incidents SET status='resolved',resolved_at=$2,updated_at=now() WHERE device_id=$1 AND status<>'resolved' AND EXISTS(SELECT 1 FROM devices WHERE id=$1 AND healthy_since <= $3) RETURNING id::text`, d.ID, e.Timestamp, e.Timestamp.Add(-s.Config.Recovery))
	if err != nil {
		return false, err
	}
	ids := []string{}
	for recovered.Next() {
		var id string
		if err = recovered.Scan(&id); err != nil {
			recovered.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = recovered.Err()
	recovered.Close()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if err = enqueue(ctx, tx, s.Config.IncidentTopic, d.ID, map[string]any{"schema_version": 1, "event_id": uuid.NewString(), "incident_id": id, "status": "resolved"}); err != nil {
			return false, err
		}
	}
	status := "online"
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM incidents WHERE device_id=$1 AND status<>'resolved')", d.ID).Scan(&active); err != nil {
		return false, err
	}
	if active || len(rules) > 0 {
		status = "degraded"
	}
	if d.Status != status {
		if _, err = tx.Exec(ctx, "INSERT INTO device_transitions(device_id,timestamp,status) VALUES($1,$2,$3)", d.ID, e.Timestamp, status); err != nil {
			return false, err
		}
	}
	var transitions int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM device_transitions WHERE device_id=$1 AND timestamp >= $2", d.ID, e.Timestamp.Add(-s.Config.Window)).Scan(&transitions); err != nil {
		return false, err
	}
	if transitions >= s.Config.Instability {
		if err = s.upsertIncident(ctx, tx, d.ID, "instability", "Repeated device instability", "high", "Repeated health transitions in the rolling window", e.Timestamp); err != nil {
			return false, err
		}
		status = "degraded"
	}
	if _, err = tx.Exec(ctx, "UPDATE devices SET status=$2 WHERE id=$1", d.ID, status); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}

	return true, nil
}
func (s *Store) upsertIncident(ctx context.Context, tx pgx.Tx, device, kind, title, severity, description string, at time.Time) error {
	var id string
	err := tx.QueryRow(ctx, `UPDATE incidents SET last_seen_at=GREATEST(last_seen_at,$3),occurrence_count=occurrence_count+1,updated_at=now(),severity=CASE WHEN severity='critical' OR $4='critical' THEN 'critical' WHEN severity='high' OR $4='high' THEN 'high' ELSE $4 END WHERE device_id=$1 AND type=$2 AND status<>'resolved' RETURNING id::text`, device, kind, at, severity).Scan(&id)
	if err == nil {
		return enqueue(ctx, tx, s.Config.IncidentTopic, device, map[string]any{"schema_version": 1, "event_id": uuid.NewString(), "incident_id": id, "action": "observed"})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var cooldown bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM incidents WHERE device_id=$1 AND type=$2 AND resolved_at>$3)", device, kind, at.Add(-s.Config.Cooldown)).Scan(&cooldown); err != nil {
		return err
	}
	if cooldown {
		return nil
	}
	id = uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO incidents(id,device_id,type,severity,title,description,first_seen_at,last_seen_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, id, device, kind, severity, title, description, at)
	if err != nil {
		return err
	}
	status := "unavailable"
	if s.Config.AIEnabled {
		status = "pending"
	}
	if _, err = tx.Exec(ctx, "INSERT INTO incident_analyses(incident_id,status) VALUES($1,$2)", id, status); err != nil {
		return err
	}
	if err = enqueue(ctx, tx, s.Config.IncidentTopic, device, map[string]any{"schema_version": 1, "event_id": uuid.NewString(), "incident_id": id, "action": "created"}); err != nil {
		return err
	}
	if s.Config.AIEnabled {
		if err = enqueue(ctx, tx, s.Config.AnalysisTopic, id, map[string]any{"schema_version": 1, "incident_id": id}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Maintenance(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT id::text FROM devices WHERE COALESCE(last_seen_at,created_at)<$1 AND (status<>'offline' OR NOT EXISTS(SELECT 1 FROM incidents WHERE device_id=devices.id AND type='device_offline')) ORDER BY id FOR UPDATE SKIP LOCKED", time.Now().Add(-s.Config.Offline))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.upsertIncident(ctx, tx, id, "device_offline", "Device heartbeat missing", "critical", "No telemetry received within the configured timeout", time.Now().UTC()); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE devices SET status='offline',healthy_since=NULL,updated_at=now() WHERE id=$1", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO device_transitions VALUES($1,now(),'offline')", id); err != nil {
			return err
		}
	}
	for _, q := range []string{"DELETE FROM telemetry_events WHERE received_at<$1", "DELETE FROM telemetry_aggregates WHERE bucket<$1", "DELETE FROM device_transitions WHERE timestamp<$1"} {
		if _, err = tx.Exec(ctx, q, time.Now().Add(-s.Config.Retention)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
