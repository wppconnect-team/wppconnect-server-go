package eventbus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the actual SQLite WAL across an abrupt process exit, not an orderly
// Log.Close. Neither the fixture nor its two destinations use a network.
func TestBacklogSurvivesProcessExit(t *testing.T) {
	const events = 128
	targets := []string{"webhook", "nats"}
	if os.Getenv("WPP_OUTBOX_CRASH_CHILD") == "1" {
		path := filepath.Join(os.Getenv("WPP_OUTBOX_CRASH_DIR"), "backlog.db")
		log, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		log.Limit = events
		ctx := context.Background()
		for i := 0; i < events; i++ {
			if _, err := log.Append(ctx, "onmessage", fmt.Sprintf("tenant%d", i%2), map[string]any{"index": i}, targets); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := log.Append(ctx, "onmessage", "tenant0", nil, targets); !errors.Is(err, ErrFull) {
			t.Fatalf("at capacity: %v", err)
		}
		for i := 0; i < 32; i++ {
			d, err := log.claim(ctx, targets[:1], time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err = log.finish(ctx, d, "delivered", time.Now(), ""); err != nil {
				t.Fatal(err)
			}
		}
		inFlight, err := log.claim(ctx, targets[1:], time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(inFlight)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path+".inflight", raw, 0600); err != nil {
			t.Fatal(err)
		}
		os.Exit(17) // skip defers and leave the lease and database as they were
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBacklogSurvivesProcessExit$")
	child.Env = append(os.Environ(), "WPP_OUTBOX_CRASH_CHILD=1", "WPP_OUTBOX_CRASH_DIR="+dir)
	buf, err := child.CombinedOutput()
	var status *exec.ExitError
	if !errors.As(err, &status) || status.ExitCode() != 17 {
		t.Fatalf("child did not reach crash point: %v %s", err, buf)
	}
	path := filepath.Join(dir, "backlog.db")
	raw, err := os.ReadFile(path + ".inflight")
	if err != nil {
		t.Fatal(err)
	}
	var stale delivery
	if err = json.Unmarshal(raw, &stale); err != nil {
		t.Fatal(err)
	}
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ids := map[string]int{}
	for i := 0; i < 2; i++ {
		session := fmt.Sprintf("tenant%d", i)
		rows, err := log.List(ctx, session, 0, events)
		if err != nil || len(rows) != events/2 {
			t.Fatalf("tenant %d count %d: %v", i, len(rows), err)
		}
		for j, e := range rows {
			var body struct{ Index int }
			if e.Session != session || json.Unmarshal(e.Data, &body) != nil || body.Index != 2*j+i || e.ID == "" {
				t.Fatalf("event changed: tenant %d item %d", i, j)
			}
			ids[e.ID] = body.Index
		}
	}
	if len(ids) != events {
		t.Fatalf("expected %d distinct IDs, got %d", events, len(ids))
	}
	// Reopen kept the live lease. Drain the other 127 events for that
	// destination without allowing this worker to steal the in-flight event.
	for n, target := range targets {
		seen := map[string]bool{}
		for {
			d, err := log.claim(ctx, []string{target}, time.Minute)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			idx, ok := ids[d.Event.ID]
			if !ok || seen[d.Event.ID] || (n == 0 && idx < 32) || (n == 1 && d.Event.ID == stale.Event.ID) {
				t.Fatalf("unexpected delivery index %d for %s", idx, target)
			}
			seen[d.Event.ID] = true
			if err = log.finish(ctx, d, "delivered", time.Now(), ""); err != nil {
				t.Fatal(err)
			}
		}
		want := events - 32
		if n == 1 {
			want = events - 1
		}
		if len(seen) != want {
			t.Fatalf("%s drained %d want %d", target, len(seen), want)
		}
	}
	// Move only the lease deadline into the past; avoid waiting a minute.
	if _, err = log.db.ExecContext(ctx, "UPDATE bus_deliveries SET lease_until=0 WHERE event=? AND target=? AND lease=?", stale.Event.ID, stale.Target, stale.Lease); err != nil {
		t.Fatal(err)
	}
	fresh, err := log.claim(ctx, targets, time.Minute)
	if err != nil || fresh.Event.ID != stale.Event.ID || fresh.Target != stale.Target || fresh.Lease == stale.Lease {
		t.Fatalf("lease not reacquired: %v", err)
	}
	if err = log.finish(ctx, stale, "failed", time.Now(), "timeout"); err != nil {
		t.Fatal(err)
	}
	if err = log.finish(ctx, fresh, "delivered", time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	h, err := log.Health(ctx)
	if err != nil || h.Events != events || len(h.Queues) != 2 {
		t.Fatalf("unexpected health %+v %v", h, err)
	}
	for _, q := range h.Queues {
		if q.State != "delivered" || q.Count != events {
			t.Fatalf("queue not fully drained: %+v", q)
		}
	}
}
