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

	"github.com/coder/websocket"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/eventbus"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type roadmapAccount struct {
	*whatsmeow.Client
	calls int
	err   error
}

func (f *fakeMediaClient) MarkRead(ctx context.Context, ids []types.MessageID, at time.Time, chat, sender types.JID, receipt ...types.ReceiptType) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("receipt lacks deadline")
	}
	if len(ids) == 0 {
		panic("empty receipt IDs")
	}
	return f.sendErr
}

func (f *roadmapAccount) Self() types.JID { return types.NewJID("999", types.DefaultUserServer) }
func (f *roadmapAccount) SendAppState(ctx context.Context, _ appstate.PatchInfo) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("missing deadline")
	}
	f.calls++
	return f.err
}
func (f *roadmapAccount) SetPrivacySetting(_ context.Context, _ types.PrivacySettingType, _ types.PrivacySetting) (types.PrivacySettings, error) {
	f.calls++
	return types.PrivacySettings{}, f.err
}

func TestRoadmapHistoryActionsAndPrivacy(t *testing.T) {
	h := newMediaHarness(t)
	stores := map[string]*history.Store{}
	for _, name := range []string{"alpha", "beta"} {
		store, err := history.Open(filepath.Join(t.TempDir(), name+".db"), history.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		stores[name] = store
		t.Cleanup(func() { store.Close() })
	}
	fake := &roadmapAccount{}
	s := &Server{cfg: config.Config{SecretKey: "test-secret"}, accountSession: func(string) (account.Client, bool) { return fake, true }, mediaSession: func(name string) (media.Client, *media.Store, bool) { v, ok := h.stores[name]; return h.client, v, ok }, historySession: func(name string) (*history.Store, func(), error) { return stores[name], func() {}, nil }}
	h.router = s.router()
	chat := types.NewJID("123", types.DefaultUserServer)
	put := func(id string, own bool) {
		t.Helper()
		err := stores["alpha"].Put(context.Background(), types.MessageInfo{ID: id, Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: chat, Sender: chat, IsFromMe: own}}, &waE2E.Message{Conversation: proto.String("stored text"), MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: []byte("never-expose-me")}})
		if err != nil {
			t.Fatal(err)
		}
	}
	put("incoming", false)
	put("own", true)
	for _, route := range []string{"messages", "all-messages-in-chat/123", "messages/incoming?chatId=123@s.whatsapp.net"} {
		w := h.call("GET", "/api/alpha/"+route, h.tokens["alpha"], "", nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "never-expose-me") {
			t.Fatalf("history %s: %d %s", route, w.Code, w.Body.String())
		}
	}
	w := h.call("GET", "/api/beta/messages/incoming?chatId=123@s.whatsapp.net", h.tokens["beta"], "", nil)
	if w.Code != 404 {
		t.Fatalf("cross-session read %d", w.Code)
	}
	for _, tc := range []struct {
		route string
		body  any
		code  int
	}{
		{"send-reply", map[string]any{"phone": "123", "message": "reply", "quotedMessageId": "incoming", "mentionedList": []string{"456"}}, 201},
		{"send-reply", map[string]any{"phone": "456", "message": "reply", "quotedMessageId": "incoming"}, 404},
		{"edit-message", map[string]any{"phone": "123", "messageId": "incoming", "message": "bad"}, 400},
		{"edit-message", map[string]any{"phone": "123", "messageId": "own", "message": "edited"}, 201},
		{"send-reaction", map[string]any{"phone": "123", "messageId": "incoming", "reaction": ""}, 201},
		{"mark-read", map[string]any{"phone": "123", "messageIds": []string{"incoming"}}, 200},
		{"mark-read", map[string]any{"phone": "123", "messageIds": []string{"own"}}, 400},
		{"mark-played", map[string]any{"phone": "123", "messageIds": []string{"incoming"}}, 400},
		{"privacy-settings", map[string]any{"name": "online", "value": "match_last_seen"}, 200},
		{"privacy-settings", map[string]any{"name": "online", "value": "all_except"}, 400},
		{"edit-label", map[string]any{"labelId": "1", "name": "Customer", "color": 2}, 200},
		{"label-chat", map[string]any{"phone": "123", "labelId": "1", "value": true, "messageId": "incoming"}, 400},
		{"label-chat", map[string]any{"phone": "123", "labelId": "1", "value": true}, 200},
		{"send-buttons", map[string]any{}, 501}, {"send-carousel", map[string]any{}, 501},
		{"delete-message", map[string]any{"phone": "123", "messageId": "own"}, 201},
	} {
		w := h.post(t, tc.route, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
	}
	last, err := stores["alpha"].Get(context.Background(), chat.String(), "own")
	if err != nil || !last.Deleted || proto.Size(last.Message) != 0 {
		t.Fatalf("revoke tombstone: %#v %v", last, err)
	}
	before := fake.calls
	fake.err = errors.New("protocol denied")
	w = h.post(t, "edit-label", map[string]any{"labelId": "2", "name": "Rejected", "color": 1})
	if w.Code < 400 || fake.calls != before+1 {
		t.Fatalf("remote rejection %d", w.Code)
	}
	labels, _ := stores["alpha"].Labels(context.Background())
	if len(labels) != 1 {
		t.Fatal("unacknowledged label persisted")
	}
	w = h.call("DELETE", "/api/alpha/messages?chatId="+chat.String(), h.tokens["alpha"], "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	page, _ := stores["alpha"].List(context.Background(), history.Query{})
	if len(page.Messages) != 0 {
		t.Fatal("purge retained messages")
	}
}

func TestAdminCookieCSRFAndRevocation(t *testing.T) {
	token := strings.Repeat("a", 40)
	s := &Server{cfg: config.Config{AdminToken: token, SecretKey: "test-secret", ManagerEnabled: true}}
	router := s.router()
	call := func(method, path, body, csrf, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Origin", origin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/metrics", "", "", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/api/admin/login", `{"token":"`+token+`"}`, "", "https://evil.test", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/api/admin/login", `{"token":"`+token+`"}`, "", "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var login struct {
		CSRF string `json:"csrfToken"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 28800 {
		t.Fatal("unsafe cookie")
	}
	cookie := cookies[0]
	if w = call("GET", "/api/admin/capabilities", "", "", "", cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = call("POST", "/api/admin/logout", "{}", "", "", cookie); w.Code != 401 {
		t.Fatal("missing CSRF accepted")
	}
	if w = call("POST", "/api/admin/logout", "{}", login.CSRF, "", cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call("GET", "/metrics", "", "", "", cookie); w.Code != 401 {
		t.Fatal("revoked cookie accepted")
	}
	for i := 0; i < 8; i++ {
		w = call("POST", "/api/admin/login", `{"token":"wrong"}`, "", "", nil)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w = call("POST", "/api/admin/login", `{"token":"wrong"}`, "", "", nil); w.Code != 429 {
		t.Fatal("rate limit", w.Code)
	}
}

func TestDurableEventHTTPAndWebSocketIsolation(t *testing.T) {
	log, err := eventbus.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	bus, err := eventbus.New(log, nil, eventbus.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		bus.Close(ctx)
	}()
	bus.Publish("onmessage", "alpha", map[string]any{"body": "alpha-event"})
	bus.Publish("onmessage", "beta", map[string]any{"body": "private-beta-event"})
	router := (&Server{cfg: config.Config{SecretKey: "test-secret"}, events: bus}).router()
	server := httptest.NewServer(router)
	defer server.Close()
	token, _ := auth.Generate("alpha", "test-secret")
	req, _ := http.NewRequest("GET", server.URL+"/api/alpha/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	body.ReadFrom(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || strings.Contains(body.String(), "private-beta-event") {
		t.Fatal(body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/alpha/events/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_, data, err := ws.Read(ctx)
	if err != nil || !strings.Contains(string(data), "alpha-event") || strings.Contains(string(data), "private-beta-event") {
		t.Fatalf("stream: %s %v", data, err)
	}
}
