package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestChatwootAPIProvision(t *testing.T) {
	path := os.Getenv("CHATWOOT_INBOX_FIXTURE")
	if path == "" {
		t.Skip("requires the isolated Chatwoot API inbox")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Account, Inbox int64
		Token          string
	}
	if json.Unmarshal(raw, &cfg) != nil || cfg.Token == "" {
		t.Fatal("invalid Rails configuration")
	}
	var mu sync.Mutex
	posts := map[string]int{}
	var loseRead bool
	// Rails commits each POST. The proxy drops its response and the next read,
	// forcing the next service invocation to reconcile using persistent state.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if loseRead && r.Method == "GET" {
			loseRead = false
			w.WriteHeader(502)
			return
		}
		u := "http://127.0.0.1:3000" + r.URL.RequestURI()
		req, err := http.NewRequestWithContext(r.Context(), r.Method, u, r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		req.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		if r.Method == "POST" {
			posts[r.URL.Path]++
			if resp.StatusCode != 200 && resp.StatusCode != 201 {
				t.Errorf("Rails rejected %s: %d", r.URL.Path, resp.StatusCode)
			}
			loseRead = true
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()
	testRoute := binding{Account: cfg.Account, Inbox: cfg.Inbox, Session: "test-auto", Phone: "12025550123"}
	dir := t.TempDir()
	run := func() (binding, error) {
		return resolveRoute(t.Context(), dir, proxy.URL, cfg.Token, proxy.Client(), testRoute)
	}
	for i := 0; i < 2; i++ {
		if _, err := run(); err == nil {
			t.Fatalf("lost write %d returned success", i)
		}
	}
	recovered, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Contact <= 0 || recovered.Conversation <= 0 {
		t.Fatal("missing remote IDs")
	}
	again, err := run()
	if err != nil || again != recovered {
		t.Fatal("binding changed after restart", err)
	}
	root := fmt.Sprintf("/api/v1/accounts/%d", cfg.Account)
	mu.Lock()
	if len(posts) != 2 || posts[root+"/contacts"] != 1 || posts[root+"/conversations"] != 1 {
		t.Fatalf("duplicate create: %v", posts)
	}
	mu.Unlock()
	// A pre-existing contact can be linked to this API inbox without creating a
	// second contact or changing its name/identifier.
	var out struct {
		Payload struct{ Contact contactRecord }
	}
	existing := testRoute
	existing.Phone = "12025550124"
	body, _ := json.Marshal(map[string]string{"name": "Existing contact", "phone_number": "+" + existing.Phone})
	api := "http://127.0.0.1:3000" + root
	if !chatwootRequest(t.Context(), http.DefaultClient, cfg.Token, "POST", api+"/contacts", body, &out) || out.Payload.Contact.ID <= 0 {
		t.Fatal("unable to seed existing contact")
	}
	existingDir := t.TempDir()
	var bound binding
	for i := 0; i < 3; i++ {
		bound, err = resolveRoute(t.Context(), existingDir, proxy.URL, cfg.Token, proxy.Client(), existing)
		if err == nil {
			break
		}
	}
	if err != nil || bound.Contact != out.Payload.Contact.ID || bound.Conversation <= 0 {
		t.Fatal("existing contact not linked", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts[root+"/contacts"] != 1 || posts[root+"/conversations"] != 2 || posts[fmt.Sprintf("%s/contacts/%d/contact_inboxes", root, bound.Contact)] != 1 {
		t.Fatal("unexpected link or create count", posts)
	}
}

func TestProvisionRejectsBlockedAmbiguousOrUnscopedContact(t *testing.T) {
	for _, tc := range []struct{ name, search string }{
		{"blocked", `{"payload":[{"id":3,"blocked":true,"phone_number":"+12025550123"}],"meta":{"has_more":false}}`},
		{"two-matches", `{"payload":[{"id":3,"phone_number":"+12025550123"},{"id":4,"phone_number":"+12025550123"}],"meta":{"has_more":false}}`},
		{"missing-pagination", `{"payload":[],"meta":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("unexpected mutation")
					w.WriteHeader(403)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/inboxes/2") {
					fmt.Fprint(w, `{"id":2,"channel_type":"Channel::Api"}`)
					return
				}
				fmt.Fprint(w, tc.search)
			}))
			defer srv.Close()
			b := binding{Account: 1, Inbox: 2, Session: "alpha", Phone: "12025550123"}
			dir := t.TempDir()
			if _, err := resolveRoute(t.Context(), dir, srv.URL, "fixture", srv.Client(), b); err == nil {
				t.Fatal("accepted invalid contact")
			}
			b.Phone = "12025550124"
			if _, err := resolveRoute(t.Context(), dir, srv.URL, "fixture", srv.Client(), b); err == nil {
				t.Fatal("allowed a different binding")
			}
		})
	}
}
