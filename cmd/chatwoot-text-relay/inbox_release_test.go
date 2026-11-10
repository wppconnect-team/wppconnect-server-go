package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChatwootAPIInbox(t *testing.T) {
	path := os.Getenv("CHATWOOT_INBOX_FIXTURE")
	if path == "" {
		t.Skip("requires the isolated Rails API inbox")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var data struct {
		Account, Inbox, Conversation, Contact int64
		Token, Secret, Body                   string
		Headers                               map[string]string
	}
	if json.Unmarshal(raw, &data) != nil || data.Conversation == 0 || data.Token == "" {
		t.Fatal("invalid Rails fixture")
	}
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["message"] != "agent fixture" {
			t.Error("incorrect human reply")
		}
		w.WriteHeader(201)
		fmt.Fprint(w, acceptedJSON)
	})
	a.db.Close()
	a.bound = binding{data.Account, data.Inbox, data.Conversation, data.Contact, "demo", "19995550101"}
	a.db, e = state(filepath.Join(t.TempDir(), "actual.db"), a.bound)
	if e != nil {
		t.Fatal(e)
	}
	a.secret = data.Secret
	a.now = time.Now
	req := httptest.NewRequest("POST", "/chatwoot", strings.NewReader(data.Body))
	for k, v := range data.Headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("Rails webhook HTTP %d", w.Code)
	}
	var acceptedCount int
	if err := a.db.QueryRow("SELECT count(*) FROM receipts WHERE state='accepted'").Scan(&acceptedCount); err != nil || acceptedCount != 1 {
		t.Fatal("Rails webhook not processed")
	}
	a.now = func() time.Time { return time.Unix(1000, 0) }
	url := fmt.Sprintf("http://127.0.0.1:3000/api/v1/accounts/%d/conversations/%d", data.Account, data.Conversation)
	h, e := newIncoming(a, url, data.Token, "synthetic-secret", "")
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range []string{"rails-1", "rails-2"} {
		if status := submitWPP(h, incomingJSON, event, h.secret); status != 204 {
			t.Fatalf("delivering to Rails: HTTP %d", status)
		}
	}
	var source string
	if e = a.db.QueryRow("SELECT source FROM cw_incoming WHERE accepted=1").Scan(&source); e != nil {
		t.Fatal("missing receipt")
	}
	var page struct{ Payload []cwMessage }
	if !h.request(t.Context(), "GET", "/messages", nil, &page) {
		t.Fatal("read Rails conversation")
	}
	count := 0
	for _, m := range page.Payload {
		if h.matches(m, source, "original incoming") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Rails persisted %d matching messages", count)
	}
}
