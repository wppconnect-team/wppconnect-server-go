// Package groups adapts whatsmeow group data to the Node HTTP contract.
package groups

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const MaxBodyBytes = 64 << 10

type Client interface {
	GetJoinedGroups(context.Context) ([]*types.GroupInfo, error)
	GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error)
	CreateGroup(context.Context, whatsmeow.ReqCreateGroup) (*types.GroupInfo, error)
	UpdateGroupParticipants(context.Context, types.JID, []types.JID, whatsmeow.ParticipantChange) ([]types.GroupParticipant, error)
	SetGroupName(context.Context, types.JID, string) error
	SetGroupDescription(context.Context, types.JID, string) error
	SetGroupLocked(context.Context, types.JID, bool) error
	SetGroupAnnounce(context.Context, types.JID, bool) error
	SetDisappearingTimer(context.Context, types.JID, time.Duration, time.Time) error
	GetGroupInviteLink(context.Context, types.JID, bool) (string, error)
	GetGroupInfoFromLink(context.Context, string) (*types.GroupInfo, error)
	JoinGroupWithLink(context.Context, string) (types.JID, error)
	LeaveGroup(context.Context, types.JID) error
	SetGroupPhoto(context.Context, types.JID, []byte) (string, error)
}

// Strings accepts Node's string, comma/semicolon-separated strings or arrays.
func Strings(raw json.RawMessage, limit int) ([]string, error) {
	var single string
	var values []string
	if json.Unmarshal(raw, &single) == nil {
		values = strings.FieldsFunc(single, func(r rune) bool { return r == ',' || r == ';' })
	} else if err := json.Unmarshal(raw, &values); err != nil {
		return nil, errors.New("expected a string or an array of strings")
	}
	if len(values) == 0 || len(values) > limit {
		return nil, errors.New("list is empty or exceeds the operation limit")
	}
	for i, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !utf8.ValidString(value) {
			return nil, errors.New("list contains an empty or invalid value")
		}
		values[i] = value
	}
	return values, nil
}

var groupUser = regexp.MustCompile(`^[0-9]+(-[0-9]+)?$`)
var userNumber = regexp.MustCompile(`^[0-9]+$`)
var inviteCode = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)

func ParseGroup(value string) (types.JID, error) {
	user, server, hasServer := strings.Cut(strings.TrimSpace(value), "@")
	if len(user) > 128 || !groupUser.MatchString(user) || (hasServer && server != types.GroupServer) {
		return types.EmptyJID, errors.New("groupId must be a numeric group ID, optionally ending in @g.us")
	}
	return types.NewJID(user, types.GroupServer), nil
}

func ParseParticipant(value string) (types.JID, error) {
	user, server, hasServer := strings.Cut(strings.TrimSpace(value), "@")
	if !hasServer || server == types.LegacyUserServer {
		server = types.DefaultUserServer
	}
	if server != types.DefaultUserServer && server != types.HiddenUserServer {
		return types.EmptyJID, errors.New("participant must be a phone number or user JID")
	}
	if server == types.DefaultUserServer {
		user = strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(user)
	}
	if len(user) > 32 || !userNumber.MatchString(user) {
		return types.EmptyJID, errors.New("invalid participant number")
	}
	return types.NewJID(user, server), nil
}

func ParseList(raw json.RawMessage, group bool, limit int) ([]types.JID, error) {
	values, err := Strings(raw, limit)
	if err != nil {
		return nil, err
	}
	out := make([]types.JID, 0, len(values))
	seen := make(map[types.JID]bool)
	for _, value := range values {
		parse := ParseParticipant
		if group {
			parse = ParseGroup
		}
		jid, err := parse(value)
		if err != nil {
			return nil, err
		}
		if !seen[jid] {
			out = append(out, jid)
			seen[jid] = true
		}
	}
	return out, nil
}

func Invite(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "chat.whatsapp.com" || u.User != nil {
			return "", errors.New("expected a WhatsApp HTTPS invitation link")
		}
		value = strings.TrimPrefix(strings.TrimPrefix(u.Path, "/"), "invite/")
	}
	if !inviteCode.MatchString(value) {
		return "", errors.New("invalid invitation code")
	}
	return value, nil
}

// ID uses Node's c.us spelling for phone numbers and preserves explicit LIDs.
func ID(jid types.JID) string {
	jid = jid.ToNonAD()
	if jid.Server == types.DefaultUserServer {
		jid.Server = types.LegacyUserServer
	}
	if jid.IsEmpty() {
		return ""
	}
	return jid.String()
}

func WID(jid types.JID) map[string]any {
	jid = jid.ToNonAD()
	if jid.Server == types.DefaultUserServer {
		jid.Server = types.LegacyUserServer
	}
	return map[string]any{"user": jid.User, "server": jid.Server, "_serialized": ID(jid)}
}

func Participant(p types.GroupParticipant) map[string]any {
	out := map[string]any{"id": WID(p.JID), "isAdmin": p.IsAdmin || p.IsSuperAdmin, "isSuperAdmin": p.IsSuperAdmin}
	if !p.PhoneNumber.IsEmpty() {
		out["phoneNumber"] = ID(p.PhoneNumber)
	}
	if !p.LID.IsEmpty() {
		out["lid"] = ID(p.LID)
	}
	if p.DisplayName != "" {
		out["pushname"] = p.DisplayName
	}
	if p.Error != 0 && p.Error != 200 {
		out["error"] = p.Error
	}
	if p.AddRequest != nil {
		out["requiresInvite"] = true
	}
	return out
}

func Timestamp(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func optionalID(jid types.JID) any {
	if jid.IsEmpty() {
		return nil
	}
	return ID(jid)
}

func Info(g *types.GroupInfo) map[string]any {
	if g == nil {
		return nil
	}
	participants := make([]map[string]any, 0, len(g.Participants))
	for _, p := range g.Participants {
		participants = append(participants, map[string]any{"id": ID(p.JID), "isAdmin": p.IsAdmin || p.IsSuperAdmin})
	}
	var description any
	if g.Topic != "" && !g.TopicDeleted {
		description = g.Topic
	}
	return map[string]any{
		"id": g.JID.String(), "name": g.Name, "subject": g.Name, "description": description,
		"subjectUpdatedAt": Timestamp(g.NameSetAt), "subjectUpdatedBy": optionalID(g.NameSetBy),
		"descriptionUpdatedAt": Timestamp(g.TopicSetAt), "descriptionUpdatedBy": optionalID(g.TopicSetBy),
		"createdAt": Timestamp(g.GroupCreated), "lastActivityAt": nil, "participants": participants,
		"restrict": g.IsLocked, "announcement": g.IsAnnounce, "ephemeralDuration": g.DisappearingTimer,
	}
}

func Summary(g *types.GroupInfo) map[string]any {
	count := g.ParticipantCount
	if count < len(g.Participants) {
		count = len(g.Participants)
	}
	return map[string]any{"id": g.JID.String(), "name": g.Name, "participants": count}
}
