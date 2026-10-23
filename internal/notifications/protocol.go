// Package notifications exposes safe protocol event metadata, never raw nodes.
package notifications

import (
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func call(kind string, v types.BasicCallMeta) (string, map[string]any) {
	return "oncall", map[string]any{"type": kind, "id": v.CallID, "from": v.From.ToNonAD().String(), "creator": v.CallCreator.ToNonAD().String(), "groupId": v.GroupJID.String(), "t": v.Timestamp.Unix()}
}
func Protocol(evt any) (string, map[string]any) {
	switch v := evt.(type) {
	case *events.Receipt:
		if v == nil {
			return "", nil
		}
		ids := v.MessageIDs[:min(len(v.MessageIDs), 1000)]
		return "onack", map[string]any{"chatId": v.Chat.ToNonAD().String(), "author": v.Sender.ToNonAD().String(), "messageIds": ids, "type": v.Type, "t": v.Timestamp.Unix()}
	case *events.LabelEdit:
		if v != nil && v.Action != nil {
			return "onlabelchanged", map[string]any{"id": v.LabelID, "name": v.Action.GetName(), "color": v.Action.GetColor(), "deleted": v.Action.GetDeleted(), "t": v.Timestamp.UnixMilli()}
		}
	case *events.LabelAssociationChat:
		if v != nil && v.Action != nil {
			return "onlabelassociation", map[string]any{"labelId": v.LabelID, "chatId": v.JID.String(), "value": v.Action.GetLabeled(), "t": v.Timestamp.UnixMilli()}
		}
	case *events.LabelAssociationMessage:
		if v != nil && v.Action != nil {
			return "onlabelassociation", map[string]any{"labelId": v.LabelID, "chatId": v.JID.String(), "messageId": v.MessageID, "value": v.Action.GetLabeled(), "t": v.Timestamp.UnixMilli()}
		}
	case *events.CallOffer:
		if v != nil {
			return call("offer", v.BasicCallMeta)
		}
	case *events.CallAccept:
		if v != nil {
			return call("accept", v.BasicCallMeta)
		}
	case *events.CallTerminate:
		if v != nil {
			return call("terminate", v.BasicCallMeta)
		}
	case *events.CallReject:
		if v != nil {
			return call("reject", v.BasicCallMeta)
		}
	case *events.NewsletterJoin:
		if v != nil {
			return "onnewsletter", map[string]any{"type": "join", "id": v.ID.String()}
		}
	case *events.NewsletterLeave:
		if v != nil {
			return "onnewsletter", map[string]any{"type": "leave", "id": v.ID.String(), "role": v.Role}
		}
	case *events.NewsletterMuteChange:
		if v != nil {
			return "onnewsletter", map[string]any{"type": "mute", "id": v.ID.String()}
		}
	case *events.NewsletterLiveUpdate:
		if v != nil {
			return "onnewsletter", map[string]any{"type": "update", "id": v.JID.String(), "messageCount": len(v.Messages)}
		}
	case *events.PrivacySettings:
		if v != nil {
			return "onprivacychanged", map[string]any{"settings": v.NewSettings}
		}
	case *events.UserAbout:
		if v != nil {
			return "onprofilechanged", map[string]any{"type": "status", "id": v.JID.String(), "status": v.Status, "t": v.Timestamp.Unix()}
		}
	case *events.Picture:
		if v != nil {
			return "onprofilechanged", map[string]any{"type": "picture", "id": v.JID.String(), "pictureId": v.PictureID, "removed": v.Remove, "t": v.Timestamp.Unix()}
		}
	case *events.Blocklist:
		if v != nil {
			return "onblocklistchanged", map[string]any{"action": v.Action, "changeCount": len(v.Changes)}
		}
	}
	return "", nil
}
