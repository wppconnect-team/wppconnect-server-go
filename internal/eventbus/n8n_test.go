package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestN8NTokenStaysOnConfiguredWorkflow(t *testing.T) {
	token := strings.Repeat("x", 40)
	for _, bad := range []string{"http://external.example/webhook/x", "https://example.com/webhook-test/x", "https://example.com/webhook/x?token=x", "https://person@example.com/webhook/x"} {
		if _, e := NewN8N(bad, token); e == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	var leaked atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-WPP-Workflow-Token") != token || r.Header.Get("X-WPP-Event-ID") != "event-one" {
			t.Error("missing credential or stable event identity")
		}
		http.Redirect(w, r, sink.URL, 307)
	}))
	defer source.Close()
	target, e := NewN8N(source.URL+"/webhook/test", token)
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	if target.Name() != "n8n" || target.Deliver(context.Background(), Event{ID: "event-one", Data: []byte(`{}`)}) == nil || leaked.Load() != 0 {
		t.Fatal("redirected or falsely confirmed workflow")
	}
}
func TestIntegrationN8NReceiver(t *testing.T) {
	url := os.Getenv("INTEGRATION_N8N_URL")
	if url == "" {
		t.Skip("use scripts/test-n8n.sh")
	}
	token := os.Getenv("INTEGRATION_N8N_TOKEN")
	target, e := NewN8N(url, token)
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	event := Event{ID: "n8n-fixture-event", Attempt: 1, Data: []byte(`{"event":"onmessage","session":"fixture"}`)}
	bad, e := NewN8N(url, strings.Repeat("z", 45))
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Close()
	if err := bad.Deliver(ctx, event); err == nil {
		t.Fatal("wrong credential was accepted")
	}
	if err := target.Deliver(ctx, event); err != nil {
		t.Fatal("authenticated workflow delivery failed", err)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(event.Data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WPP-Workflow-Token", token)
	req.Header.Set("X-WPP-Event-ID", event.ID)
	res, e := target.Client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var out map[string]any
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&out) != nil || len(out) != 3 || out["eventId"] != event.ID || out["event"] != "onmessage" || out["session"] != "fixture" {
		t.Fatal("workflow did not return exactly the three projected fields")
	}
}

func TestN8NPairingMaterialNotForwardedEvenOnReplay(t *testing.T) {
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil || len(fields) != 3 || fields["event"] != "qrcode" || fields["session"] != "test" || fields["available"] != true {
			t.Error("unexpected QR projection")
		}
		calls.Add(1)
	}))
	defer receiver.Close()
	dest, err := NewN8N(receiver.URL+"/webhook/test", strings.Repeat("x", 45))
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	qr := Event{Name: "qrcode", Session: "test", ID: "stable-id", Data: []byte(`{"event":"qrcode","urlcode":"private-test","futureField":"private-too"}`)}
	for i := 1; i <= 2; i++ {
		qr.Attempt = i
		if e := dest.Deliver(context.Background(), qr); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 2 || !bytes.Contains(qr.Data, []byte("private-test")) {
		t.Fatal("source event or calls changed")
	}
}
