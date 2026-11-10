package httpapi

import (
	"context"
	"encoding/json"
	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/eventbus"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliveryOperationsAuthenticateFilterReplayAndAudit(t *testing.T) {
	var accepts atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accepts.Load() {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(400)
		}
	}))
	defer receiver.Close()
	target, err := eventbus.NewHTTP(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	store, err := eventbus.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	bus, err := eventbus.New(store, []eventbus.Target{target}, eventbus.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := bus.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err = bus.Publish("onmessage", "alpha", map[string]any{"secretMessage": "must-not-be-exposed"}); err != nil {
		t.Fatal(err)
	}
	var failures []eventbus.DeliveryStatus
	for i := 0; i < 100; i++ {
		failures, err = store.Deliveries(context.Background(), "alpha", "webhook", "failed", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(failures) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(failures) != 1 || failures[0].Failure != "permanent_rejection" {
		t.Fatal(failures)
	}
	alpha, _ := auth.Generate("alpha", "secret")
	beta, _ := auth.Generate("beta", "secret")
	router := (&Server{cfg: config.Config{SecretKey: "secret", AdminToken: strings.Repeat("a", 32)}, events: bus}).router()
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	path := "/api/alpha/events/deliveries?target=webhook"
	if w := call("GET", path, beta, ""); w.Code != 401 {
		t.Fatal("authentication bypass", w.Code)
	}
	w := call("GET", path, alpha, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "must-not-be-exposed") || !strings.Contains(w.Body.String(), "permanent_rejection") {
		t.Fatal(w.Body.String())
	}
	if w = call("GET", "/api/alpha/events/deliveries?target=private-url", alpha, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	body, _ := json.Marshal(failedReplayRequest{EventID: failures[0].ID, Target: "webhook"})
	if w = call("POST", "/api/beta/events/retry-failed", beta, string(body)); w.Code != 404 {
		t.Fatal("cross-session replay", w.Code)
	}
	accepts.Store(true)
	if w = call("POST", "/api/alpha/events/retry-failed", alpha, string(body)); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("POST", "/api/alpha/events/retry-failed", alpha, string(body)); w.Code != 409 {
		t.Fatal("successful or pending replayed", w.Code)
	}
	w = call("GET", "/api/alpha/events/replay-audit", alpha, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "session-token") || !strings.Contains(w.Body.String(), "failed-only") {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/metrics", strings.Repeat("a", 32), "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "wppconnect_event_log_max_bytes") || strings.Contains(w.Body.String(), "must-not-be-exposed") {
		t.Fatal(w.Code, w.Body.String())
	}
}
