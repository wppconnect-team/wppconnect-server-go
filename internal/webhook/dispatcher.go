// Package webhook forwards normalized session events to the configured webhook
// URL, mirroring the Node server's callWebHook payload shape (an `event` field
// plus the data) so existing webhook consumers keep working.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"

	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow/types/events"
)

// Dispatcher posts events to a webhook URL. It satisfies session.EventSink.
type Dispatcher struct {
	URL       string
	client    *http.Client
	options   Options
	queue     chan delivery
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	done      chan struct{}
	workers   sync.WaitGroup
	delivered atomic.Uint64
	failed    atomic.Uint64
	dropped   atomic.Uint64
	retries   atomic.Uint64
}

type Options struct {
	Workers    int
	QueueSize  int
	Attempts   int
	Timeout    time.Duration
	RetryBase  time.Duration
	RetryMax   time.Duration
	MaxPayload int
}

func DefaultOptions() Options {
	return Options{Workers: 2, QueueSize: 256, Attempts: 4, Timeout: 10 * time.Second, RetryBase: 250 * time.Millisecond, RetryMax: 30 * time.Second, MaxPayload: 1 << 20}
}

type delivery struct {
	id   string
	body []byte
}
type Stats struct {
	Queued                              int
	Delivered, Failed, Dropped, Retries uint64
}

func (d *Dispatcher) Stats() Stats {
	return Stats{Queued: len(d.queue), Delivered: d.delivered.Load(), Failed: d.failed.Load(), Dropped: d.dropped.Load(), Retries: d.retries.Load()}
}

func (d *Dispatcher) OnDirectoryEvent(session string, evt any) {
	if event, data := directory.Notification(evt); data != nil {
		d.post(event, session, data)
	}
}

// OnGroupEvent preserves Node participant names and adds normalized metadata.
func (d *Dispatcher) OnGroupEvent(session string, evt any) {
	for _, notification := range groups.Notifications(evt) {
		d.post(notification.Event, session, notification.Data)
	}
}

// New creates a dispatcher for the given webhook URL (may be empty to disable).
func New(url string) *Dispatcher {
	d, err := NewWithOptions(url, DefaultOptions())
	if err != nil {
		panic(err)
	}
	return d
}

func NewWithOptions(target string, options Options) (*Dispatcher, error) {
	if target != "" {
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("WEBHOOK_URL must be an absolute HTTP(S) URL")
		}
	}
	if options.Workers < 1 || options.Workers > 32 || options.QueueSize < 1 || options.QueueSize > 10000 || options.Attempts < 1 || options.Attempts > 10 || options.Timeout <= 0 || options.RetryBase <= 0 || options.RetryMax < options.RetryBase || options.MaxPayload < 1 || options.MaxPayload > 16<<20 {
		return nil, errors.New("invalid webhook limits")
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{URL: target, options: options, queue: make(chan delivery, options.QueueSize), ctx: ctx, cancel: cancel, done: make(chan struct{}),
		client: &http.Client{Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if target != "" {
		for i := 0; i < options.Workers; i++ {
			d.workers.Add(1)
			go d.worker()
		}
	}
	go func() { d.workers.Wait(); close(d.done) }()
	return d, nil
}

func (d *Dispatcher) post(event, session string, payload map[string]any) {
	if d.URL == "" {
		return
	}
	body := map[string]any{"event": event, "session": session}
	for k, v := range payload {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil || len(b) > d.options.MaxPayload {
		d.dropped.Add(1)
		log.Printf("webhook dropped: serialization or payload limit")
		return
	}
	item := delivery{id: uuid.NewString(), body: b}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		d.dropped.Add(1)
		return
	}
	select {
	case d.queue <- item:
	default:
		d.dropped.Add(1)
		log.Printf("webhook dropped: queue full, id=%s", item.id)
	}
}

func (d *Dispatcher) worker() {
	defer d.workers.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case item, ok := <-d.queue:
			if !ok {
				return
			}
			if d.deliver(item) {
				d.delivered.Add(1)
			} else {
				d.failed.Add(1)
				log.Printf("webhook delivery failed: id=%s", item.id)
			}
		}
	}
}

func (d *Dispatcher) deliver(item delivery) bool {
	for attempt := 1; attempt <= d.options.Attempts; attempt++ {
		if d.ctx.Err() != nil {
			return false
		}
		req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, d.URL, bytes.NewReader(item.body))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-WPP-Event-ID", item.id)
		req.Header.Set("X-WPP-Attempt", strconv.Itoa(attempt))
		resp, err := d.client.Do(req)
		retry, wait := err != nil, time.Duration(0)
		if resp != nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return true
			}
			retry = resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500
			wait = retryAfter(resp.Header.Get("Retry-After"), d.options.RetryMax)
		}
		if !retry || attempt == d.options.Attempts {
			return false
		}
		d.retries.Add(1)
		backoff := min(d.options.RetryMax, d.options.RetryBase*time.Duration(1<<uint(attempt-1)))
		backoff = time.Duration(float64(backoff) * (0.8 + rand.Float64()*0.4))
		wait = min(d.options.RetryMax, max(wait, backoff))
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-d.ctx.Done():
			timer.Stop()
			return false
		}
	}
	return false
}

func retryAfter(value string, maximum time.Duration) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		if int64(seconds) > int64(maximum/time.Second) {
			return maximum
		}
		return min(time.Duration(seconds)*time.Second, maximum)
	}
	if at, err := http.ParseTime(value); err == nil {
		return min(max(time.Until(at), 0), maximum)
	}
	return 0
}

// Close stops producers, drains pending events, and cancels requests on deadline.
func (d *Dispatcher) Close(ctx context.Context) error {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.queue)
	}
	d.mu.Unlock()
	select {
	case <-d.done:
		d.cancel()
		d.client.CloseIdleConnections()
		return nil
	case <-ctx.Done():
		d.cancel()
		d.client.CloseIdleConnections()
		return fmt.Errorf("webhook shutdown: %w", ctx.Err())
	}
}

func (d *Dispatcher) OnStatus(name string, status session.Status) {
	d.post("status-find", name, map[string]any{"status": string(status)})
}

// OnQR forwards a qrcode event (matches Node's "qrcode" event).
func (d *Dispatcher) OnQR(session, code string) {
	d.post("qrcode", session, map[string]any{"urlcode": code})
}

// OnConnected forwards a status-find/connected event.
func (d *Dispatcher) OnConnected(session string) {
	d.post("status-find", session, map[string]any{"status": "CONNECTED"})
}

// OnMessage forwards an incoming message (matches Node's "onmessage" event).
func (d *Dispatcher) OnMessage(session string, msg *events.Message) {
	if payload := media.Event(msg); payload != nil {
		d.post("onmessage", session, payload)
	}
}
