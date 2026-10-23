// Package eventbus provides a durable, bounded event log with per-target delivery.
package eventbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

var ErrFull = errors.New("durable event log is full")
var ErrMissing = errors.New("event not found in this session")

type Event struct {
	Sequence int64           `json:"sequence"`
	ID       string          `json:"id"`
	Session  string          `json:"session"`
	Name     string          `json:"event"`
	Created  int64           `json:"createdAt"`
	Data     json.RawMessage `json:"data"`
	Attempt  int             `json:"-"`
}
type delivery struct {
	Event   Event
	Target  string
	Attempt int
	Lease   string
}
type Log struct {
	db        *sql.DB
	Limit     int
	MaxBytes  int64
	Retention time.Duration
}

func Open(path string) (*Log, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("event database cannot be a symbolic link")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;PRAGMA journal_mode=WAL;PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS bus_events(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,session TEXT NOT NULL,name TEXT NOT NULL,created INTEGER NOT NULL,body BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS bus_session ON bus_events(session,seq);
CREATE TABLE IF NOT EXISTS bus_deliveries(event TEXT NOT NULL REFERENCES bus_events(id) ON DELETE CASCADE,target TEXT NOT NULL,attempt INTEGER NOT NULL DEFAULT 0,next INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT 'pending',lease TEXT NOT NULL DEFAULT '',lease_until INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(event,target));
CREATE INDEX IF NOT EXISTS bus_ready ON bus_deliveries(state,next,lease_until);
CREATE TABLE IF NOT EXISTS bus_stats(id INTEGER PRIMARY KEY CHECK(id=1),n INTEGER NOT NULL,b INTEGER NOT NULL);
INSERT OR IGNORE INTO bus_stats SELECT 1,count(*),coalesce(sum(length(body)),0) FROM bus_events;
CREATE TRIGGER IF NOT EXISTS bus_insert AFTER INSERT ON bus_events BEGIN UPDATE bus_stats SET n=n+1,b=b+length(new.body) WHERE id=1;END;
CREATE TRIGGER IF NOT EXISTS bus_delete AFTER DELETE ON bus_events BEGIN UPDATE bus_stats SET n=n-1,b=b-length(old.body) WHERE id=1;END;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return &Log{db: db, Limit: 100000, MaxBytes: 128 << 20, Retention: 7 * 24 * time.Hour}, nil
}
func (s *Log) Close() error { return s.db.Close() }
func (s *Log) Append(ctx context.Context, name, session string, data map[string]any, targets []string) (Event, error) {
	e := Event{ID: uuid.NewString(), Session: session, Name: name, Created: time.Now().UnixMilli()}
	if name == "" || len(name) > 64 || session == "" || len(session) > 64 {
		return e, errors.New("invalid event identity")
	}
	body := make(map[string]any, len(data)+2)
	for k, v := range data {
		body[k] = v
	}
	body["event"], body["session"] = name, session
	encoded, err := json.Marshal(body)
	if err != nil {
		return e, err
	}
	if len(encoded) > 1<<20 {
		return e, errors.New("event exceeds 1 MiB")
	}
	e.Data = encoded
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	// Expire terminal events only. Pending work is never silently evicted.
	_, err = tx.ExecContext(ctx, `DELETE FROM bus_events WHERE created<? AND NOT EXISTS(SELECT 1 FROM bus_deliveries d WHERE d.event=bus_events.id AND d.state='pending')`, time.Now().Add(-s.Retention).UnixMilli())
	if err != nil {
		return e, err
	}
	var count, size int64
	if err = tx.QueryRowContext(ctx, "SELECT n,b FROM bus_stats WHERE id=1").Scan(&count, &size); err != nil {
		return e, err
	}
	if count >= int64(s.Limit) || size+int64(len(encoded)) > s.MaxBytes {
		return e, ErrFull
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO bus_events(id,session,name,created,body) VALUES(?,?,?,?,?)", e.ID, session, name, e.Created, encoded)
	if err != nil {
		return e, err
	}
	e.Sequence, err = result.LastInsertId()
	if err != nil {
		return e, err
	}
	for _, target := range targets {
		if _, err = tx.ExecContext(ctx, "INSERT INTO bus_deliveries(event,target) VALUES(?,?)", e.ID, target); err != nil {
			return e, err
		}
	}
	return e, tx.Commit()
}
func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	err := row.Scan(&e.Sequence, &e.ID, &e.Session, &e.Name, &e.Created, &e.Data)
	return e, err
}
func (s *Log) List(ctx context.Context, session string, after int64, limit int) ([]Event, error) {
	if after < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("cursor must be nonnegative and limit 1..200")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT seq,id,session,name,created,body FROM bus_events WHERE session=? AND seq>? AND created>=? ORDER BY seq LIMIT ?", session, after, time.Now().Add(-s.Retention).UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	bytes := 0
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		if bytes+len(e.Data) > 2<<20 && len(out) > 0 {
			break
		}
		bytes += len(e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Log) claim(ctx context.Context, targets []string, leaseTime time.Duration) (delivery, error) {
	var d delivery
	for _, target := range targets {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return d, err
		}
		row := tx.QueryRowContext(ctx, `SELECT e.seq,e.id,e.session,e.name,e.created,e.body,d.attempt FROM bus_deliveries d JOIN bus_events e ON e.id=d.event WHERE d.target=? AND d.state='pending' AND d.next<=? AND d.lease_until<=? ORDER BY d.next,e.seq LIMIT 1`, target, time.Now().UnixMilli(), time.Now().UnixMilli())
		err = row.Scan(&d.Event.Sequence, &d.Event.ID, &d.Event.Session, &d.Event.Name, &d.Event.Created, &d.Event.Data, &d.Attempt)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			continue
		}
		if err != nil {
			tx.Rollback()
			return d, err
		}
		d.Target = target
		d.Lease = uuid.NewString()
		_, err = tx.ExecContext(ctx, "UPDATE bus_deliveries SET lease=?,lease_until=? WHERE event=? AND target=?", d.Lease, time.Now().Add(leaseTime).UnixMilli(), d.Event.ID, target)
		if err != nil {
			tx.Rollback()
			return d, err
		}
		if err = tx.Commit(); err != nil {
			return d, err
		}
		return d, nil
	}
	return d, sql.ErrNoRows
}
func (s *Log) finish(ctx context.Context, d delivery, state string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE bus_deliveries SET state=?,attempt=attempt+1,next=?,lease='',lease_until=0 WHERE event=? AND target=? AND lease=?`, state, next.UnixMilli(), d.Event.ID, d.Target, d.Lease)
	return err
}
func (s *Log) Replay(ctx context.Context, session, id string, targets []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM bus_events WHERE id=? AND session=? AND created>=?", id, session, time.Now().Add(-s.Retention).UnixMilli()).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return ErrMissing
	} else if err != nil {
		return err
	}
	for _, target := range targets {
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM bus_deliveries WHERE event=? AND target=? AND lease_until>?", id, target, time.Now().UnixMilli()).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("event delivery is in progress")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bus_deliveries(event,target) VALUES(?,?) ON CONFLICT(event,target) DO UPDATE SET attempt=0,next=0,state='pending',lease='',lease_until=0`, id, target)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Log) Stats(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{"pending": 0, "delivered": 0, "failed": 0}
	rows, err := s.db.QueryContext(ctx, "SELECT state,count(*) FROM bus_deliveries GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int64
		if err = rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}
func (s *Log) pending(ctx context.Context, targets []string) (int64, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	args := make([]any, len(targets))
	for i, name := range targets {
		args[i] = name
	}
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM bus_deliveries WHERE state='pending' AND target IN ("+strings.TrimSuffix(strings.Repeat("?,", len(targets)), ",")+")", args...).Scan(&n)
	return n, err
}
