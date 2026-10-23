package session

import (
	"context"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

func TestPollSDKEncryptionAndPersistedResults(t *testing.T) {
	ctx := context.Background()
	m, _ := managerForTest(t, t.TempDir())
	seedDevice(t, m, "alpha")
	h, err := m.Start(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	sender := types.NewJID("456", types.DefaultUserServer)
	own := h.Client.Store.ID.ToNonAD()
	poll := h.Client.BuildPollCreation("Decision", []string{"Yes", "No"}, 1)
	info := types.MessageInfo{ID: "poll", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: sender, Sender: sender}}
	if err = h.History.Put(ctx, info, poll); err != nil {
		t.Fatal(err)
	}
	if err = h.Client.Store.MsgSecrets.PutMessageSecret(ctx, sender, sender, "poll", poll.GetMessageContextInfo().GetMessageSecret()); err != nil {
		t.Fatal(err)
	}
	vote, err := h.Client.BuildPollVote(ctx, &info, []string{"Yes"})
	if err != nil {
		t.Fatal(err)
	}
	// Both ends use real SDK crypto and the real SQLite secret store; no network.
	event := &events.Message{Info: types.MessageInfo{ID: "vote", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: sender, Sender: own, IsFromMe: true}}, Message: vote}
	m.forwardMessage(h, event)
	result, err := h.History.PollResults(ctx, sender.String(), "poll")
	if err != nil || result["counts"].(map[string]int)["Yes"] != 1 {
		t.Fatalf("decrypted vote %#v %v", result, err)
	}
	vote.PollUpdateMessage.Vote.EncPayload[0] ^= 1
	event.Info.ID = "tampered"
	m.forwardMessage(h, event)
	result, _ = h.History.PollResults(ctx, sender.String(), "poll")
	if result["counts"].(map[string]int)["Yes"] != 1 {
		t.Fatal("tampered vote changed results")
	}
}

func TestIncomingEditsCheckAuthorAndTimestamp(t *testing.T) {
	ctx := context.Background()
	m, _ := managerForTest(t, t.TempDir())
	h, err := m.Start(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	chat := types.NewJID("123-456", types.GroupServer)
	sender := types.NewJID("456", types.DefaultUserServer)
	at := time.Now()
	info := types.MessageInfo{ID: "original", Timestamp: at, MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: true}}
	h.History.Put(ctx, info, &waE2E.Message{Conversation: proto.String("original")})
	change := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(), Key: &waCommon.MessageKey{ID: proto.String("original"), RemoteJID: proto.String(chat.String())}, TimestampMS: proto.Int64(at.Add(time.Second).UnixMilli()), EditedMessage: &waE2E.Message{Conversation: proto.String("edited")}}}
	event := &events.Message{Info: info, Message: change}
	event.Info.Sender = types.NewJID("789", types.DefaultUserServer)
	m.applyMessageChange(ctx, h, event)
	record, _ := h.History.Get(ctx, chat.String(), "original")
	if record.Edited {
		t.Fatal("foreign author changed history")
	}
	event.Info.Sender = sender
	m.applyMessageChange(ctx, h, event)
	record, _ = h.History.Get(ctx, chat.String(), "original")
	if !record.Edited || record.Message.GetConversation() != "edited" {
		t.Fatal("author edit not applied")
	}
	change.ProtocolMessage.TimestampMS = proto.Int64(at.UnixMilli())
	change.ProtocolMessage.EditedMessage.Conversation = proto.String("stale")
	m.applyMessageChange(ctx, h, event)
	record, _ = h.History.Get(ctx, chat.String(), "original")
	if record.Message.GetConversation() != "edited" {
		t.Fatal("stale edit overwrote newer content")
	}
}
