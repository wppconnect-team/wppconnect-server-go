package messages

import (
	"errors"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func MessageKey(r history.Record) *waCommon.MessageKey {
	key := &waCommon.MessageKey{RemoteJID: proto.String(r.Chat), ID: proto.String(r.ID), FromMe: proto.Bool(r.FromMe)}
	chat, _ := types.ParseJID(r.Chat)
	if chat.Server == types.GroupServer && !r.FromMe {
		key.Participant = proto.String(r.Sender)
	}
	return key
}

// Text uses only a persisted quote in the same chat. Callers cannot inject
// arbitrary quoted protocol objects, media URLs or message secrets.
func Text(text string, mentions []string, quote *history.Record) (*waE2E.Message, error) {
	if !validText(text, 65536, true) || len(mentions) > 100 {
		return nil, errors.New("text must contain 1..65536 characters; at most 100 mentions")
	}
	ctx := &waE2E.ContextInfo{MentionedJID: mentions}
	if quote != nil {
		if quote.Deleted {
			return nil, errors.New("cannot quote a deleted message")
		}
		ctx.StanzaID = proto.String(quote.ID)
		ctx.Participant = proto.String(quote.Sender)
		// Quote display content only, never the poll secret or cached media keys.
		body, _ := quote.Content["body"].(string)
		ctx.QuotedMessage = &waE2E.Message{Conversation: proto.String(body)}
	}
	if quote == nil && len(mentions) == 0 {
		return &waE2E.Message{Conversation: proto.String(text)}, nil
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ctx}}, nil
}

func Action(kind string, r history.Record, value string, now time.Time) (*waE2E.Message, error) {
	if r.Deleted {
		return nil, errors.New("message was already deleted")
	}
	switch kind {
	case "send-reaction":
		if !validText(value, 16, false) {
			return nil, errors.New("reaction must fit in 16 characters; empty removes it")
		}
		return &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: MessageKey(r), Text: proto.String(value), SenderTimestampMS: proto.Int64(now.UnixMilli())}}, nil
	case "edit-message":
		if !r.FromMe {
			return nil, errors.New("only own messages can be edited")
		}
		if now.Sub(time.Unix(r.Timestamp, 0)) > whatsmeow.EditWindow {
			return nil, errors.New("message is outside the SDK edit window")
		}
		if r.Message.GetConversation() == "" && r.Message.GetExtendedTextMessage() == nil {
			return nil, errors.New("only text editing is supported")
		}
		content, err := Text(value, nil, nil)
		if err != nil {
			return nil, err
		}
		return (&whatsmeow.Client{}).BuildEdit(r.Info().Chat, r.ID, content), nil
	case "delete-message":
		if !r.FromMe {
			return nil, errors.New("only own messages can be revoked through this route")
		}
		return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: MessageKey(r)}}, nil
	}
	return nil, errors.New("unknown message action")
}
