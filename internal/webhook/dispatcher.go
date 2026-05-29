// Package webhook forwards normalized session events to the configured webhook
// URL, mirroring the Node server's callWebHook payload shape (an `event` field
// plus the data) so existing webhook consumers keep working.
package webhook

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

// Dispatcher posts events to a webhook URL. It satisfies session.EventSink.
type Dispatcher struct {
	URL    string
	client *http.Client
}

// New creates a dispatcher for the given webhook URL (may be empty to disable).
func New(url string) *Dispatcher {
	return &Dispatcher{URL: url, client: &http.Client{Timeout: 10 * time.Second}}
}

func (d *Dispatcher) post(event, session string, payload map[string]any) {
	if d.URL == "" {
		return
	}
	body := map[string]any{"event": event, "session": session}
	for k, v := range payload {
		body[k] = v
	}
	b, _ := json.Marshal(body)
	go func() {
		resp, err := d.client.Post(d.URL, "application/json", bytes.NewReader(b))
		if err != nil {
			log.Printf("webhook error: %v", err)
			return
		}
		resp.Body.Close()
	}()
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
	d.post("onmessage", session, map[string]any{
		"from": msg.Info.Sender.String(),
		"body": msg.Message.GetConversation(),
		"id":   msg.Info.ID,
	})
}
