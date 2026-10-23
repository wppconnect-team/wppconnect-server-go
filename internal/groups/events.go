package groups

import (
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Notification struct {
	Event string
	Data  map[string]any
}

func actor(jid *types.JID) any {
	if jid == nil {
		return nil
	}
	return optionalID(*jid)
}

// Notifications splits multi-action updates into Node participant events and
// Go metadata events. Protocol nodes and invitation secrets are never exposed.
func Notifications(evt any) []Notification {
	switch v := evt.(type) {
	case *events.GroupInfo:
		if v == nil || v.JID.Server != types.GroupServer {
			return nil
		}
		out := make([]Notification, 0, 5)
		for _, change := range []struct {
			operation string
			people    []types.JID
		}{
			{"add", v.Join}, {"remove", v.Leave}, {"promote", v.Promote}, {"demote", v.Demote},
		} {
			if len(change.people) == 0 {
				continue
			}
			action := change.operation
			if action == "add" && v.JoinReason == "invite" {
				action = "join"
			}
			if action == "remove" && len(change.people) == 1 && ((v.Sender != nil && *v.Sender == change.people[0]) || (v.SenderPN != nil && *v.SenderPN == change.people[0])) {
				action = "leaver"
			}
			who := make([]string, 0, len(change.people))
			for _, p := range change.people {
				who = append(who, ID(p))
			}
			out = append(out, Notification{Event: "onparticipantschanged", Data: map[string]any{
				"groupId": v.JID.String(), "by": actor(v.Sender), "action": action, "operation": change.operation, "who": who, "timestamp": Timestamp(v.Timestamp),
			}})
		}
		changes := make(map[string]any)
		if v.Name != nil {
			changes["subject"] = v.Name.Name
		}
		if v.Topic != nil {
			changes["description"] = v.Topic.Topic
			if v.Topic.TopicDeleted {
				changes["description"] = nil
			}
		}
		if v.Locked != nil {
			changes["restrict"] = v.Locked.IsLocked
		}
		if v.Announce != nil {
			changes["announcement"] = v.Announce.IsAnnounce
		}
		if v.Ephemeral != nil {
			changes["ephemeralDuration"] = v.Ephemeral.DisappearingTimer
		}
		if v.MembershipApprovalMode != nil {
			changes["membershipApprovalRequired"] = v.MembershipApprovalMode.IsJoinApprovalRequired
		}
		if v.NewInviteLink != nil {
			changes["inviteLinkChanged"] = true
		}
		if v.Delete != nil {
			changes["deleted"] = v.Delete.Deleted
		}
		if v.Suspended {
			changes["suspended"] = true
		}
		if v.Unsuspended {
			changes["suspended"] = false
		}
		if v.Link != nil {
			changes["linkedGroup"] = v.Link.Group.JID.String()
		}
		if v.Unlink != nil {
			changes["unlinkedGroup"] = v.Unlink.Group.JID.String()
		}
		if len(changes) > 0 {
			out = append(out, Notification{Event: "ongroupchange", Data: map[string]any{"groupId": v.JID.String(), "by": actor(v.Sender), "timestamp": Timestamp(v.Timestamp), "changes": changes}})
		}
		return out
	case *events.JoinedGroup:
		if v == nil || v.JID.Server != types.GroupServer {
			return nil
		}
		return []Notification{{Event: "onaddedtogroup", Data: map[string]any{"groupId": v.JID.String(), "by": actor(v.Sender), "reason": v.Reason, "type": v.Type, "group": Info(&v.GroupInfo)}}}
	case *events.Picture:
		if v == nil || v.JID.Server != types.GroupServer {
			return nil
		}
		return []Notification{{Event: "ongroupchange", Data: map[string]any{"groupId": v.JID.String(), "by": optionalID(v.Author), "timestamp": Timestamp(v.Timestamp), "changes": map[string]any{"pictureId": v.PictureID, "pictureRemoved": v.Remove}}}}
	}
	return nil
}
