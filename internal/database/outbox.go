package database

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"netpulse/internal/metrics"
)

type Publisher interface {
	Publish(context.Context, string, string, []byte) error
}

// Holding row locks while publishing permits multiple dispatchers. A crash after
// publish but before delete can duplicate a message, so consumers must deduplicate.
func (s *Store) Dispatch(ctx context.Context, p Publisher) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT id,topic,key,payload FROM outbox ORDER BY id LIMIT 50 FOR UPDATE SKIP LOCKED")
	if err != nil {
		return err
	}
	type message struct {
		id         int64
		topic, key string
		payload    []byte
	}
	batch := []message{}
	for rows.Next() {
		var m message
		if err = rows.Scan(&m.id, &m.topic, &m.key, &m.payload); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range batch {
		if err = p.Publish(ctx, m.topic, m.key, m.payload); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM outbox WHERE id=$1", m.id); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, m := range batch {
		if m.topic != s.Config.IncidentTopic {
			continue
		}
		var v struct {
			Action string
			Status string
		}
		if json.Unmarshal(m.payload, &v) == nil {
			if v.Action == "created" {
				metrics.Created.Inc()
			}
			if v.Status == "resolved" {
				metrics.Resolved.Inc()
			}
		}
	}
	return nil
}

// ClaimAnalysis uses a durable lease and generation fencing to avoid concurrent charges
// and prevent a late provider response overwriting a newer retry.
func (s *Store) ClaimAnalysis(ctx context.Context) (string, int, error) {
	var id string
	var generation int
	err := s.Pool.QueryRow(ctx, `UPDATE incident_analyses SET status='processing',attempts=attempts+1,generation=generation+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE incident_id=(SELECT incident_id FROM incident_analyses WHERE ((status='pending' AND updated_at <= now()-make_interval(secs => power(2,attempts)::int)) OR (status='processing' AND lease_until<now())) AND attempts<3 ORDER BY updated_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING incident_id::text,generation`).Scan(&id, &generation)
	return id, generation, err
}
func (s *Store) FinishAnalysis(ctx context.Context, id string, generation int, result []byte, failed bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	if err = tx.QueryRow(ctx, "SELECT attempts FROM incident_analyses WHERE incident_id=$1 AND generation=$2 AND status='processing' FOR UPDATE", id, generation).Scan(&attempts); err == pgx.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	status := "completed"
	if failed {
		status = "pending"
		if attempts >= 3 {
			status = "failed"
			if err = enqueue(ctx, tx, s.Config.AnalysisDLQ, id, map[string]any{"schema_version": 1, "incident_id": id, "reason": "provider failed after 3 attempts"}); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, "UPDATE incident_analyses SET status=$3,result=$4,lease_until=NULL,updated_at=now() WHERE incident_id=$1 AND generation=$2", id, generation, status, result)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReapAnalysis finalizes exhausted leases left by a process crash on its last attempt.
func (s *Store) ReapAnalysis(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `UPDATE incident_analyses SET status='failed',generation=generation+1,lease_until=NULL,updated_at=now() WHERE status='processing' AND lease_until<now() AND attempts>=3 RETURNING incident_id::text`)
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
		if err = enqueue(ctx, tx, s.Config.AnalysisDLQ, id, map[string]any{"schema_version": 1, "incident_id": id, "reason": "analysis lease exhausted after worker interruption"}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
