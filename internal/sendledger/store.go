// Package sendledger persists send reservations before any protocol side effect.
// A pending or ambiguous reservation is never automatically retried or expired.
package sendledger

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const MaxResponse = 64 << 10

var ErrConflict = errors.New("idempotency key already belongs to another request")
var ErrFull = errors.New("send ledger capacity reached")
var ErrMissing = errors.New("send result not found")

type Record struct {
	Fingerprint string
	State       string
	Code        int
	Body        []byte
	ContentType string
	Created     int64
	Owner       string
}
type Store struct {
	db        *sql.DB
	Limit     int
	MaxBytes  int64
	Retention time.Duration
}

func Open(path string) (*Store, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("send ledger cannot be a symlink")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;
 CREATE TABLE IF NOT EXISTS sends(session TEXT NOT NULL,key_hash TEXT NOT NULL,fingerprint TEXT NOT NULL,state TEXT NOT NULL,code INTEGER NOT NULL DEFAULT 0,body BLOB NOT NULL DEFAULT X'',content_type TEXT NOT NULL DEFAULT '',created INTEGER NOT NULL,owner TEXT NOT NULL,PRIMARY KEY(session,key_hash));
 CREATE INDEX IF NOT EXISTS send_retention ON sends(state,created);`)
	if err == nil {
		err = os.Chmod(path, 0600)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, Limit: 100000, MaxBytes: 128 << 20, Retention: 7 * 24 * time.Hour}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func scan(row *sql.Row) (Record, error) {
	var r Record
	err := row.Scan(&r.Fingerprint, &r.State, &r.Code, &r.Body, &r.ContentType, &r.Created, &r.Owner)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrMissing
	}
	return r, err
}

const columns = "fingerprint,state,code,body,content_type,created,owner"

func (s *Store) Get(ctx context.Context, session, key string) (Record, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM sends WHERE session=? AND key_hash=?", session, key))
}
func (s *Store) Claim(ctx context.Context, session, key, fingerprint string) (Record, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, false, err
	}
	defer tx.Rollback()
	// Acquire the SQLite write lock before reading, including across processes.
	_, err = tx.ExecContext(ctx, "DELETE FROM sends WHERE state='complete' AND created<?", time.Now().Add(-s.Retention).Unix())
	if err != nil {
		return Record{}, false, err
	}
	r, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM sends WHERE session=? AND key_hash=?", session, key))
	if err == nil {
		if r.Fingerprint != fingerprint {
			return r, false, ErrConflict
		}
		return r, false, tx.Commit()
	}
	if !errors.Is(err, ErrMissing) {
		return r, false, err
	}
	var count int
	var used int64
	err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(CASE WHEN state='pending' THEN ? ELSE length(body) END),0) FROM sends", MaxResponse).Scan(&count, &used)
	if err != nil {
		return r, false, err
	}
	if count >= s.Limit || used+MaxResponse > s.MaxBytes {
		return r, false, ErrFull
	}
	r = Record{Fingerprint: fingerprint, State: "pending", Created: time.Now().Unix(), Owner: uuid.NewString()}
	_, err = tx.ExecContext(ctx, "INSERT INTO sends(session,key_hash,fingerprint,state,created,owner) VALUES(?,?,?,'pending',?,?)", session, key, fingerprint, r.Created, r.Owner)
	if err != nil {
		return r, false, err
	}
	return r, true, tx.Commit()
}
func (s *Store) Finish(ctx context.Context, session, key, owner string, code int, contentType string, body []byte) error {
	if len(body) > MaxResponse || code < 200 || code > 599 {
		return errors.New("invalid send result")
	}
	state := "complete"
	if body == nil {
		body = []byte{}
	}
	if code >= 500 || code == 408 {
		state = "ambiguous"
	}
	result, err := s.db.ExecContext(ctx, "UPDATE sends SET state=?,code=?,content_type=?,body=?,owner='' WHERE session=? AND key_hash=? AND state='pending' AND owner=?", state, code, contentType, body, session, key, owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		err = ErrConflict
	}
	return err
}
