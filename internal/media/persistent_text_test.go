package media

import (
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestPersistentTextOnlyForOrdinaryEnvelope(t *testing.T) {
	plain := &waE2E.Message{Conversation: proto.String("text")}
	extended := func(ctx *waE2E.ContextInfo) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("text"), ContextInfo: ctx}}
	}
	vectors := []struct {
		msg  *events.Message
		want bool
	}{
		{&events.Message{Message: plain}, true},
		{&events.Message{Message: extended(nil)}, true},
		{&events.Message{Message: extended(&waE2E.ContextInfo{Expiration: proto.Uint32(120)})}, false},
		{&events.Message{Message: plain, IsEphemeral: true}, false},
		{&events.Message{Message: plain, IsViewOnce: true}, false},
		{&events.Message{Message: plain, RawMessage: &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: plain}}}, false},
		{&events.Message{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("photo")}}}, false},
	}
	for i, v := range vectors {
		got, _ := Event(v.msg)["persistentText"].(bool)
		if got != v.want {
			t.Errorf("case %d: %t", i, got)
		}
	}
}
