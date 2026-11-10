package eventbus

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFailedReplayIsAtomicIsolatedAuditedAndTargeted(t *testing.T) {
	s := testLog(t)
	defer s.Close()
	ctx := context.Background()
	e, err := s.Append(ctx, "onmessage", "alpha", map[string]any{"private": "do not return"}, []string{"webhook", "nats"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("UPDATE bus_deliveries SET state=CASE target WHEN 'webhook' THEN 'failed' ELSE 'delivered' END,attempt=4,failure_code=CASE target WHEN 'webhook' THEN 'retry_exhausted' ELSE '' END")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Deliveries(ctx, "alpha", "webhook", "failed", 0, 50)
	if err != nil || len(rows) != 1 || rows[0].Failure != "retry_exhausted" || rows[0].Attempt != 4 {
		t.Fatal(rows, err)
	}
	if err = s.ReplayFailed(ctx, "beta", e.ID, "webhook", "session-token"); !errors.Is(err, ErrMissing) {
		t.Fatal("other session replayed", err)
	}
	if err = s.ReplayFailed(ctx, "alpha", e.ID, "nats", "session-token"); !errors.Is(err, ErrNotFailed) {
		t.Fatal("delivered target replayed", err)
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.ReplayFailed(ctx, "alpha", e.ID, "webhook", "session-token")
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, ErrNotFailed) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("duplicate replay", count.Load())
	}
	delivered, err := s.Deliveries(ctx, "alpha", "nats", "delivered", 0, 50)
	if err != nil || len(delivered) != 1 || delivered[0].Attempt != 4 {
		t.Fatal("changed successful target", delivered, err)
	}
	audit, err := s.ReplayAudit(ctx, "alpha", 0, 50)
	if err != nil || len(audit) != 1 || audit[0].Mode != "failed-only" || audit[0].Actor != "session-token" {
		t.Fatal(audit, err)
	}
	audit, err = s.ReplayAudit(ctx, "beta", 0, 50)
	if err != nil || len(audit) != 0 {
		t.Fatal("cross-session audit", audit, err)
	}
}
func TestDeliveryPaginationLeaseAndHealth(t *testing.T) {
	s := testLog(t)
	defer s.Close()
	ctx := context.Background()
	ids := []string{}
	for i := 0; i < 3; i++ {
		e, err := s.Append(ctx, "onmessage", "alpha", map[string]any{}, []string{"webhook", "nats"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	var cursor int64
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		rows, err := s.Deliveries(ctx, "alpha", "webhook", "pending", cursor, 1)
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		seen[rows[0].ID] = true
		cursor = rows[0].Sequence
	}
	if len(seen) != 3 {
		t.Fatal("pagination skipped event")
	}
	if _, err := s.db.Exec("UPDATE bus_events SET created=?", time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	health, err := s.Health(ctx)
	if err != nil || health.Events != 3 || health.Bytes <= 0 || len(health.Queues) != 2 {
		t.Fatal(health, err)
	}
	for _, q := range health.Queues {
		if q.Count != 3 || q.OldestAgeSeconds < 59 {
			t.Fatal(q)
		}
	}
	if _, err = s.db.Exec("UPDATE bus_deliveries SET state='failed',lease_until=? WHERE event=? AND target='webhook'", time.Now().Add(time.Minute).UnixMilli(), ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = s.ReplayFailed(ctx, "alpha", ids[0], "webhook", "administrator"); !errors.Is(err, ErrNotFailed) {
		t.Fatal("active lease overwritten", err)
	}
}
func TestOperationsMigrateOldDatabasePreservingDeliveries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE bus_events(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,session TEXT NOT NULL,name TEXT NOT NULL,created INTEGER NOT NULL,body BLOB NOT NULL);
 CREATE TABLE bus_deliveries(event TEXT NOT NULL REFERENCES bus_events(id) ON DELETE CASCADE,target TEXT NOT NULL,attempt INTEGER NOT NULL DEFAULT 0,next INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT 'pending',lease TEXT NOT NULL DEFAULT '',lease_until INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(event,target));
 INSERT INTO bus_events(id,session,name,created,body) VALUES('retained','alpha','onmessage',?,X'7B7D');
 INSERT INTO bus_deliveries(event,target,attempt,state) VALUES('retained','webhook',4,'failed');`, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.Deliveries(context.Background(), "alpha", "webhook", "failed", 0, 10)
		s.Close()
		if err != nil || len(rows) != 1 || rows[0].ID != "retained" || rows[0].Attempt != 4 || rows[0].Failure != "" {
			t.Fatal("migration lost data", rows, err)
		}
	}
}
func TestReplayAuditRollsBackWithStateAndLegacyIsVisible(t *testing.T) {
	s := testLog(t)
	defer s.Close()
	ctx := context.Background()
	e, err := s.Append(ctx, "onmessage", "alpha", map[string]any{}, []string{"webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Replay(ctx, "alpha", e.ID, []string{"webhook"}); err != nil {
		t.Fatal(err)
	}
	audits, err := s.ReplayAudit(ctx, "alpha", 0, 50)
	if err != nil || len(audits) != 1 || audits[0].Mode != "all-states" {
		t.Fatal(audits, err)
	}
	if _, err = s.db.Exec("UPDATE bus_deliveries SET state='failed'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER reject_audit BEFORE INSERT ON bus_replay_audit BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReplayFailed(ctx, "alpha", e.ID, "webhook", "administrator"); err == nil {
		t.Fatal("replay without audit")
	}
	failed, err := s.Deliveries(ctx, "alpha", "webhook", "failed", 0, 50)
	if err != nil || len(failed) != 1 {
		t.Fatal("state not rolled back", failed, err)
	}
}
