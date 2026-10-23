// Package directory implements contacts, an observed chat index and presence.
package directory

import (
	"context"
	"errors"
	"strings"

	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type Client interface {
	AllContacts(context.Context) (map[types.JID]types.ContactInfo, error)
	Contact(context.Context, types.JID) (types.ContactInfo, error)
	Mapping(context.Context, types.JID) (types.JID, error)
	SendPresence(context.Context, types.Presence) error
	SubscribePresence(context.Context, types.JID) error
	SendChatPresence(context.Context, types.JID, types.ChatPresence, types.ChatPresenceMedia) error
	GetUserInfo(context.Context, []types.JID) (map[types.JID]types.UserInfo, error)
	GetProfilePictureInfo(context.Context, types.JID, *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error)
}

type SDK struct{ *whatsmeow.Client }

func (s SDK) AllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return s.Store.Contacts.GetAllContacts(ctx)
}
func (s SDK) Contact(ctx context.Context, jid types.JID) (types.ContactInfo, error) {
	return s.Store.Contacts.GetContact(ctx, jid)
}
func (s SDK) Mapping(ctx context.Context, jid types.JID) (types.JID, error) {
	if jid.Server == types.HiddenUserServer {
		return s.Store.LIDs.GetPNForLID(ctx, jid)
	}
	return s.Store.LIDs.GetLIDForPN(ctx, jid)
}

func Parse(value string, group, lid bool) (types.JID, error) {
	if group && lid {
		return types.EmptyJID, errors.New("isGroup and isLid conflict")
	}
	if lid {
		if !strings.Contains(value, "@") {
			value += "@lid"
		} else if !strings.HasSuffix(value, "@lid") {
			return types.EmptyJID, errors.New("isLid conflicts with the identifier")
		}
	}
	if group || strings.HasSuffix(value, "@g.us") {
		return groups.ParseGroup(value)
	}
	return groups.ParseParticipant(value)
}

func Supported(jid types.JID) bool {
	return jid.User != "" && len(jid.User) <= 128 && (jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer || jid.Server == types.GroupServer)
}

func ContactData(jid types.JID, c types.ContactInfo) map[string]any {
	name := c.FullName
	if name == "" {
		name = c.BusinessName
	}
	if name == "" {
		name = c.PushName
	}
	if name == "" {
		name = jid.User
	}
	return map[string]any{"id": groups.WID(jid), "name": name, "shortName": c.FirstName, "pushname": c.PushName, "verifiedName": c.BusinessName,
		"isBusiness": c.BusinessName != "", "isMyContact": c.FullName != "" || c.FirstName != "", "isUser": true, "isGroup": false}
}

func ChatData(c Chat) map[string]any {
	jid, _ := types.ParseJID(c.JID)
	return map[string]any{"id": groups.WID(jid), "name": c.Name, "formattedTitle": c.Name, "isGroup": jid.Server == types.GroupServer,
		"isUser": jid.Server != types.GroupServer, "t": c.T, "unreadCount": c.Unread, "unreadCountIsPartial": !c.UnreadKnown,
		"archive": c.Archived, "isArchived": c.Archived, "pin": c.Pinned, "isMuted": c.Muted, "muteUntil": c.MuteUntil}
}
