package eventbus

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFailed = errors.New("delivery is not failed or is currently leased")

func migrateOperations(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(bus_deliveries)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var id, notnull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "failure_code" {
			found = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = db.Exec("ALTER TABLE bus_deliveries ADD COLUMN failure_code TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS bus_target_state ON bus_deliveries(target,state,event);
 CREATE TABLE IF NOT EXISTS bus_replay_audit(id INTEGER PRIMARY KEY AUTOINCREMENT,event TEXT NOT NULL REFERENCES bus_events(id) ON DELETE CASCADE,target TEXT NOT NULL,at INTEGER NOT NULL,actor TEXT NOT NULL,mode TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS bus_audit_event ON bus_replay_audit(event,id);`)
	return err
}
func validTarget(target string) bool {
	return target == "webhook" || target == "nats" || target == "rabbitmq" || target == "redis-streams" || target == "sqs" || target == "n8n"
}

type DeliveryStatus struct {
	Sequence    int64  `json:"sequence"`
	ID          string `json:"eventId"`
	Event       string `json:"event"`
	Target      string `json:"target"`
	State       string `json:"state"`
	Attempt     int    `json:"attempt"`
	Created     int64  `json:"createdAt"`
	Next        int64  `json:"nextAttemptAt"`
	LeasedUntil int64  `json:"leasedUntil"`
	Failure     string `json:"failureCode"`
}

func (s *Log) Deliveries(ctx context.Context, session, target, state string, after int64, limit int) ([]DeliveryStatus, error) {
	if !validTarget(target) || (state != "failed" && state != "pending" && state != "delivered") || after < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("target, state, cursor or limit is invalid")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.seq,e.id,e.name,d.target,d.state,d.attempt,e.created,d.next,d.lease_until,d.failure_code FROM bus_deliveries d JOIN bus_events e ON e.id=d.event WHERE e.session=? AND d.target=? AND d.state=? AND e.seq>? AND e.created>=? ORDER BY e.seq LIMIT ?`, session, target, state, after, time.Now().Add(-s.Retention).UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryStatus{}
	for rows.Next() {
		var d DeliveryStatus
		if err = rows.Scan(&d.Sequence, &d.ID, &d.Event, &d.Target, &d.State, &d.Attempt, &d.Created, &d.Next, &d.LeasedUntil, &d.Failure); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func auditReplay(ctx context.Context, tx *sql.Tx, event, target, actor, mode string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO bus_replay_audit(event,target,at,actor,mode) VALUES(?,?,?,?,?)", event, target, time.Now().UnixMilli(), actor, mode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM bus_replay_audit WHERE id <= (SELECT coalesce(max(id),0)-100000 FROM bus_replay_audit)")
	return err
}
func (s *Log) ReplayFailed(ctx context.Context, session, id, target, actor string) error {
	if !validTarget(target) || (actor != "administrator" && actor != "session-token") {
		return errors.New("invalid replay target or actor")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The update acquires the write lock, and restricts ownership in the same SQL.
	result, err := tx.ExecContext(ctx, `UPDATE bus_deliveries SET state='pending',attempt=0,next=0,lease='',lease_until=0,failure_code='' WHERE event=? AND target=? AND state='failed' AND lease_until<=? AND EXISTS(SELECT 1 FROM bus_events e WHERE e.id=bus_deliveries.event AND e.session=? AND e.created>=?)`, id, target, time.Now().UnixMilli(), session, time.Now().Add(-s.Retention).UnixMilli())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		var exists int
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM bus_events WHERE id=? AND session=? AND created>=?", id, session, time.Now().Add(-s.Retention).UnixMilli()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMissing
		}
		if err != nil {
			return err
		}
		return ErrNotFailed
	}
	if err = auditReplay(ctx, tx, id, target, actor, "failed-only"); err != nil {
		return err
	}
	return tx.Commit()
}

type ReplayAudit struct {
	Cursor  int64  `json:"cursor"`
	EventID string `json:"eventId"`
	Target  string `json:"target"`
	At      int64  `json:"at"`
	Actor   string `json:"actorType"`
	Mode    string `json:"mode"`
}

func (s *Log) ReplayAudit(ctx context.Context, session string, after int64, limit int) ([]ReplayAudit, error) {
	if after < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("invalid cursor or limit")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.event,a.target,a.at,a.actor,a.mode FROM bus_replay_audit a JOIN bus_events e ON e.id=a.event WHERE e.session=? AND a.id>? ORDER BY a.id LIMIT ?`, session, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReplayAudit{}
	for rows.Next() {
		var a ReplayAudit
		if err = rows.Scan(&a.Cursor, &a.EventID, &a.Target, &a.At, &a.Actor, &a.Mode); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type QueueHealth struct {
	Target           string
	State            string
	Count            int64
	OldestAgeSeconds float64
}
type LogHealth struct {
	Events, Bytes int64
	Queues        []QueueHealth
}

func (s *Log) Health(ctx context.Context) (LogHealth, error) {
	var h LogHealth
	if err := s.db.QueryRowContext(ctx, "SELECT n,b FROM bus_stats WHERE id=1").Scan(&h.Events, &h.Bytes); err != nil {
		return h, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.target,d.state,count(*),min(e.created) FROM bus_deliveries d JOIN bus_events e ON e.id=d.event GROUP BY d.target,d.state`)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var q QueueHealth
		var oldest int64
		if err = rows.Scan(&q.Target, &q.State, &q.Count, &oldest); err != nil {
			return h, err
		}
		if validTarget(q.Target) && (q.State == "pending" || q.State == "failed" || q.State == "delivered") {
			q.OldestAgeSeconds = max(0, float64(time.Now().UnixMilli()-oldest)/1000)
			h.Queues = append(h.Queues, q)
		}
	}
	return h, rows.Err()
}
