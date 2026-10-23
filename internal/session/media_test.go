package session

import (
	"testing"

	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type checkingSink struct {
	t      *testing.T
	h      *Handle
	called bool
}

func (*checkingSink) OnQR(string, string) {}
func (*checkingSink) OnConnected(string)  {}
func (s *checkingSink) OnMessage(name string, evt *events.Message) {
	s.called = true
	if name != s.h.Name {
		s.t.Fatalf("wrong session: %s", name)
	}
	if _, ok := s.h.Media.Get(evt.Info.ID); !ok {
		s.t.Fatal("webhook dispatched before download descriptor was available")
	}
}

func TestIncomingAttachmentAvailableBeforeWebhook(t *testing.T) {
	for _, msg := range []*waE2E.Message{
		{DocumentMessage: &waE2E.DocumentMessage{DirectPath: proto.String("/media"), FileName: proto.String("report.pdf")}},
		{VideoMessage: &waE2E.VideoMessage{DirectPath: proto.String("/media"), Mimetype: proto.String("video/mp4")}},
		{StickerMessage: &waE2E.StickerMessage{DirectPath: proto.String("/media"), Mimetype: proto.String("image/webp")}},
	} {
		h := &Handle{Name: "alpha", Media: media.NewStore()}
		sink := &checkingSink{t: t, h: h}
		mgr := &Manager{sink: sink}
		evt := &events.Message{Info: types.MessageInfo{ID: "received-id"}, RawMessage: msg}
		mgr.forwardMessage(h, evt)
		if !sink.called {
			t.Fatal("event not forwarded")
		}
		other := &Handle{Name: "beta", Media: media.NewStore()}
		if _, ok := other.Media.Get(evt.Info.ID); ok {
			t.Fatal("event registered in another session")
		}
	}
}
