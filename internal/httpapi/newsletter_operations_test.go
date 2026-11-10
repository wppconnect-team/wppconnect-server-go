package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
)

type newsletterFixture struct {
	*roadmapChannels
	role    types.NewsletterRole
	calls   []string
	ids     []types.MessageServerID
	updates *whatsmeow.GetNewsletterUpdatesParams
	failure error
	wrongID bool
}

func (f *newsletterFixture) note(ctx context.Context, op string) {
	if _, ok := ctx.Deadline(); !ok {
		panic("missing newsletter deadline")
	}
	f.calls = append(f.calls, op)
}
func (f *newsletterFixture) metadata() *types.NewsletterMetadata {
	jid := types.NewJID("123", types.NewsletterServer)
	if f.wrongID {
		jid = types.NewJID("123", types.DefaultUserServer)
	}
	return &types.NewsletterMetadata{ID: jid, ViewerMeta: &types.NewsletterViewerMetadata{Role: f.role}, ThreadMeta: types.NewsletterThreadMetadata{Name: types.NewsletterText{Text: "Verified channel name"}}}
}
func (f *newsletterFixture) GetNewsletterInfoWithInvite(ctx context.Context, invite string) (*types.NewsletterMetadata, error) {
	f.note(ctx, "resolve:"+invite)
	return f.metadata(), f.failure
}
func (f *newsletterFixture) GetNewsletterInfo(ctx context.Context, id types.JID) (*types.NewsletterMetadata, error) {
	f.note(ctx, "info:"+id.String())
	return f.metadata(), f.failure
}
func (f *newsletterFixture) FollowNewsletter(ctx context.Context, id types.JID) error {
	f.note(ctx, "follow:"+id.String())
	return f.failure
}
func (f *newsletterFixture) UnfollowNewsletter(ctx context.Context, id types.JID) error {
	f.note(ctx, "unfollow:"+id.String())
	return f.failure
}
func (f *newsletterFixture) GenerateMessageID() types.MessageID { return "reaction-id" }
func (f *newsletterFixture) NewsletterSendReaction(ctx context.Context, id types.JID, serverID types.MessageServerID, reaction string, messageID types.MessageID) error {
	f.note(ctx, "reaction:"+id.String()+":"+reaction+":"+messageID)
	f.ids = []types.MessageServerID{serverID}
	return f.failure
}
func (f *newsletterFixture) NewsletterMarkViewed(ctx context.Context, id types.JID, ids []types.MessageServerID) error {
	f.note(ctx, "view:"+id.String())
	f.ids = ids
	return f.failure
}
func (f *newsletterFixture) GetNewsletterMessageUpdates(ctx context.Context, id types.JID, params *whatsmeow.GetNewsletterUpdatesParams) ([]*types.NewsletterMessage, error) {
	f.note(ctx, "updates:"+id.String())
	f.updates = params
	return []*types.NewsletterMessage{{MessageID: "first", MessageServerID: 41, Timestamp: time.Unix(100, 0), ViewsCount: 31, ForwardsCount: 4, ReactionCounts: map[string]int{"ok": 3}}, {MessageID: "second"}}, f.failure
}
func (f *newsletterFixture) AcceptTOSNotice(ctx context.Context, id, stage string) error {
	f.note(ctx, "tos:"+id+":"+stage)
	return f.failure
}

func TestNewsletterContractsAndSideEffects(t *testing.T) {
	h := newMediaHarness(t)
	f := &newsletterFixture{roadmapChannels: &roadmapChannels{sender: h.client}, role: types.NewsletterRoleOwner}
	ledger, e := sendledger.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ledger.Close() })
	h.router = (&Server{cfg: config.Config{SecretKey: "test-secret"}, sends: ledger, channelSession: func(name string) (channels.Client, bool) { return f, name == "alpha" }}).router()
	call := func(route, body, token, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/alpha/"+route, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, r)
		return w
	}
	post := func(route, body string, want int) string {
		t.Helper()
		w := call(route, body, h.tokens["alpha"], "")
		if w.Code != want {
			t.Fatalf("%s returned %d, want %d: %s", route, w.Code, want, w.Body.String())
		}
		return w.Body.String()
	}
	for _, route := range []string{"newsletter-info-invite", "follow-newsletter-invite", "unfollow-newsletter-invite"} {
		post(route, `{"invite":"https://whatsapp.com/channel/Abcdef99"}`, 200)
	}
	want := []string{"resolve:Abcdef99", "resolve:Abcdef99", "follow:123@newsletter", "resolve:Abcdef99", "unfollow:123@newsletter"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
	f.wrongID = true
	post("follow-newsletter-invite", `{"invite":"Abcdef99"}`, 502)
	f.wrongID = false
	if got := f.calls[len(f.calls)-1]; got != "resolve:Abcdef99" {
		t.Fatal("mutated wrong upstream identity", got)
	}
	output := post("newsletter-reaction", `{"id":"123@newsletter","serverId":22,"reaction":""}`, 202)
	if !strings.Contains(output, `"acknowledged":false`) || f.calls[len(f.calls)-1] != "reaction:123@newsletter::reaction-id" || f.ids[0] != 22 {
		t.Fatal("reaction removal was not sent as a removal", output)
	}
	output = post("newsletter-message-updates", `{"id":"123@newsletter","count":1,"since":500,"after":7}`, 200)
	if f.updates.Count != 1 || f.updates.After != 7 || f.updates.Since.Unix() != 500 || !strings.Contains(output, `"forwards":4`) || !strings.Contains(output, `"ok":3`) || strings.Contains(output, "second") {
		t.Fatal("incorrect update window or counters", output)
	}
	post("newsletter-accept-tos", `{"noticeId":"123","stage":"5","accepted":true}`, 200)
	if f.calls[len(f.calls)-1] != "tos:123:5" {
		t.Fatal("wrong terms notice")
	}
	before := len(f.calls)
	for _, bad := range []struct{ route, body string }{
		{"follow-newsletter-invite", `{"invite":"https://elsewhere.test/channel/Abcdef99"}`},
		{"newsletter-reaction", `{"id":"123@newsletter","serverId":1}`},
		{"newsletter-mark-viewed", `{"id":"123@newsletter","serverIds":[1,1]}`},
		{"newsletter-mark-viewed", `{"id":"123@g.us","serverIds":[1]}`},
		{"newsletter-message-updates", `{"id":"123@newsletter","before":2}`},
		{"newsletter-message-updates", `{"id":"123@newsletter","count":0}`},
		{"newsletter-accept-tos", `{"noticeId":"123","stage":"5"}`},
		{"newsletter-admin-invite", `{"id":"123@newsletter","phone":"123","thumbnail":"https://invalid.test/image.jpg"}`},
	} {
		post(bad.route, bad.body, 400)
	}
	if len(f.calls) != before {
		t.Fatalf("invalid input caused SDK calls: %v", f.calls[before:])
	}
	body := `{"id":"123@newsletter","serverIds":[5,7]}`
	if w := call("newsletter-mark-viewed", body, h.tokens["beta"], "k"); w.Code != 401 {
		t.Fatal("cross-session token accepted", w.Code)
	}
	first := call("newsletter-mark-viewed", body, h.tokens["alpha"], "k")
	again := call("newsletter-mark-viewed", body, h.tokens["alpha"], "k")
	if first.Code != 200 || again.Body.String() != first.Body.String() || again.Header().Get("Idempotency-Replayed") != "true" || len(f.calls) != before+1 || !reflect.DeepEqual(f.ids, []types.MessageServerID{5, 7}) {
		t.Fatal("view replay duplicated or changed the effect", first.Body.String(), again.Body.String(), f.calls)
	}
	if w := call("newsletter-mark-viewed", strings.Replace(body, "5,7", "7,8", 1), h.tokens["alpha"], "k"); w.Code != 409 || len(f.calls) != before+1 {
		t.Fatal("changed view command not blocked", w.Code)
	}
	for _, c := range []struct {
		path   string
		status int
	}{{"newsletter-write-result", 200}, {"newsletter-admin-result", 404}, {"send-result", 404}} {
		req := httptest.NewRequest("GET", "/api/alpha/"+c.path, nil)
		req.Header.Set("Authorization", "Bearer "+h.tokens["alpha"])
		req.Header.Set("Idempotency-Key", "k")
		res := httptest.NewRecorder()
		h.router.ServeHTTP(res, req)
		if res.Code != c.status {
			t.Fatalf("result lookup %s: %d", c.path, res.Code)
		}
	}
	f.role = types.NewsletterRoleSubscriber
	invite := `{"id":"123@newsletter","phone":"555","caption":"Join","expiresInSeconds":90}`
	post("newsletter-admin-invite", invite, 403)
	if len(h.client.sent) != 0 {
		t.Fatal("subscriber sent admin invite")
	}
	f.role = types.NewsletterRoleOwner
	output = post("newsletter-admin-invite", invite, 201)
	msg := h.client.sent[0].GetNewsletterAdminInviteMessage()
	if msg.GetNewsletterName() != "Verified channel name" || msg.GetNewsletterJID() != "123@newsletter" || msg.GetCaption() != "Join" || msg.GetInviteExpiration() < time.Now().Unix()+85 || !strings.Contains(output, `"roleGranted":false`) {
		t.Fatal("incorrect invitation message")
	}
	f.failure = errors.New("private upstream failure detail")
	output = post("newsletter-mark-viewed", body, 502)
	if strings.Contains(output, "private upstream") {
		t.Fatal("upstream error exposed")
	}
}
