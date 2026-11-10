package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const incomingJSON = `{"event":"onmessage","session":"demo","id":"test-wa-id","chatId":"19995550101@s.whatsapp.net","fromMe":false,"isGroupMsg":false,"isMedia":false,"type":"chat","persistentText":true,"body":"original incoming"}`

type cwFixture struct {
	mu                                      sync.Mutex
	missingResponse, wrongContact, unlisted bool
	posts, reads                            int
	saved                                   map[string]any
}

func (f *cwFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("api_access_token") != "local-token" {
		w.WriteHeader(401)
		return
	}
	base := "/api/v1/accounts/1/conversations/3"
	switch {
	case r.Method == "GET" && r.URL.Path == base:
		contact := 4
		if f.wrongContact {
			contact = 5
		}
		fmt.Fprintf(w, `{"id":3,"account_id":1,"inbox_id":2,"meta":{"channel":"Channel::Api","sender":{"id":%d}}}`, contact)
	case r.Method == "POST" && r.URL.Path == base+"/messages":
		var msg map[string]any
		if json.NewDecoder(r.Body).Decode(&msg) != nil || msg["content"] != "original incoming" || msg["message_type"] != "incoming" || msg["private"] != false || !strings.HasPrefix(fmt.Sprint(msg["source_id"]), "wpp:") {
			w.WriteHeader(422)
			return
		}
		f.posts++
		f.saved = map[string]any{"id": 9, "inbox_id": 2, "conversation_id": 3, "message_type": 0, "content": msg["content"], "private": false, "source_id": msg["source_id"]}
		if f.missingResponse {
			w.WriteHeader(502)
			return
		}
		json.NewEncoder(w).Encode(f.saved)
	case r.Method == "GET" && r.URL.Path == base+"/messages":
		f.reads++
		p := []map[string]any{}
		if !f.unlisted && f.saved != nil {
			p = append(p, f.saved)
		}
		json.NewEncoder(w).Encode(map[string]any{"payload": p})
	default:
		w.WriteHeader(404)
	}
}
func incomingFixture(t *testing.T, f *cwFixture) (*incomingHandler, string) {
	t.Helper()
	a, path := fixture(t, f.serve)
	h, err := newIncoming(a, strings.TrimSuffix(a.endpoint, "/send-text")+"/api/v1/accounts/1/conversations/3", "local-token", "local-signing-key", "777@lid")
	if err != nil {
		t.Fatal(err)
	}
	return h, path
}
func submitWPP(h *incomingHandler, raw, event, key string) int {
	r := httptest.NewRequest("POST", "/wpp", strings.NewReader(raw))
	mac := hmac.New(sha256.New, []byte(key))
	fmt.Fprint(mac, "1000."+event+".1."+raw)
	for k, v := range map[string]string{"X-WPP-Timestamp": "1000", "X-WPP-Event-ID": event, "X-WPP-Attempt": "1", "X-WPP-Signature": "v1=" + hex.EncodeToString(mac.Sum(nil))} {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}
func TestIncomingTextRecoversFromChatwootBySourceAfterRestart(t *testing.T) {
	f := &cwFixture{missingResponse: true, unlisted: true}
	h, path := incomingFixture(t, f)
	post := func(event string) int { return submitWPP(h, incomingJSON, event, h.secret) }
	if code := post("event1"); code != 503 {
		t.Fatal(code)
	}
	h.relay.db.Close()
	var err error
	h.relay.db, err = state(path, h.relay.bound)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newIncoming(h.relay, h.conversationURL+"other", h.token, h.secret, h.lid); err == nil {
		t.Fatal("rebound Chatwoot destination")
	}
	if code := post("event2"); code != 503 || f.posts != 1 || f.reads != 1 {
		t.Fatal(code, f.posts, f.reads)
	}
	f.unlisted = false
	if code := post("event3"); code != 204 || f.posts != 1 || f.reads != 2 {
		t.Fatal(code, f.posts, f.reads)
	}
	if code := post("event4"); code != 204 || f.posts != 1 || f.reads != 2 {
		t.Fatal(code, f.posts, f.reads)
	}
}
func TestIncomingRequiresExactPeerPublicTextAndCurrentContact(t *testing.T) {
	f := &cwFixture{wrongContact: true}
	h, _ := incomingFixture(t, f)
	if submitWPP(h, incomingJSON, "e", "wrong") != 401 {
		t.Fatal("signature")
	}
	for _, pair := range [][2]string{{"demo", "other"}, {"onmessage", "qrcode"}, {"19995550101@s.whatsapp.net", "unbound@lid"}, {`"persistentText":true,`, ""}, {`"fromMe":false`, `"fromMe":true`}, {`"isGroupMsg":false`, `"isGroupMsg":true`}, {`"isMedia":false`, `"isMedia":true`}, {`"fromMe":false,`, ""}} {
		if code := submitWPP(h, strings.Replace(incomingJSON, pair[0], pair[1], 1), "e", h.secret); code != 204 {
			t.Fatal(pair, code)
		}
	}
	if code := submitWPP(h, incomingJSON, "e", h.secret); code != 503 || f.posts != 0 {
		t.Fatal("changed conversation", code, f.posts)
	}
	f.wrongContact = false
	if code := submitWPP(h, strings.Replace(incomingJSON, "19995550101@s.whatsapp.net", "777@lid", 1), "e", h.secret); code != 204 || f.posts != 1 {
		t.Fatal("bound LID", code, f.posts)
	}
}
func TestConcurrentIncomingCreatesOneConversationMessage(t *testing.T) {
	f := &cwFixture{}
	h, _ := incomingFixture(t, f)
	var wg sync.WaitGroup
	var unexpected atomic.Bool
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := submitWPP(h, incomingJSON, fmt.Sprint(i), h.secret)
			if c != 503 && c != 204 {
				unexpected.Store(true)
			}
		}(i)
	}
	wg.Wait()
	if unexpected.Load() || f.posts != 1 {
		t.Fatal("duplicate post or unexpected response", f.posts)
	}
}
func TestOptionalIncomingConfiguration(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected HTTP") })
	if h, e := configureIncoming(a, "", "", "", ""); h != nil || e != nil {
		t.Fatal("empty inbound configuration")
	}
	for _, url := range []string{"http://untrusted.invalid", "https://example.invalid?redirect=1", "https://account@example.invalid", "https://example.invalid/api", ""} {
		if h, e := configureIncoming(a, url, "token", strings.Repeat("x", 32), ""); h != nil || e == nil {
			t.Fatal("accepted invalid origin")
		}
	}
	if _, e := configureIncoming(a, "https://example.invalid", "token", strings.Repeat("x", 32), "wrong"); e == nil {
		t.Fatal("invalid LID")
	}

}
