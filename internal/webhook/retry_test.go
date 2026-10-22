package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testDispatcher(t *testing.T, target string, edit func(*Options)) *Dispatcher {
	t.Helper()
	o := DefaultOptions()
	o.RetryBase = time.Millisecond
	o.RetryMax = 5 * time.Millisecond
	if edit != nil {
		edit(&o)
	}
	d, err := NewWithOptions(target, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d.Close(ctx)
	})
	return d
}
func drain(t *testing.T, d *Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookRetriesStableIDAndPayload(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var ids, bodies []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		ids = append(ids, r.Header.Get("X-WPP-Event-ID"))
		bodies = append(bodies, string(b))
		mu.Unlock()
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer receiver.Close()
	d := testDispatcher(t, receiver.URL, nil)
	d.OnConnected("alpha")
	drain(t, d)
	mu.Lock()
	defer mu.Unlock()
	if calls.Load() != 3 || ids[0] == "" || ids[0] != ids[1] || ids[1] != ids[2] || bodies[0] != bodies[2] {
		t.Fatal(calls.Load(), ids, bodies)
	}
	stats := d.Stats()
	if stats.Delivered != 1 || stats.Retries != 2 || stats.Failed != 0 {
		t.Fatal(stats)
	}
}

func TestWebhookPermanentRejectionNoRetryAndTransientBudget(t *testing.T) {
	for _, status := range []int{400, 401, 403, 302, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(status)
			}))
			defer receiver.Close()
			d := testDispatcher(t, receiver.URL, nil)
			d.OnConnected("alpha")
			drain(t, d)
			want := int32(1)
			if status == 429 || status >= 500 {
				want = 4
			}
			if calls.Load() != want || d.Stats().Failed != 1 {
				t.Fatal(calls.Load(), d.Stats())
			}
		})
	}
}

func TestWebhookQueuePayloadAndShutdownBounds(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	defer close(release)
	d := testDispatcher(t, receiver.URL, func(o *Options) { o.Workers = 1; o.QueueSize = 1; o.MaxPayload = 128 })
	d.OnConnected("one")
	<-entered
	d.OnConnected("two")
	d.OnConnected("three")
	d.OnQR("four", string(make([]byte, 200)))
	if stats := d.Stats(); stats.Dropped != 2 || stats.Queued != 1 {
		t.Fatal(stats)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Close(ctx); err == nil {
		t.Fatal("shutdown deadline ignored")
	}
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("worker ignored canceled request")
	}
	d.OnConnected("after-close")
	if d.Stats().Dropped != 3 {
		t.Fatal(d.Stats())
	}
}

func TestRetryAfterIsBounded(t *testing.T) {
	for _, value := range []string{"9999999999999999999", "999999", "-1", "invalid", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)} {
		if wait := retryAfter(value, time.Second); wait < 0 || wait > time.Second {
			t.Fatal(value, wait)
		}
	}
	if _, err := NewWithOptions("file:///tmp/receiver", DefaultOptions()); err == nil {
		t.Fatal("non HTTP URL accepted")
	}
}
