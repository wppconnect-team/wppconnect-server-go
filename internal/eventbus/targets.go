package eventbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	amqp "github.com/rabbitmq/amqp091-go"
)

type HTTP struct {
	URL    string
	Client *http.Client
}

func NewHTTP(target string) (*HTTP, error) {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("invalid webhook URL")
	}
	return &HTTP{URL: target, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (h *HTTP) Name() string { return "webhook" }
func (h *HTTP) Close() error { h.Client.CloseIdleConnections(); return nil }
func (h *HTTP) Deliver(ctx context.Context, e Event) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(e.Data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WPP-Event-ID", e.ID)
	req.Header.Set("X-WPP-Attempt", strconv.Itoa(e.Attempt))
	resp, err := h.Client.Do(req)
	if err != nil {
		return errors.New("webhook transport failed")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	retry := resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500
	after := time.Duration(0)
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		after = time.Duration(min(seconds, 30)) * time.Second
	} else if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		after = max(0, min(30*time.Second, time.Until(at)))
	}
	return &DeliveryError{Permanent: !retry, After: after}
}

type NATS struct {
	URL, Subject string
	mu           sync.Mutex
	conn         *nats.Conn
}

func (n *NATS) Name() string { return "nats" }
func (n *NATS) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn != nil {
		n.conn.Close()
		n.conn = nil
	}
	return nil
}
func (n *NATS) Deliver(ctx context.Context, e Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.conn == nil || n.conn.IsClosed() {
		var err error
		timeout := 3 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			timeout = min(timeout, time.Until(deadline))
		}
		n.conn, err = nats.Connect(n.URL, nats.Timeout(timeout), nats.MaxReconnects(0))
		if err != nil {
			return errors.New("NATS connection failed")
		}
	}
	prefix := n.Subject
	if prefix == "" {
		prefix = "wppconnect.events"
	}
	msg := nats.NewMsg(prefix + "." + e.Session + "." + e.Name)
	msg.Data = e.Data
	msg.Header.Set("WPP-Event-ID", e.ID)
	if err := n.conn.PublishMsg(msg); err != nil {
		n.conn.Close()
		n.conn = nil
		return errors.New("NATS publish failed")
	}
	if err := n.conn.FlushWithContext(ctx); err != nil {
		n.conn.Close()
		n.conn = nil
		return errors.New("NATS flush failed")
	}
	return nil
}

type RabbitMQ struct {
	URL, Exchange, Queue string
	mu                   sync.Mutex
	conn                 *amqp.Connection
	channel              *amqp.Channel
	wire                 net.Conn
}

func (r *RabbitMQ) Name() string { return "rabbitmq" }
func (r *RabbitMQ) close() {
	// Closing the transport unblocks pending RPCs even if the broker stops responding.
	if r.wire != nil {
		r.wire.Close()
	}
	r.channel, r.conn, r.wire = nil, nil, nil
}
func (r *RabbitMQ) Close() error { r.mu.Lock(); defer r.mu.Unlock(); r.close(); return nil }
func (r *RabbitMQ) Deliver(ctx context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	if r.wire != nil {
		r.wire.SetDeadline(deadline)
	}
	defer func() {
		if r.wire != nil {
			r.wire.SetDeadline(time.Time{})
		}
	}()
	exchange, queue := r.Exchange, r.Queue
	if exchange == "" {
		exchange = "wppconnect.events"
	}
	if queue == "" {
		queue = "wppconnect.events"
	}
	if r.conn == nil || r.conn.IsClosed() || r.channel == nil || r.channel.IsClosed() {
		r.close()
		var err error
		r.conn, err = amqp.DialConfig(r.URL, amqp.Config{Dial: func(network, address string) (net.Conn, error) {
			wire, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
			if err == nil {
				wire.SetDeadline(deadline)
				r.wire = wire
			}
			return wire, err
		}})
		if err != nil {
			r.close()
			return errors.New("RabbitMQ connection failed")
		}
		r.channel, err = r.conn.Channel()
		if err != nil {
			r.close()
			return errors.New("RabbitMQ channel failed")
		}
		if err = r.channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			r.close()
			return errors.New("RabbitMQ exchange declaration failed")
		}
		if _, err = r.channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
			r.close()
			return errors.New("RabbitMQ queue declaration failed")
		}
		if err = r.channel.QueueBind(queue, "#", exchange, false, nil); err != nil {
			r.close()
			return errors.New("RabbitMQ binding failed")
		}
		if err = r.channel.Confirm(false); err != nil {
			r.close()
			return errors.New("RabbitMQ publisher confirms failed")
		}
	}
	confirm, err := r.channel.PublishWithDeferredConfirmWithContext(ctx, exchange, e.Session+"."+e.Name, false, false, amqp.Publishing{DeliveryMode: amqp.Persistent, ContentType: "application/json", MessageId: e.ID, Type: e.Name, Timestamp: time.UnixMilli(e.Created), Body: e.Data})
	if err != nil {
		r.close()
		return errors.New("RabbitMQ publish failed")
	}
	acked, err := confirm.WaitContext(ctx)
	if err != nil || !acked {
		r.close()
		return errors.New("RabbitMQ publish not confirmed")
	}
	return nil
}

func ValidateBrokerURL(value string, schemes ...string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return errors.New("invalid broker URL")
	}
	for _, scheme := range schemes {
		if u.Scheme == scheme {
			return nil
		}
	}
	return errors.New("unsupported broker URL scheme")
}
func ValidSubject(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, " \t\r\n*>/") && !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".") && !strings.Contains(value, "..")
}
