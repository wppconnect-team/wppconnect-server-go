package media

import "go.mau.fi/whatsmeow/types/events"

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
