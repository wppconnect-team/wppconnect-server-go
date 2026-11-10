package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdempotencyCrashAndCanceledHTTPDoNotRepeatProtocolWork(t *testing.T) {
	for _, mode := range []string{"panic", "cancel", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			store, err := sendledger.Open(filepath.Join(t.TempDir(), "sends.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			s := &Server{sends: store, sendSlots: make(chan struct{}, 4)}
			calls := 0
			invoke := func() *httptest.ResponseRecorder {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				route := chi.NewRouteContext()
				route.URLParams.Add("session", "alpha")
				r := httptest.NewRequest("POST", "/api/alpha/send-message", strings.NewReader(`{"message":"one"}`)).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", "same")
				w := httptest.NewRecorder()
				handler := s.idempotency(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					switch mode {
					case "panic":
						panic("simulated crash after protocol send")
					case "cancel":
						cancel()
					case "overflow":
						w.Write([]byte(strings.Repeat("x", sendledger.MaxResponse+1)))
						return
					}
					writeJSON(w, 201, map[string]any{"id": "sent-once"})
				}))
				func() {
					defer func() {
						if recovered := recover(); recovered != nil && mode != "panic" {
							t.Fatal(recovered)
						}
					}()
					handler.ServeHTTP(w, r)
				}()
				return w
			}
			first := invoke()
			again := invoke()
			if calls != 1 {
				t.Fatal("protocol side effect repeated", calls)
			}
			if mode == "cancel" {
				if first.Code != 201 || again.Code != 201 || again.Header().Get("Idempotency-Replayed") != "true" {
					t.Fatal("cancellation lost response", first.Code, again.Code)
				}
			} else if again.Code != 409 || !strings.Contains(again.Body.String(), "SEND_PENDING_OR_UNKNOWN") {
				t.Fatal("uncertain result retried", again.Body.String())
			}
		})
	}
}

func TestIdempotentSendRecoveryConflictAuthenticationAndIsolation(t *testing.T) {
	dbpath := filepath.Join(t.TempDir(), "sends.db")
	ledger, err := sendledger.Open(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ledger.Close() }()
	client := &fakeMediaClient{}
	stores := map[string]*media.Store{"alpha": media.NewStore(), "beta": media.NewStore()}
	tokens := map[string]string{}
	for name := range stores {
		tokens[name], err = auth.Generate(name, "test-secret")
		if err != nil {
			t.Fatal(err)
		}
	}
	server := func() http.Handler {
		return (&Server{cfg: config.Config{SecretKey: "test-secret"}, sends: ledger, mediaSession: func(name string) (media.Client, *media.Store, bool) {
			store, ok := stores[name]
			return client, store, ok
		}}).router()
	}
	router := server()
	call := func(method, name, route, key, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/"+name+"/"+route, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	body := `{"phone":"123","message":"one"}`
	first := call("POST", "alpha", "send-message", "key", tokens["alpha"], body)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	ledger.Close()
	ledger, err = sendledger.Open(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	router = server()
	repeat := call("POST", "alpha", "send-message", "key", tokens["alpha"], body)
	if repeat.Code != first.Code || repeat.Body.String() != first.Body.String() || repeat.Header().Get("Idempotency-Replayed") != "true" || len(client.sent) != 1 {
		t.Fatal("send repeated or result lost", repeat.Body.String(), len(client.sent))
	}
	conflict := call("POST", "alpha", "send-message", "key", tokens["alpha"], strings.Replace(body, "one", "two", 1))
	if conflict.Code != 409 || len(client.sent) != 1 {
		t.Fatal(conflict.Code)
	}
	if w := call("GET", "alpha", "send-result", "key", tokens["beta"], ""); w.Code != 401 {
		t.Fatal("other token read result", w.Code)
	}
	if w := call("GET", "beta", "send-result", "key", tokens["beta"], ""); w.Code != 404 {
		t.Fatal("cross-session result", w.Code)
	}
	recovered := call("GET", "alpha", "send-result", "key", tokens["alpha"], "")
	var result map[string]any
	json.Unmarshal(recovered.Body.Bytes(), &result)
	if recovered.Code != 200 || result["state"] != "complete" || result["httpStatus"] != float64(201) {
		t.Fatal(recovered.Body.String())
	}
	if w := call("POST", "beta", "send-message", "key", tokens["beta"], body); w.Code != 201 || len(client.sent) != 2 {
		t.Fatal("independent tenant denied", w.Code)
	}
	client.sendErr = context.DeadlineExceeded
	timeout := call("POST", "alpha", "send-message", "ambiguous", tokens["alpha"], body)
	if timeout.Code != 504 {
		t.Fatal(timeout.Code)
	}
	client.sendErr = nil
	again := call("POST", "alpha", "send-message", "ambiguous", tokens["alpha"], body)
	if again.Code != 504 || len(client.sent) != 3 {
		t.Fatal("ambiguous send repeated", again.Code, len(client.sent))
	}
	if w := call("POST", "alpha", "send-message", "invalid key", tokens["alpha"], body); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "alpha", "close-session", "key", tokens["alpha"], "{}"); w.Code != 400 {
		t.Fatal("unsupported key ignored", w.Code)
	}
	ledger.Close()
	if w := call("POST", "alpha", "send-message", "storage-failed", tokens["alpha"], body); w.Code != 503 || len(client.sent) != 3 {
		t.Fatal("sent without durable reservation", w.Code)
	}
}
func TestIdempotentMediaDoesNotUploadOrSendTwice(t *testing.T) {
	store, err := sendledger.Open(filepath.Join(t.TempDir(), "sends.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token, err := auth.Generate("alpha", "secret")
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeMediaClient{}
	mediaStore := media.NewStore()
	s := &Server{cfg: config.Config{SecretKey: "secret"}, sends: store, mediaSession: func(string) (media.Client, *media.Store, bool) { return client, mediaStore, true }}
	router := s.router()
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/api/alpha/send-file", strings.NewReader(`{"phone":"123","base64":"aGk=","filename":"hi.txt"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", "media-1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if client.uploads != 1 || len(client.sent) != 1 {
		t.Fatal("duplicated media side effects")
	}
}
func TestPendingResultIsNeverReexecuted(t *testing.T) {
	store, err := sendledger.Open(filepath.Join(t.TempDir(), "sends.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token, _ := auth.Generate("alpha", "secret")
	_, _, err = store.Claim(context.Background(), "alpha", keyHash("pending"), "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{SecretKey: "secret"}, sends: store}
	r := httptest.NewRequest("GET", "/api/alpha/send-result", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", "pending")
	w := httptest.NewRecorder()
	s.router().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "pending_or_unknown") {
		t.Fatal(w.Body.String())
	}
	if _, _, err = store.Claim(context.Background(), "alpha", keyHash("pending"), "different"); !errors.Is(err, sendledger.ErrConflict) {
		t.Fatal(err)
	}
}
