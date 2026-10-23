package messages

import (
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
	"time"
)

func TestActionsEnforceOwnershipWindowAndSafeQuotes(t *testing.T) {
	now := time.Now()
	r := history.Record{ID: "id", Chat: "123@g.us", Sender: "456@s.whatsapp.net", Timestamp: now.Unix(), Content: map[string]any{"body": "safe text"}, Message: &waE2E.Message{Conversation: proto.String("safe text"), MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: []byte("private")}}}
	msg, err := Text("reply", []string{"456@s.whatsapp.net"}, &r)
	if err != nil {
		t.Fatal(err)
	}
	quote := msg.GetExtendedTextMessage().GetContextInfo().GetQuotedMessage()
	if quote.GetMessageContextInfo() != nil || quote.GetConversation() != "safe text" {
		t.Fatal("private quote leaked")
	}
	for _, kind := range []string{"edit-message", "delete-message"} {
		if _, err = Action(kind, r, "edit", now); err == nil {
			t.Fatal("foreign ownership accepted")
		}
	}
	r.FromMe = true
	r.Timestamp = now.Add(-21 * time.Minute).Unix()
	if _, err = Action("edit-message", r, "edit", now); err == nil {
		t.Fatal("expired edit accepted")
	}
	r.Deleted = true
	if _, err = Text("reply", nil, &r); err == nil {
		t.Fatal("deleted quote accepted")
	}
	for _, kind := range []string{"send-buttons", "send-list"} {
		if _, err = Interactive(kind, "", "body", "", "Choose", []Choice{{ID: "x", Text: "Yes"}, {ID: "x", Text: "No"}}); err == nil {
			t.Fatal("duplicate choice accepted")
		}
	}
	if _, err = Text(strings.Repeat("a", 65537), nil, nil); err == nil {
		t.Fatal("oversized text accepted")
	}
}
