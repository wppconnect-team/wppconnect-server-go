package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const msgJSON = `{"event":"message_created","id":113,"content":"hello world","content_type":"text","message_type":"outgoing","private":false,"source_id":null,"sender":{"type":"user","id":7},"account":{"id":1},"inbox":{"id":2},"conversation":{"id":3,"inbox_id":2,"account":{"id":1},"meta":{"sender":{"id":4}}}}`
const acceptedJSON = `{"status":"success","response":[{"id":"protocol-id","type":"chat"}]}`

func fixture(t *testing.T, upstream http.HandlerFunc) (*relay, string) {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "relay.db")
	b := binding{1, 2, 3, 4, "demo", "19995550101"}
	db, e := state(path, b)
	if e != nil {
		t.Fatal(e)
	}
	a := &relay{b, "test-signing-key", "test-cloud-key", server.URL + "/send-text", db, server.Client(), func() time.Time { return time.Unix(1000, 0) }, make(chan struct{}, 8)}
	t.Cleanup(func() { a.db.Close() })
	return a, path
}
func apply(a *relay, body string, ts int, secret string) int {
	r := httptest.NewRequest("POST", "/chatwoot", bytes.NewBufferString(body))
	stamp := strconv.Itoa(ts)
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprint(h, stamp+"."+body)
	r.Header.Set("X-Chatwoot-Timestamp", stamp)
	r.Header.Set("X-Chatwoot-Signature", "sha256="+hex.EncodeToString(h.Sum(nil)))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w.Code
}
func TestSignedPublicAgentReplyUsesBoundPhoneAndDeduplicatesAcrossRestart(t *testing.T) {
	var posts atomic.Int32
	a, path := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/send-text" || r.Header.Get("Idempotency-Key") != "cw:1:2:3:113" || r.Header.Get("Authorization") != "Bearer test-cloud-key" {
			t.Error("unexpected operation")
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["phone"] != "19995550101" || body["message"] != "hello world" || len(body) != 2 {
			t.Error("wrong recipient or content")
		}
		posts.Add(1)
		w.WriteHeader(201)
		fmt.Fprint(w, acceptedJSON)
	})
	body := msgJSON[:len(msgJSON)-1] + `,"phone":"untrusted-input"}`
	if code := apply(a, body, 1000, a.secret); code != 204 {
		t.Fatal(code)
	}
	if e := a.db.Close(); e != nil {
		t.Fatal(e)
	}
	changed := a.bound
	changed.Phone = "19995550102"
	if db, e := state(path, changed); e == nil {
		db.Close()
		t.Fatal("rebound existing history")
	}
	var e error
	a.db, e = state(path, a.bound)
	if e != nil {
		t.Fatal(e)
	}
	if code := apply(a, body, 1001, a.secret); code != 204 || posts.Load() != 1 {
		t.Fatalf("%d, %d posts", code, posts.Load())
	}
}
func TestIgnoresNotesEchoesAndUnboundConversations(t *testing.T) {
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	if code := apply(a, msgJSON, 699, a.secret); code != 401 {
		t.Fatal(code)
	}
	if code := apply(a, msgJSON, 1000, "unrelated"); code != 401 {
		t.Fatal(code)
	}
	for _, pair := range [][2]string{
		{`"private":false`, `"private":true`}, {`"private":false,`, ""}, {`"outgoing"`, `"incoming"`}, {`"text"`, `"template"`}, {`"message_created"`, `"message_updated"`}, {`"source_id":null`, `"source_id":"echo"`}, {`"type":"user"`, `"type":"agent_bot"`}, {`"id":1}`, `"id":9}`}, {`"id":2}`, `"id":9}`}, {`"id":3,`, `"id":9,`}, {`"id":4}`, `"id":9}`}, {`"inbox_id":2`, `"inbox_id":8`}, {`"private":false`, `"private":false,"attachments":[{"id":1}]`},
	} {
		body := string(bytes.ReplaceAll([]byte(msgJSON), []byte(pair[0]), []byte(pair[1])))
		if body == msgJSON {
			t.Fatal("invalid fixture")
		}
		if code := apply(a, body, 1000, a.secret); code != 204 {
			t.Fatalf("%s: %d", pair[0], code)
		}
	}
}
func TestLostAckRecoversByReadingOriginalResultOnly(t *testing.T) {
	var posts, reads atomic.Int32
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "cw:1:2:3:113" {
			t.Error("key changed")
		}
		if r.Method == "POST" {
			posts.Add(1)
			w.WriteHeader(502)
			return
		}
		if r.Method != "GET" || r.URL.Path != "/send-result" {
			t.Error("unexpected recovery request")
		}
		switch reads.Add(1) {
		case 1:
			w.WriteHeader(404)
		case 2:
			fmt.Fprint(w, `{"status":"success","state":"pending_or_unknown","httpStatus":0}`)
		case 3:
			fmt.Fprint(w, `{"status":"success","state":"complete","httpStatus":201,"response":{"status":"success","response":[]}}`)
		default:
			fmt.Fprintf(w, `{"status":"success","state":"complete","httpStatus":201,"response":%s}`, acceptedJSON)
		}
	})
	for _, want := range []int{503, 503, 503, 503, 204, 204} {
		if got := apply(a, msgJSON, 1000, a.secret); got != want {
			t.Fatalf("want %d got %d", want, got)
		}
	}
	if posts.Load() != 1 || reads.Load() != 4 {
		t.Fatal(posts.Load(), reads.Load())
	}
}
func TestRacingCallbacksMakeOnePost(t *testing.T) {
	var posts atomic.Int32
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(404)
			return
		}
		posts.Add(1)
		w.WriteHeader(201)
		fmt.Fprint(w, acceptedJSON)
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code := apply(a, msgJSON, 1000, a.secret)
			if code != 204 && code != 503 {
				t.Errorf("unexpected status %d", code)
			}
		}()
	}
	wg.Wait()
	if posts.Load() != 1 || apply(a, msgJSON, 1000, a.secret) != 204 {
		t.Fatal("duplicate dispatch")
	}
}
