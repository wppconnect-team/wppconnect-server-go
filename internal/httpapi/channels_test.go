package httpapi

import (
	"context"
	"errors"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roadmapChannels struct {
	*whatsmeow.Client
	sender         *fakeMediaClient
	calls          int
	descriptionErr error
}

func (f *roadmapChannels) Self() types.JID { return types.NewJID("999", types.DefaultUserServer) }
func (f *roadmapChannels) CreateGroup(ctx context.Context, req whatsmeow.ReqCreateGroup) (*types.GroupInfo, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("missing deadline")
	}
	f.calls++
	if !req.IsParent {
		panic("missing parent classification")
	}
	return &types.GroupInfo{JID: types.NewJID("123-456", types.GroupServer), GroupParent: req.GroupParent}, nil
}
func (f *roadmapChannels) SetGroupDescription(context.Context, types.JID, string) error {
	return f.descriptionErr
}
func (f *roadmapChannels) GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error) {
	f.calls++
	return &types.GroupInfo{}, nil
}
func (f *roadmapChannels) CreateNewsletter(context.Context, whatsmeow.CreateNewsletterParams) (*types.NewsletterMetadata, error) {
	f.calls++
	return &types.NewsletterMetadata{ID: types.NewJID("123", types.NewsletterServer)}, nil
}
func (f *roadmapChannels) FollowNewsletter(context.Context, types.JID) error   { f.calls++; return nil }
func (f *roadmapChannels) RejectCall(context.Context, types.JID, string) error { f.calls++; return nil }
func (f *roadmapChannels) BuildPollVote(_ context.Context, _ *types.MessageInfo, choices []string) (*waE2E.Message, error) {
	f.calls++
	return &waE2E.Message{Conversation: proto.String(strings.Join(choices, ","))}, nil
}
func (f *roadmapChannels) SendMessage(ctx context.Context, to types.JID, m *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	return f.sender.SendMessage(ctx, to, m, extra...)
}

func TestEvaluatedChannelsStatusAndExperimentalMessages(t *testing.T) {
	h := newMediaHarness(t)
	f := &roadmapChannels{sender: h.client}
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"), history.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	chat := types.NewJID("123", types.DefaultUserServer)
	poll := (&whatsmeow.Client{}).BuildPollCreation("Decision", []string{"Yes", "No"}, 1)
	err = store.Put(context.Background(), types.MessageInfo{ID: "poll", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: chat, Sender: chat}}, poll)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{SecretKey: "test-secret", ExperimentalInteractives: true}, channelSession: func(string) (channels.Client, bool) { return f, true }, historySession: func(string) (*history.Store, func(), error) { return store, func() {}, nil }, mediaSession: func(name string) (media.Client, *media.Store, bool) { v, ok := h.stores[name]; return h.client, v, ok }}
	h.router = s.router()
	for _, tc := range []struct {
		route string
		body  any
		code  int
	}{
		{"create-community", map[string]any{"name": "Support"}, 201},
		{"community-info", map[string]any{"id": "123-456@g.us"}, 400},
		{"create-newsletter", map[string]any{"name": "Updates", "description": "Product news"}, 201},
		{"follow-newsletter", map[string]any{"id": "123@newsletter"}, 200},
		{"follow-newsletter", map[string]any{"id": "123@s.whatsapp.net"}, 400},
		{"reject-call", map[string]any{"phone": "123", "callId": "call-1"}, 200},
		{"reject-call", map[string]any{"phone": "123@g.us", "callId": "call-1"}, 400},
		{"send-status-text", map[string]any{"message": "A status update"}, 201},
		{"send-status-text", map[string]any{"message": strings.Repeat("x", 1001)}, 400},
		{"send-buttons", map[string]any{"phone": "123", "message": "Choose", "choices": []map[string]string{{"id": "yes", "text": "Yes"}}}, 201},
		{"send-list", map[string]any{"phone": "123", "message": "Choose", "buttonText": "Options", "choices": []map[string]string{{"id": "yes", "text": "Yes"}}}, 201},
		{"vote-poll", map[string]any{"phone": "123", "messageId": "poll", "choices": []string{"Yes"}}, 201},
		{"vote-poll", map[string]any{"phone": "123", "messageId": "poll", "choices": []string{"Wrong"}}, 400},
		{"vote-poll", map[string]any{"phone": "123", "messageId": "poll", "choices": []string{"Yes", "No"}}, 400},
	} {
		w := h.post(t, tc.route, tc.body)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
	}
	result, err := store.PollResults(context.Background(), chat.String(), "poll")
	if err != nil || result["voters"] != 1 {
		t.Fatalf("local vote: %#v %v", result, err)
	}
	w := h.post(t, "vote-poll", map[string]any{"phone": "123", "messageId": "poll", "choices": []string{}})
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	result, _ = store.PollResults(context.Background(), chat.String(), "poll")
	if result["voters"] != 0 {
		t.Fatal("withdrawal ignored")
	}
	f.descriptionErr = errors.New("denied")
	w = h.post(t, "create-community", map[string]any{"name": "Support", "description": "Partial"})
	if w.Code != 502 || !strings.Contains(w.Body.String(), "completed") {
		t.Fatal("creation partial result lost", w.Body.String())
	}
}
