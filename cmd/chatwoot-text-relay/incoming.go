package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type incomingHandler struct {
	relay                               *relay
	conversationURL, token, secret, lid string
}

func configureIncoming(a *relay, origin, token, secret, lid string) (*incomingHandler, error) {
	if origin == "" && token == "" && secret == "" && lid == "" {
		return nil, nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || token == "" || len(secret) < 32 || (lid != "" && !lidFormat.MatchString(lid)) {
		return nil, errors.New("invalid incoming webhook configuration")
	}
	endpoint := fmt.Sprintf("https://%s/api/v1/accounts/%d/conversations/%d", u.Host, a.bound.Account, a.bound.Conversation)
	return newIncoming(a, endpoint, token, secret, lid)
}
func newIncoming(a *relay, endpoint, token, secret, lid string) (*incomingHandler, error) {
	_, err := a.db.Exec("CREATE TABLE IF NOT EXISTS cw_origin(id INTEGER PRIMARY KEY CHECK(id=1), identity TEXT NOT NULL);CREATE TABLE IF NOT EXISTS cw_incoming(source TEXT PRIMARY KEY, accepted INTEGER NOT NULL DEFAULT 0)")
	if err != nil {
		return nil, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint+"|"+lid)))
	if _, err = a.db.Exec("INSERT OR IGNORE INTO cw_origin VALUES(1,?)", fingerprint); err != nil {
		return nil, err
	}
	var current string
	if err = a.db.QueryRow("SELECT identity FROM cw_origin WHERE id=1").Scan(&current); err != nil {
		return nil, err
	}
	if current != fingerprint {
		return nil, errors.New("incoming state belongs to a different Chatwoot route")
	}
	return &incomingHandler{a, endpoint, token, secret, lid}, nil
}

type wppText struct {
	Event, Session, ID, ChatID, Type, Body string
	FromMe, IsGroupMsg, IsMedia            *bool
	PersistentText                         bool
}

func (h *incomingHandler) peerMatches(jid string) bool {
	return jid == h.relay.bound.Phone+"@s.whatsapp.net" || (h.lid != "" && jid == h.lid)
}
func (h *incomingHandler) signed(r *http.Request, body []byte) bool {
	headers := []string{"X-WPP-Timestamp", "X-WPP-Event-ID", "X-WPP-Attempt", "X-WPP-Signature"}
	for _, k := range headers {
		if len(r.Header.Values(k)) != 1 {
			return false
		}
	}
	stamp, event, attempt := r.Header.Get(headers[0]), r.Header.Get(headers[1]), r.Header.Get(headers[2])
	ts, err := strconv.ParseInt(stamp, 10, 64)
	now := h.relay.now().Unix()
	if err != nil || ts < now-300 || ts > now+300 || len(event) == 0 || len(event) > 128 {
		return false
	}
	if n, err := strconv.ParseUint(attempt, 10, 32); err != nil || n == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	fmt.Fprintf(mac, "%s.%s.%s.", stamp, event, attempt)
	mac.Write(body)
	return hmac.Equal([]byte("v1="+hex.EncodeToString(mac.Sum(nil))), []byte(r.Header.Get(headers[3])))
}
func (h *incomingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/wpp" {
		w.WriteHeader(404)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	select {
	case h.relay.slots <- struct{}{}:
		defer func() { <-h.relay.slots }()
	default:
		w.WriteHeader(503)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		w.WriteHeader(413)
		return
	}
	if !h.signed(r, raw) {
		w.WriteHeader(401)
		return
	}
	var msg wppText
	if json.Unmarshal(raw, &msg) != nil {
		w.WriteHeader(400)
		return
	}
	if msg.Event != "onmessage" || msg.Session != h.relay.bound.Session || msg.Type != "chat" || !msg.PersistentText || !h.peerMatches(msg.ChatID) || msg.FromMe == nil || *msg.FromMe || msg.IsGroupMsg == nil || *msg.IsGroupMsg || msg.IsMedia == nil || *msg.IsMedia {
		w.WriteHeader(204)
		return
	}
	if msg.ID == "" || len(msg.ID) > 256 || strings.TrimSpace(msg.Body) == "" || len(msg.Body) > 16384 {
		w.WriteHeader(422)
		return
	}
	// The WhatsApp ID stays stable even when two deliveries have distinct event IDs.
	source := fmt.Sprintf("wpp:%x", sha256.Sum256([]byte(msg.Session+"\x00"+msg.ID)))
	if h.deliver(r.Context(), source, msg.Body) {
		w.WriteHeader(204)
	} else {
		w.WriteHeader(503)
	}
}
func (h *incomingHandler) request(ctx context.Context, method, suffix string, payload []byte, v any) bool {
	req, err := http.NewRequestWithContext(ctx, method, h.conversationURL+suffix, bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.GetBody = nil
	req.Header.Set("api_access_token", h.token)
	req.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.relay.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	return err == nil && len(raw) <= 1<<20 && json.Unmarshal(raw, v) == nil
}

type cwMessage struct {
	ID           int64
	Source       string `json:"source_id"`
	Conversation int64  `json:"conversation_id"`
	Inbox        int64  `json:"inbox_id"`
	Type         *int   `json:"message_type"`
	Private      *bool
	Content      string
}

func (h *incomingHandler) matches(m cwMessage, source, text string) bool {
	return m.ID > 0 && m.Source == source && m.Inbox == h.relay.bound.Inbox && m.Conversation == h.relay.bound.Conversation && m.Type != nil && *m.Type == 0 && m.Private != nil && !*m.Private && m.Content == text
}
func (h *incomingHandler) boundConversation(ctx context.Context) bool {
	var c struct {
		ID      int64
		Account int64 `json:"account_id"`
		Inbox   int64 `json:"inbox_id"`
		Meta    struct {
			Channel string
			Sender  struct{ ID int64 }
		}
	}
	b := h.relay.bound
	return h.request(ctx, "GET", "", nil, &c) && c.ID == b.Conversation && c.Account == b.Account && c.Inbox == b.Inbox && c.Meta.Sender.ID == b.Contact && c.Meta.Channel == "Channel::Api"
}
func (h *incomingHandler) deliver(ctx context.Context, source, text string) bool {
	db := h.relay.db
	var done int
	err := db.QueryRowContext(ctx, "SELECT accepted FROM cw_incoming WHERE source=?", source).Scan(&done)
	if err == nil && done == 1 {
		return true
	}
	if !h.boundConversation(ctx) {
		return false
	}
	res, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO cw_incoming(source) SELECT ? WHERE (SELECT count(*) FROM cw_incoming)<100000", source)
	if err != nil {
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false
	}
	if n == 1 {
		payload, _ := json.Marshal(map[string]any{"source_id": source, "content": text, "message_type": "incoming", "content_type": "text", "private": false})
		var created cwMessage
		if !h.request(ctx, "POST", "/messages", payload, &created) || !h.matches(created, source, text) {
			return false
		}
	} else {
		if err = db.QueryRowContext(ctx, "SELECT accepted FROM cw_incoming WHERE source=?", source).Scan(&done); err != nil {
			return false
		}
		if done == 1 {
			return true
		}
		// Reconcile in the latest page. Absence never authorizes a second POST.
		var page struct{ Payload []cwMessage }
		if !h.request(ctx, "GET", "/messages", nil, &page) {
			return false
		}
		found := false
		for _, m := range page.Payload {
			if h.matches(m, source, text) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	_, err = db.ExecContext(ctx, "UPDATE cw_incoming SET accepted=1 WHERE source=?", source)
	return err == nil
}

var lidFormat = regexp.MustCompile(`^[1-9][0-9]{0,19}@lid$`)
