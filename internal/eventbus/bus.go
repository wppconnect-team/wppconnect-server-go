package eventbus

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/notifications"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow/types/events"
)

type Target interface {
	Name() string
	Deliver(context.Context, Event) error
	Close() error
}
type DeliveryError struct {
	Permanent bool
	After     time.Duration
}

func (e *DeliveryError) Error() string { return "event target rejected delivery" }

type Options struct {
	Workers, Attempts  int
	Timeout, RetryBase time.Duration
}

func DefaultOptions() Options { return Options{2, 4, 10 * time.Second, 250 * time.Millisecond} }

type Bus struct {
	Log       *Log
	targets   map[string]Target
	names     []string
	options   Options
	ctx       context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
	closing   atomic.Bool
	rejected  atomic.Int64
	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

func New(store *Log, targets []Target, options Options) (*Bus, error) {
	if store == nil || options.Workers < 1 || options.Workers > 32 || options.Attempts < 1 || options.Attempts > 10 || options.Timeout <= 0 || options.Timeout > time.Minute || options.RetryBase <= 0 {
		return nil, errors.New("invalid event bus options")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bus{Log: store, targets: map[string]Target{}, options: options, ctx: ctx, cancel: cancel}
	for _, target := range targets {
		if target.Name() == "" || b.targets[target.Name()] != nil {
			cancel()
			return nil, errors.New("duplicate event target")
		}
		b.targets[target.Name()] = target
		b.names = append(b.names, target.Name())
	}
	for i := 0; i < options.Workers; i++ {
		b.workers.Add(1)
		go b.worker(i)
	}
	return b, nil
}
func (b *Bus) Publish(event, name string, data map[string]any) error {
	if b.closing.Load() {
		return errors.New("event bus is closing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := b.Log.Append(ctx, event, name, data, b.names)
	if err != nil {
		b.rejected.Add(1)
	}
	return err
}
func (b *Bus) post(event, name string, data map[string]any) {
	if data == nil {
		return
	}
	if err := b.Publish(event, name, data); err != nil {
		log.Printf("event not persisted: %v", err)
	}
}
func (b *Bus) OnQR(name, code string) { b.post("qrcode", name, map[string]any{"urlcode": code}) }
func (b *Bus) OnConnected(name string) {
	b.post("status-find", name, map[string]any{"status": "CONNECTED"})
}
func (b *Bus) OnStatus(name string, status session.Status) {
	b.post("status-find", name, map[string]any{"status": status})
}
func (b *Bus) OnMessage(name string, msg *events.Message) {
	b.post("onmessage", name, media.Event(msg))
}
func (b *Bus) OnGroupEvent(name string, evt any) {
	for _, event := range groups.Notifications(evt) {
		b.post(event.Event, name, event.Data)
	}
}
func (b *Bus) OnDirectoryEvent(name string, evt any) {
	event, data := directory.Notification(evt)
	if data != nil {
		b.post(event, name, data)
	}
}
func (b *Bus) OnProtocolEvent(name string, evt any) {
	event, data := notifications.Protocol(evt)
	if data != nil {
		b.post(event, name, data)
	}
}
func (b *Bus) Targets() []string     { return append([]string{}, b.names...) }
func (b *Bus) Rejected() int64       { return b.rejected.Load() }
func (b *Bus) Done() <-chan struct{} { return b.ctx.Done() }
func (b *Bus) worker(offset int) {
	defer b.workers.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	names := append([]string{}, b.names...)
	if len(names) > 0 {
		n := offset % len(names)
		names = append(names[n:], names[:n]...)
	}
	for {
		if b.ctx.Err() != nil {
			return
		}
		d, err := b.Log.claim(b.ctx, names, b.options.Timeout+15*time.Second)
		if errors.Is(err, sql.ErrNoRows) || err != nil {
			select {
			case <-b.ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}
		ctx, cancel := context.WithTimeout(b.ctx, b.options.Timeout)
		d.Event.Attempt = d.Attempt + 1
		err = b.targets[d.Target].Deliver(ctx, d.Event)
		cancel()
		state := "delivered"
		next := time.Now()
		if err != nil {
			state = "pending"
			delay := min(30*time.Second, b.options.RetryBase*time.Duration(1<<min(d.Attempt, 7)))
			var targetError *DeliveryError
			if errors.As(err, &targetError) {
				if targetError.Permanent {
					state = "failed"
				}
				delay = max(delay, min(30*time.Second, targetError.After))
			}
			if d.Attempt+1 >= b.options.Attempts {
				state = "failed"
			}
			next = next.Add(delay)
		}
		// Canceled shutdown leaves the lease recoverable instead of consuming retry budget.
		if b.ctx.Err() != nil {
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		err = b.Log.finish(ctx, d, state, next)
		cancel()
		if err != nil {
			log.Printf("event acknowledgement persistence failed: %v", err)
		}
		if len(names) > 1 {
			names = append(names[1:], names[0])
		}
	}
}
func (b *Bus) Close(ctx context.Context) error {
	b.closing.Store(true)
	for len(b.names) > 0 {
		pending, err := b.Log.pending(ctx, b.names)
		if err != nil {
			break
		}
		if pending == 0 {
			break
		}
		select {
		case <-ctx.Done():
			goto stop
		case <-time.After(100 * time.Millisecond):
		}
	}
stop:
	b.cancel()
	b.closeOnce.Do(func() {
		b.closed = make(chan struct{})
		go func() {
			b.workers.Wait()
			for _, target := range b.targets {
				b.closeErr = errors.Join(b.closeErr, target.Close())
			}
			b.closeErr = errors.Join(b.closeErr, b.Log.Close())
			close(b.closed)
		}()
	})
	select {
	case <-b.closed:
		return b.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
