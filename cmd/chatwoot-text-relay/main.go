// Command chatwoot-text-relay forwards signed, public agent replies for one
// pre-bound Chatwoot conversation. It is a separate, opt-in operator tool.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type binding struct {
	Account, Inbox, Conversation, Contact int64
	Session, Phone                        string
}
type relay struct {
	bound                   binding
	secret, token, endpoint string
	db                      *sql.DB
	client                  *http.Client
	now                     func() time.Time
	slots                   chan struct{}
}
type event struct {
	Event, Content string
	MessageType    string `json:"message_type"`
	ContentType    string `json:"content_type"`
	SourceID       string `json:"source_id"`
	ID             int64
	Private        *bool
	Account, Inbox struct{ ID int64 }
	Sender         struct {
		ID   int64
		Type string
	}
	Conversation struct {
		ID      int64
		InboxID int64 `json:"inbox_id"`
		Meta    struct{ Sender struct{ ID int64 } }
	}
	Attachments []json.RawMessage
}

// Explicit transport names matter: an omitted privacy flag must never be
// interpreted as a public reply.
func (b binding) accepts(e event) bool {
	return e.Event == "message_created" && e.ID > 0 && e.MessageType == "outgoing" && e.ContentType == "text" && e.Private != nil && !*e.Private && len(e.Attachments) == 0 && e.SourceID == "" && e.Sender.Type == "user" && e.Sender.ID > 0 && e.Account.ID == b.Account && e.Inbox.ID == b.Inbox && e.Conversation.InboxID == b.Inbox && e.Conversation.ID == b.Conversation && e.Conversation.Meta.Sender.ID == b.Contact
}
func state(path string, b binding) (*sql.DB, error) {
	// The DB contains only integer message IDs and states; bind its identity
	// permanently to the configured route. Credentials and content stay out.
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec("PRAGMA busy_timeout=5000;PRAGMA journal_mode=WAL;CREATE TABLE IF NOT EXISTS binding(id INTEGER PRIMARY KEY CHECK(id=1), fingerprint TEXT NOT NULL);CREATE TABLE IF NOT EXISTS receipts(id INTEGER PRIMARY KEY, state TEXT NOT NULL)")
	if e == nil {
		var data []byte
		data, e = json.Marshal(b)
		if e == nil {
			fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
			_, e = db.Exec("INSERT OR IGNORE INTO binding VALUES(1,?)", fingerprint)
			if e == nil {
				var current string
				e = db.QueryRow("SELECT fingerprint FROM binding WHERE id=1").Scan(&current)
				if e == nil && current != fingerprint {
					e = errors.New("state is bound to another route")
				}
			}
		}
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func (a *relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reply := func(status int) { w.WriteHeader(status) }
	if r.URL.Path != "/chatwoot" {
		reply(404)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		reply(405)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		reply(503)
		return
	}
	ts := r.Header.Get("X-Chatwoot-Timestamp")
	sec, e := strconv.ParseInt(ts, 10, 64)
	now := a.now().Unix()
	if e != nil || sec < now-300 || sec > now+300 || len(r.Header.Values("X-Chatwoot-Timestamp")) != 1 || len(r.Header.Values("X-Chatwoot-Signature")) != 1 {
		reply(401)
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if e != nil {
		reply(413)
		return
	}
	mac := hmac.New(sha256.New, []byte(a.secret))
	io.WriteString(mac, ts+".")
	mac.Write(raw)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-Chatwoot-Signature"))) {
		reply(401)
		return
	}
	var msg event
	if json.Unmarshal(raw, &msg) != nil {
		reply(400)
		return
	}
	if !a.bound.accepts(msg) {
		reply(204)
		return
	}
	if strings.TrimSpace(msg.Content) == "" || len(msg.Content) > 16384 {
		reply(422)
		return
	}
	// Reserve once before the network call, including across processes. A pending
	// receipt survives a timeout or power loss and never authorizes another POST.
	res, e := a.db.ExecContext(r.Context(), "INSERT OR IGNORE INTO receipts(id,state) SELECT ?, 'pending' WHERE (SELECT COUNT(*) FROM receipts)<100000", msg.ID)
	if e != nil {
		reply(503)
		return
	}
	n, e := res.RowsAffected()
	if e != nil {
		reply(503)
		return
	}
	key := fmt.Sprintf("cw:%d:%d:%d:%d", a.bound.Account, a.bound.Inbox, a.bound.Conversation, msg.ID)
	if n == 0 {
		var s string
		e = a.db.QueryRowContext(r.Context(), "SELECT state FROM receipts WHERE id=?", msg.ID).Scan(&s)
		if e == nil && (s == "accepted" || (s == "pending" && a.recover(r.Context(), msg.ID, key))) {
			reply(204)
		} else {
			reply(503)
		}
		return
	}
	payload, _ := json.Marshal(map[string]string{"phone": a.bound.Phone, "message": msg.Content})
	req, e := http.NewRequestWithContext(r.Context(), "POST", a.endpoint, bytes.NewReader(payload))
	if e != nil {
		reply(503)
		return
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	out, e := a.client.Do(req)
	if e != nil {
		reply(503)
		return
	}
	defer out.Body.Close()
	body, e := io.ReadAll(io.LimitReader(out.Body, 65537))
	if e != nil || len(body) > 65536 || out.StatusCode != http.StatusCreated || !accepted(body) {
		reply(503)
		return
	}
	// Once the remote accepted, finish even when the callback caller disconnected.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, e = a.db.ExecContext(ctx, "UPDATE receipts SET state='accepted' WHERE id=? AND state='pending'", msg.ID); e != nil {
		reply(503)
		return
	}
	reply(204)
}
func accepted(body []byte) bool {
	var result struct {
		Status   string
		Response []struct{ ID string }
	}
	return json.Unmarshal(body, &result) == nil && result.Status == "success" && len(result.Response) == 1 && result.Response[0].ID != ""
}

// A duplicate pending callback may only read the original result. A 404, an
// in-flight operation or an unconfirmed response never grants a second send.
func (a *relay) recover(ctx context.Context, id int64, key string) bool {
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(a.endpoint, "/send-text")+"/send-result", nil)
	if e != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Idempotency-Key", key)
	out, e := a.client.Do(req)
	if e != nil {
		return false
	}
	defer out.Body.Close()
	if out.StatusCode != 200 {
		return false
	}
	body, e := io.ReadAll(io.LimitReader(out.Body, 65537))
	if e != nil || len(body) > 65536 {
		return false
	}
	var v struct {
		Status, State string
		HTTPStatus    int
		Response      json.RawMessage
	}
	if json.Unmarshal(body, &v) != nil || v.Status != "success" || v.State != "complete" || v.HTTPStatus != 201 || !accepted(v.Response) {
		return false
	}
	_, e = a.db.ExecContext(ctx, "UPDATE receipts SET state='accepted' WHERE id=? AND state='pending'", id)
	return e == nil
}

func main() {
	positive := func(k string) int64 {
		n, e := strconv.ParseInt(os.Getenv(k), 10, 64)
		if e != nil || n < 1 {
			log.Fatalf("invalid %s", k)
		}
		return n
	}
	b := binding{positive("CHATWOOT_ACCOUNT_ID"), positive("CHATWOOT_INBOX_ID"), positive("CHATWOOT_CONVERSATION_ID"), positive("CHATWOOT_CONTACT_ID"), os.Getenv("WPP_SESSION_ID"), os.Getenv("WPP_PHONE")}
	secret, token := os.Getenv("CHATWOOT_WEBHOOK_SECRET"), os.Getenv("WPP_CLOUD_API_KEY")
	dir := os.Getenv("RELAY_STATE_DIR")
	if !regexp.MustCompile("^[A-Za-z0-9_-]{1,100}$").MatchString(b.Session) || !regexp.MustCompile("^[1-9][0-9]{6,14}$").MatchString(b.Phone) || len(secret) < 32 || token == "" || !filepath.IsAbs(dir) {
		log.Fatal("invalid route, credentials or state directory")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		log.Fatal("state directory unavailable")
	}
	db, e := state(filepath.Join(dir, "receipts.db"), b)
	if e != nil {
		log.Fatal("state unavailable or bound to another route")
	}
	defer db.Close()
	a := &relay{b, secret, token, "https://cloud.wppconnect.io/api/v1/sessions/" + b.Session + "/send-text", db, &http.Client{Timeout: 65 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, time.Now, make(chan struct{}, 8)}
	srv := http.Server{Addr: "127.0.0.1:8098", Handler: a, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	log.Fatal(srv.ListenAndServe())
}
