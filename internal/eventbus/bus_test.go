package eventbus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLog(t *testing.T) *Log {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func waitStats(t *testing.T, b *Bus, delivered, failed int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stats, err := b.Log.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if stats["delivered"] == delivered && stats["failed"] == failed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stats, _ := b.Log.Stats(context.Background())
	t.Fatalf("delivery stats: %+v", stats)
}
func closeBus(t *testing.T, b *Bus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDurableWebhookRetriesIdentityAndIsolation(t *testing.T) {
	var count atomic.Int32
	var mu sync.Mutex
	var ids, attempts []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ids = append(ids, r.Header.Get("X-WPP-Event-ID"))
		attempts = append(attempts, r.Header.Get("X-WPP-Attempt"))
		mu.Unlock()
		if count.Add(1) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer receiver.Close()
	target, err := NewHTTP(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := testLog(t)
	options := DefaultOptions()
	options.RetryBase = time.Millisecond
	b, err := New(store, []Target{target}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBus(t, b)
	if err = b.Publish("onmessage", "alpha", map[string]any{"body": "hello"}); err != nil {
		t.Fatal(err)
	}
	waitStats(t, b, 1, 0)
	mu.Lock()
	if len(ids) != 2 || ids[0] != ids[1] || attempts[0] != "1" || attempts[1] != "2" {
		t.Fatalf("delivery identities %+v %+v", ids, attempts)
	}
	mu.Unlock()
	events, err := store.List(context.Background(), "alpha", 0, 50)
	if err != nil || len(events) != 1 {
		t.Fatal("persisted log", err)
	}
	other, err := store.List(context.Background(), "beta", 0, 50)
	if err != nil || len(other) != 0 {
		t.Fatal("cross-session log")
	}
	if err = store.Replay(context.Background(), "beta", events[0].ID, b.Targets()); !errors.Is(err, ErrMissing) {
		t.Fatal("cross-session replay")
	}
	if err = store.Replay(context.Background(), "alpha", events[0].ID, b.Targets()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for count.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if count.Load() != 3 {
		t.Fatal("replay not delivered")
	}
	waitStats(t, b, 1, 0)
}

func TestOutboxCrashLeaseRecoveryAndBounds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := store.Append(ctx, "onmessage", "alpha", map[string]any{"body": "persisted"}, []string{"target"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.claim(ctx, []string{"target"}, 20*time.Millisecond)
	if err != nil || claim.Event.ID != e.ID {
		t.Fatal("lease", err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan Event, 1)
	target := &testTarget{name: "target", deliver: func(ctx context.Context, e Event) error { received <- e; return nil }}
	b, err := New(store, []Target{target}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer closeBus(t, b)
	select {
	case got := <-received:
		if got.ID != e.ID {
			t.Fatal("ID changed after crash")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending work not recovered")
	}
	waitStats(t, b, 1, 0)
	store.Limit = 1
	if _, err = store.Append(ctx, "onmessage", "alpha", nil, nil); !errors.Is(err, ErrFull) {
		t.Fatal("unbounded event log", err)
	}
}

type testTarget struct {
	name    string
	deliver func(context.Context, Event) error
}

func (t *testTarget) Name() string                               { return t.name }
func (t *testTarget) Deliver(ctx context.Context, e Event) error { return t.deliver(ctx, e) }
func (t *testTarget) Close() error                               { return nil }

func TestPerTargetFailuresAndPermanentRejection(t *testing.T) {
	var good, bad atomic.Int32
	targets := []Target{&testTarget{name: "good", deliver: func(context.Context, Event) error { good.Add(1); return nil }}, &testTarget{name: "bad", deliver: func(context.Context, Event) error { bad.Add(1); return &DeliveryError{Permanent: true} }}}
	b, err := New(testLog(t), targets, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer closeBus(t, b)
	if err = b.Publish("onmessage", "alpha", nil); err != nil {
		t.Fatal(err)
	}
	waitStats(t, b, 1, 1)
	if good.Load() != 1 || bad.Load() != 1 {
		t.Fatal("successful target replayed or permanent rejection retried")
	}
}

func TestShutdownDeadlineKeepsPending(t *testing.T) {
	started := make(chan struct{})
	store := testLog(t)
	b, err := New(store, []Target{&testTarget{name: "blocked", deliver: func(ctx context.Context, e Event) error { close(started); <-ctx.Done(); return ctx.Err() }}}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Publish("onmessage", "alpha", nil); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err = b.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown deadline", err)
	}
	select {
	case <-b.closed:
	case <-time.After(time.Second):
		t.Fatal("resources not released after timeout")
	}
}
