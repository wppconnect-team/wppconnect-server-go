package media

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"strings"
)

func bounded(value string, size int) string {
	if len(value) > size {
		value = value[:size]
	}
	return strings.ToValidUTF8(value, "")
}

func pollCreation(msg *waE2E.Message) *waE2E.PollCreationMessage {
	for _, poll := range []*waE2E.PollCreationMessage{msg.GetPollCreationMessage(), msg.GetPollCreationMessageV2(), msg.GetPollCreationMessageV3(), msg.GetPollCreationMessageV5(), msg.GetPollCreationMessageV6()} {
		if poll != nil {
			return poll
		}
	}
	return nil
}

// Event normalizes content while keeping the original webhook's id/from/body.
// Encryption keys and WhatsApp URLs are deliberately not exposed to consumers.
func Event(evt *events.Message) map[string]any {
	if evt == nil {
		return nil
	}
	msg := Unwrap(evt.Message)
	if msg == nil {
		msg = Unwrap(evt.RawMessage)
	}
	from := evt.Info.Chat.String()
	if evt.Info.Chat.IsEmpty() {
		from = evt.Info.Sender.String()
	}
	out := map[string]any{
		"id": evt.Info.ID, "from": from, "author": evt.Info.Sender.String(),
		"chatId": from, "fromMe": evt.Info.IsFromMe, "isGroupMsg": evt.Info.IsGroup,
		"body": msg.GetConversation(), "type": "chat", "isMedia": false,
		"t": evt.Info.Timestamp.Unix(),
	}
	if msg.GetExtendedTextMessage() != nil {
		out["body"] = msg.GetExtendedTextMessage().GetText()
	}
	switch {
	case msg.GetReactionMessage() != nil:
		m := msg.GetReactionMessage()
		out["type"], out["body"], out["messageId"] = "reaction", bounded(m.GetText(), 64), bounded(m.GetKey().GetID(), 256)
	case msg.GetProtocolMessage() != nil:
		m := msg.GetProtocolMessage()
		out["type"] = "protocol"
		if m.GetType() == waE2E.ProtocolMessage_REVOKE {
			out["type"] = "revoked"
		}
		if m.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
			out["type"] = "edited"
			out["body"] = bounded(m.GetEditedMessage().GetConversation(), 65536)
			if m.GetEditedMessage().GetExtendedTextMessage() != nil {
				out["body"] = bounded(m.GetEditedMessage().GetExtendedTextMessage().GetText(), 65536)
			}
		}
		out["messageId"] = bounded(m.GetKey().GetID(), 256)
	case msg.GetButtonsResponseMessage() != nil:
		m := msg.GetButtonsResponseMessage()
		out["type"], out["selectedId"], out["body"] = "buttons_response", bounded(m.GetSelectedButtonID(), 128), bounded(m.GetSelectedDisplayText(), 1024)
	case msg.GetListResponseMessage() != nil:
		m := msg.GetListResponseMessage()
		out["type"], out["selectedId"], out["body"] = "list_response", bounded(m.GetSingleSelectReply().GetSelectedRowID(), 128), bounded(m.GetTitle(), 1024)
	case msg.GetVideoMessage() != nil:
		m := msg.GetVideoMessage()
		out["type"], out["body"], out["caption"] = "video", m.GetCaption(), m.GetCaption()
		out["mimetype"], out["size"], out["isMedia"] = m.GetMimetype(), m.GetFileLength(), true
		out["duration"], out["width"], out["height"] = m.GetSeconds(), m.GetWidth(), m.GetHeight()
	case msg.GetStickerMessage() != nil:
		m := msg.GetStickerMessage()
		out["type"], out["isMedia"] = "sticker", true
		out["mimetype"], out["size"] = m.GetMimetype(), m.GetFileLength()
		out["width"], out["height"], out["isAnimated"] = m.GetWidth(), m.GetHeight(), m.GetIsAnimated()
	case msg.GetContactMessage() != nil:
		m := msg.GetContactMessage()
		out["type"], out["body"], out["displayName"] = "vcard", bounded(m.GetVcard(), 8192), bounded(m.GetDisplayName(), 1024)
	case msg.GetContactsArrayMessage() != nil:
		m := msg.GetContactsArrayMessage()
		cards := make([]map[string]string, 0, min(len(m.GetContacts()), 20))
		for _, card := range m.GetContacts()[:min(len(m.GetContacts()), 20)] {
			cards = append(cards, map[string]string{"displayName": bounded(card.GetDisplayName(), 1024), "vcard": bounded(card.GetVcard(), 8192)})
		}
		out["type"], out["contacts"], out["displayName"] = "multi_vcard", cards, bounded(m.GetDisplayName(), 1024)
		out["truncated"] = len(m.GetContacts()) > 20
	case msg.GetLocationMessage() != nil:
		m := msg.GetLocationMessage()
		out["type"], out["lat"], out["lng"] = "location", m.GetDegreesLatitude(), m.GetDegreesLongitude()
		out["title"], out["address"] = bounded(m.GetName(), 1024), bounded(m.GetAddress(), 4096)
	case pollCreation(msg) != nil:
		m := pollCreation(msg)
		choices := make([]string, 0, min(len(m.GetOptions()), 12))
		for _, option := range m.GetOptions()[:min(len(m.GetOptions()), 12)] {
			choices = append(choices, bounded(option.GetOptionName(), 400))
		}
		out["type"], out["name"], out["choices"] = "poll_creation", bounded(m.GetName(), 1020), choices
		out["selectableCount"], out["truncated"] = m.GetSelectableOptionsCount(), len(m.GetOptions()) > 12
	case msg.GetPollUpdateMessage() != nil:
		out["type"], out["pollId"] = "poll_update", bounded(msg.GetPollUpdateMessage().GetPollCreationMessageKey().GetID(), 256)
	case msg.GetImageMessage() != nil:
		m := msg.GetImageMessage()
		out["type"], out["body"], out["caption"] = "image", m.GetCaption(), m.GetCaption()
		out["mimetype"], out["size"], out["isMedia"] = m.GetMimetype(), m.GetFileLength(), true
	case msg.GetDocumentMessage() != nil:
		m := msg.GetDocumentMessage()
		out["type"], out["body"], out["caption"] = "document", m.GetCaption(), m.GetCaption()
		out["mimetype"], out["size"], out["isMedia"] = m.GetMimetype(), m.GetFileLength(), true
		out["filename"] = Filename(m.GetFileName())
	case msg.GetAudioMessage() != nil:
		m := msg.GetAudioMessage()
		out["type"] = "audio"
		if m.GetPTT() {
			out["type"] = "ptt"
		}
		out["mimetype"], out["size"], out["isMedia"] = m.GetMimetype(), m.GetFileLength(), true
		out["duration"] = m.GetSeconds()
	}
	return out
}
