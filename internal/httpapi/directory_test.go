package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type directoryCall struct {
	operation string
	jid       types.JID
	presence  types.Presence
	state     types.ChatPresence
	medium    types.ChatPresenceMedia
}
type fakeDirectoryClient struct {
	contacts  map[types.JID]types.ContactInfo
	mappings  map[types.JID]types.JID
	calls     []directoryCall
	err       error
	failAfter int
}

func (f *fakeDirectoryClient) record(ctx context.Context, c directoryCall) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("missing directory deadline")
	}
	f.calls = append(f.calls, c)
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.failAfter == 0 || len(f.calls) >= f.failAfter {
		return f.err
	}
	return nil
}
func (f *fakeDirectoryClient) AllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return f.contacts, f.record(ctx, directoryCall{operation: "contacts"})
}
func (f *fakeDirectoryClient) Contact(ctx context.Context, jid types.JID) (types.ContactInfo, error) {
	return f.contacts[jid], f.record(ctx, directoryCall{operation: "contact", jid: jid})
}
func (f *fakeDirectoryClient) Mapping(ctx context.Context, jid types.JID) (types.JID, error) {
	return f.mappings[jid], f.record(ctx, directoryCall{operation: "mapping", jid: jid})
}
func (f *fakeDirectoryClient) SendPresence(ctx context.Context, p types.Presence) error {
	return f.record(ctx, directoryCall{operation: "presence", presence: p})
}
func (f *fakeDirectoryClient) SubscribePresence(ctx context.Context, jid types.JID) error {
	return f.record(ctx, directoryCall{operation: "subscribe", jid: jid})
}
func (f *fakeDirectoryClient) SendChatPresence(ctx context.Context, jid types.JID, p types.ChatPresence, m types.ChatPresenceMedia) error {
	return f.record(ctx, directoryCall{operation: "chat-presence", jid: jid, state: p, medium: m})
}
func (f *fakeDirectoryClient) GetUserInfo(ctx context.Context, ids []types.JID) (map[types.JID]types.UserInfo, error) {
	return map[types.JID]types.UserInfo{ids[0]: {Status: "Available"}}, f.record(ctx, directoryCall{operation: "user", jid: ids[0]})
}
func (f *fakeDirectoryClient) GetProfilePictureInfo(ctx context.Context, jid types.JID, _ *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
	return &types.ProfilePictureInfo{ID: "picture", URL: "https://mmg.whatsapp.net/picture"}, f.record(ctx, directoryCall{operation: "picture", jid: jid})
}

type directoryHarness struct {
	router  http.Handler
	clients map[string]*fakeDirectoryClient
	states  map[string]*directory.State
	tokens  map[string]string
}

func newDirectoryHarness(t *testing.T) *directoryHarness {
	t.Helper()
	h := &directoryHarness{clients: make(map[string]*fakeDirectoryClient), states: make(map[string]*directory.State), tokens: make(map[string]string)}
	for _, name := range []string{"alpha", "beta"} {
		index, err := directory.Open(filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		state := directory.NewState(index)
		t.Cleanup(func() { state.Close() })
		h.states[name] = state
		phone := types.NewJID("111", types.DefaultUserServer)
		lid := types.NewJID("999", types.HiddenUserServer)
		alias := types.NewJID("555", types.DefaultUserServer)
		h.clients[name] = &fakeDirectoryClient{contacts: map[types.JID]types.ContactInfo{phone: {Found: true, FullName: name, FirstName: "saved"}, lid: {Found: true, PushName: "LID name"}}, mappings: map[types.JID]types.JID{alias: lid, lid: alias}}
		err = state.Observe(&events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{
			{ID: proto.String(phone.String()), Name: proto.String("old name"), LastMsgTimestamp: proto.Uint64(300), UnreadCount: proto.Uint32(3)},
			{ID: proto.String("222@g.us"), Name: proto.String("Team"), LastMsgTimestamp: proto.Uint64(200), Archived: proto.Bool(true)},
			{ID: proto.String(lid.String()), Name: proto.String("LID"), LastMsgTimestamp: proto.Uint64(100), UnreadCount: proto.Uint32(0)},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		token, err := auth.Generate(name, "directory-secret")
		if err != nil {
			t.Fatal(err)
		}
		h.tokens[name] = token
	}
	s := &Server{cfg: config.Config{SecretKey: "directory-secret"}, directorySession: func(name string) (directory.Client, *directory.State, bool) {
		client, ok := h.clients[name]
		return client, h.states[name], ok
	}}
	h.router = s.router()
	return h
}
func (h *directoryHarness) call(t *testing.T, method, route, session, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "/api/"+session+"/"+route, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	return w
}
func (h *directoryHarness) get(t *testing.T, route string) *httptest.ResponseRecorder {
	return h.call(t, "GET", route, "alpha", h.tokens["alpha"], nil)
}
func (h *directoryHarness) post(t *testing.T, route string, body any) *httptest.ResponseRecorder {
	return h.call(t, "POST", route, "alpha", h.tokens["alpha"], body)
}
func chatArray(t *testing.T, w *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
	return result
}

func TestDirectoryContactContracts(t *testing.T) {
	h := newDirectoryHarness(t)
	w := h.get(t, "all-contacts")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	response := groupResponse(t, w)["response"].([]any)
	if len(response) != 2 || response[0].(map[string]any)["name"] != "alpha" {
		t.Fatal(response)
	}
	w = h.get(t, "contact/111@c.us")
	if w.Code != 200 || groupResponse(t, w)["response"].(map[string]any)["name"] != "alpha" {
		t.Fatal(w.Body.String())
	}
	w = h.get(t, "contact/555")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "LID name") {
		t.Fatalf("PN/LID fallback: %d %s", w.Code, w.Body.String())
	}
	w = h.get(t, "contact/pn-lid/555")
	if w.Code != 200 || groupResponse(t, w)["lid"].(map[string]any)["_serialized"] != "999@lid" {
		t.Fatal(w.Body.String())
	}
	for _, route := range []string{"profile-status/111", "profile-pic/111", "profile-pic/222@g.us?isGroup=true"} {
		w = h.get(t, route)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
	w = h.get(t, "contact/777")
	if w.Code != 404 {
		t.Fatalf("unknown contact invented: %d", w.Code)
	}
	w = h.get(t, "contact/111?isGroup=false")
	if w.Code != 200 {
		t.Fatal("false query interpreted as true")
	}
	w = h.get(t, "contact/222@g.us")
	if w.Code != 400 {
		t.Fatal("group accepted as contact")
	}
}

func TestDirectoryChatContractsAndFilters(t *testing.T) {
	h := newDirectoryHarness(t)
	w := h.get(t, "all-chats")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	result := groupResponse(t, w)
	if result["mapper"] != "chat" || len(result["response"].([]any)) != 3 {
		t.Fatal(result)
	}
	w = h.get(t, "chat-by-id/111@c.us")
	if w.Code != 200 || groupResponse(t, w)["name"] != "alpha" || groupResponse(t, w)["unreadCount"] != float64(3) {
		t.Fatal(w.Body.String())
	}
	w = h.get(t, "chat-by-id/555")
	if w.Code != 200 || groupResponse(t, w)["id"].(map[string]any)["_serialized"] != "999@lid" {
		t.Fatal(w.Body.String())
	}
	for _, tc := range []struct {
		filter map[string]any
		size   int
		first  string
	}{
		{map[string]any{}, 3, "111@c.us"}, {map[string]any{"count": 1}, 1, "111@c.us"},
		{map[string]any{"onlyGroups": true}, 1, "222@g.us"}, {map[string]any{"onlyUsers": true}, 2, "111@c.us"},
		{map[string]any{"onlyWithUnreadMessage": true}, 1, "111@c.us"}, {map[string]any{"onlyArchived": true}, 1, "222@g.us"},
		{map[string]any{"id": "222@g.us", "count": 1, "direction": "after"}, 1, "222@g.us"},
		{map[string]any{"id": "222@g.us", "count": 1, "direction": "before"}, 1, "111@c.us"},
		{map[string]any{"count": 1, "direction": "before"}, 0, ""},
	} {
		w = h.post(t, "list-chats", tc.filter)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		list := chatArray(t, w)
		if len(list) != tc.size {
			t.Fatalf("%+v: %+v", tc.filter, list)
		}
		if len(list) > 0 && list[0]["id"].(map[string]any)["_serialized"] != tc.first {
			t.Fatal(list)
		}
	}
	w = h.post(t, "list-chats", nil)
	if w.Code != 200 || len(chatArray(t, w)) != 3 {
		t.Fatal("optional empty list body rejected")
	}
	w = h.get(t, "all-chats-archived")
	if w.Code != 201 || len(chatArray(t, w)) != 1 {
		t.Fatal(w.Body.String())
	}
	w = h.get(t, "chat-by-id/777")
	if w.Code != 404 {
		t.Fatal("unknown chat invented")
	}
}

func TestDirectoryPresenceQueriesAndSignals(t *testing.T) {
	h := newDirectoryHarness(t)
	w := h.get(t, "chat-is-online/111")
	result := groupResponse(t, w)
	if w.Code != 200 || result["response"] != nil || result["known"] != false {
		t.Fatal("unknown presence fabricated offline")
	}
	h.states["alpha"].Presence.Observe(&events.Presence{From: types.NewJID("111", types.DefaultUserServer), Unavailable: true, LastSeen: time.Unix(100, 0)})
	w = h.get(t, "chat-is-online/111")
	result = groupResponse(t, w)
	if result["response"] != false || result["known"] != true {
		t.Fatal(result)
	}
	w = h.get(t, "last-seen/111")
	if groupResponse(t, w)["response"] != float64(100) {
		t.Fatal(w.Body.String())
	}
	for _, tc := range []struct {
		route     string
		body      any
		operation string
		presence  types.Presence
		state     types.ChatPresence
		medium    types.ChatPresenceMedia
	}{
		{"set-online-presence", map[string]any{}, "presence", types.PresenceAvailable, "", ""},
		{"set-online-presence", map[string]any{"isOnline": false}, "presence", types.PresenceUnavailable, "", ""},
		{"subscribe-presence", map[string]any{"phone": "111"}, "subscribe", "", "", ""},
		{"typing", map[string]any{"phone": "111"}, "chat-presence", "", types.ChatPresenceComposing, types.ChatPresenceMediaText},
		{"typing", map[string]any{"phone": "111", "value": false}, "chat-presence", "", types.ChatPresencePaused, types.ChatPresenceMediaText},
		{"recording", map[string]any{"phone": "222@g.us", "isGroup": true}, "chat-presence", "", types.ChatPresenceComposing, types.ChatPresenceMediaAudio},
		{"chat-state", map[string]any{"phone": "111", "chatstate": "1"}, "chat-presence", "", types.ChatPresenceComposing, types.ChatPresenceMediaAudio},
		{"chat-state", map[string]any{"phone": "111", "chatstate": 2}, "chat-presence", "", types.ChatPresencePaused, types.ChatPresenceMediaText},
	} {
		w = h.post(t, tc.route, tc.body)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
		calls := h.clients["alpha"].calls
		c := calls[len(calls)-1]
		if c.operation != tc.operation || c.presence != tc.presence || c.state != tc.state || c.medium != tc.medium {
			t.Fatalf("%s: %+v", tc.route, c)
		}
	}
	w = h.post(t, "recording", map[string]any{"phone": "111", "duration": 1})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	calls := h.clients["alpha"].calls
	if calls[len(calls)-1].state != types.ChatPresencePaused {
		t.Fatal("recording duration did not send pause")
	}
	h.states["alpha"].Presence.Clear()
	w = h.get(t, "last-seen/111")
	if groupResponse(t, w)["known"] != false {
		t.Fatal("disconnected presence retained")
	}
}

func TestDirectoryUnsupportedAndValidation(t *testing.T) {
	h := newDirectoryHarness(t)
	for _, tc := range []struct {
		route string
		body  any
		code  int
	}{
		{"list-chats", map[string]any{"withLabels": []string{"Customer"}}, 400},
		{"list-chats", map[string]any{"onlyCommunities": true}, 404},
		{"list-chats", map[string]any{"count": 1001}, 400},
		{"list-chats", map[string]any{"count": 0}, 400},
		{"list-chats", map[string]any{"direction": "invalid"}, 400},
		{"list-chats", map[string]any{"onlyGroups": true, "onlyUsers": true}, 400},
		{"typing", map[string]any{"phone": "111", "value": nil}, 400},
		{"typing", map[string]any{"phone": "111@evil.test"}, 400},
		{"recording", map[string]any{"phone": "111", "duration": 10001}, 400},
		{"chat-state", map[string]any{"phone": "111", "chatstate": "bad"}, 400},
		{"subscribe-presence", map[string]any{"phone": "222@g.us"}, 501},
		{"subscribe-presence", map[string]any{"all": true, "isGroup": true}, 501},
	} {
		w := h.post(t, tc.route, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
	}
	for _, route := range []string{"all-chats-with-messages", "get-messages/111", "all-unread-messages"} {
		w := h.get(t, route)
		if w.Code != 501 || groupResponse(t, w)["status"] != "not_supported" {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
	if len(h.clients["alpha"].calls) != 0 {
		t.Fatal("invalid/unsupported request reached backend")
	}
	r := httptest.NewRequest("POST", "/api/alpha/list-chats", strings.NewReader(strings.Repeat(" ", 64<<10)+"{}"))
	r.Header.Set("Authorization", "Bearer "+h.tokens["alpha"])
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("body limit: %d", w.Code)
	}
}

func TestDirectoryAuthIsolationFailuresAndOutgoingIndex(t *testing.T) {
	h := newDirectoryHarness(t)
	w := h.call(t, "GET", "all-contacts", "beta", h.tokens["alpha"], nil)
	if w.Code != 401 || len(h.clients["beta"].calls) != 0 {
		t.Fatal("token escaped session")
	}
	w = h.call(t, "GET", "contact/111", "beta", h.tokens["beta"], nil)
	if w.Code != 200 || groupResponse(t, w)["response"].(map[string]any)["name"] != "beta" {
		t.Fatal(w.Body.String())
	}
	for _, tc := range []struct {
		err  error
		code int
	}{{context.DeadlineExceeded, 504}, {whatsmeow.ErrIQForbidden, 403}, {whatsmeow.ErrNoPushName, 409}, {whatsmeow.ErrNoPrivacyToken, 403}, {errors.New("private backend details"), 502}} {
		h.clients["alpha"].err = tc.err
		w = h.post(t, "set-online-presence", map[string]any{})
		if w.Code != tc.code || strings.Contains(w.Body.String(), "private backend") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	h.clients["alpha"].err = nil
	s := &Server{directorySession: func(name string) (directory.Client, *directory.State, bool) {
		return h.clients[name], h.states[name], true
	}}
	s.observeSent("alpha", types.NewJID("444", types.DefaultUserServer), "sent-id", time.Unix(500, 0))
	w = h.get(t, "chat-by-id/444")
	if w.Code != 200 || groupResponse(t, w)["unreadCount"] != float64(0) {
		t.Fatalf("outgoing chat missing: %d %s", w.Code, w.Body.String())
	}
	list, _ := h.states["beta"].Chats.List(context.Background())
	if len(list) != 3 {
		t.Fatal("outgoing metadata leaked to beta")
	}
	delete(h.clients, "alpha")
	w = h.get(t, "all-contacts")
	if w.Code != 404 {
		t.Fatal("disconnected directory accessible")
	}
}
