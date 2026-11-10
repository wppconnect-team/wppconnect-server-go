package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Exercise the production workers, SQLite receipts and two independent targets.
// Only the temporary stream key is made unwritable. The broker remains available.
func TestIntegrationRedisBacklogReplayAcrossRestart(t *testing.T) {
	const count = 32
	addr := integrationEnv(t, "INTEGRATION_REDIS_URL")
	key := "wpp:backlog:" + uuid.NewString()
	openRedis := func() *RedisStream {
		t.Helper()
		target, err := NewRedisStream(addr, key, count*2)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probe := openRedis()
	defer probe.Close()
	if err := probe.client.Set(ctx, key, "temporarily not a stream", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	defer probe.client.Del(context.Background(), key)

	var mu sync.Mutex
	received := make(map[string]int)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received[r.Header.Get("X-WPP-Event-ID")]++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	dbPath := filepath.Join(t.TempDir(), "outbox.db")
	start := func() *Bus {
		t.Helper()
		db, err := Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		webhook, err := NewHTTP(receiver.URL)
		if err != nil {
			t.Fatal(err)
		}
		bus, err := New(db, []Target{webhook, openRedis()}, Options{Workers: 4, Attempts: 2, Timeout: 2 * time.Second, RetryBase: time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeBus(t, bus) })
		return bus
	}
	sessions := []string{"alpha", "beta"}
	bus := start()
	for i := 0; i < count; i++ {
		if err := bus.Publish("onmessage", sessions[i%len(sessions)], map[string]any{"index": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := bus.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := probe.client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	restarted := start()
	expected := make(map[string]string)
	for i, session := range sessions {
		failed, err := restarted.Log.Deliveries(ctx, session, "redis-streams", "failed", 0, count)
		if err != nil || len(failed) != count/2 {
			t.Fatalf("failed deliveries for %s: %d, %v", session, len(failed), err)
		}
		for _, d := range failed {
			if d.Attempt != 2 || d.Failure != "retry_exhausted" {
				t.Fatalf("unexpected exhausted delivery: %+v", d)
			}
			if err := restarted.Log.ReplayFailed(ctx, sessions[1-i], d.ID, "redis-streams", "session-token"); !errors.Is(err, ErrMissing) {
				t.Fatalf("cross-session replay: %v", err)
			}
			if err := restarted.Log.ReplayFailed(ctx, session, d.ID, "redis-streams", "session-token"); err != nil {
				t.Fatal(err)
			}
			expected[d.ID] = session
		}
		audit, err := restarted.Log.ReplayAudit(ctx, session, 0, count)
		if err != nil || len(audit) != count/2 {
			t.Fatalf("replay audit for %s: %d, %v", session, len(audit), err)
		}
	}
	if err := restarted.Close(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := probe.client.XRange(ctx, key, "-", "+").Result()
	if err != nil || len(rows) != count || len(expected) != count {
		t.Fatalf("stream size %d, expected IDs %d: %v", len(rows), len(expected), err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != count {
		t.Fatalf("webhook got %d unique events", len(received))
	}
	seen := make(map[int]bool)
	for _, row := range rows {
		id, _ := row.Values["eventId"].(string)
		session, ok := expected[id]
		payload, _ := row.Values["payload"].(string)
		var data struct {
			Index   *int
			Session string
		}
		if !ok || row.Values["session"] != session || received[id] != 1 || json.Unmarshal([]byte(payload), &data) != nil || data.Session != session || data.Index == nil {
			t.Fatal("missing, duplicated or misrouted event")
		}
		n := *data.Index
		if n < 0 || n >= count || seen[n] || sessions[n%2] != session {
			t.Fatal("invalid payload index")
		}
		seen[n] = true
		delete(expected, id)
	}
	if len(expected) != 0 {
		t.Fatal("missing event identities after replay")
	}
}
