package groups

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestGroupAndParticipantIDs(t *testing.T) {
	for _, value := range []string{"123", "123-456@g.us"} {
		if _, err := ParseGroup(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "../123", "123@c.us", "a@g.us", "123--456", "123:4@g.us"} {
		if _, err := ParseGroup(value); err == nil {
			t.Fatalf("invalid group accepted: %s", value)
		}
	}
	for _, tc := range []struct{ input, want string }{{"+55 (11) 123-456", "5511123456@s.whatsapp.net"}, {"123@c.us", "123@s.whatsapp.net"}, {"123@lid", "123@lid"}} {
		jid, err := ParseParticipant(tc.input)
		if err != nil || jid.String() != tc.want {
			t.Fatalf("%q: %v %v", tc.input, jid, err)
		}
	}
	for _, value := range []string{"", "abc", "123@g.us", "123@evil.test", "123:4@s.whatsapp.net"} {
		if _, err := ParseParticipant(value); err == nil {
			t.Fatalf("invalid participant accepted: %s", value)
		}
	}
	list, err := ParseList(json.RawMessage(`"123;123@g.us,456"`), true, 20)
	if err != nil || len(list) != 2 {
		t.Fatalf("normalization/dedup: %v %v", list, err)
	}
	for _, raw := range []string{`null`, `42`, `["123",null]`, `[]`, `["123",42]`} {
		if _, err := ParseList(json.RawMessage(raw), true, 20); err == nil {
			t.Fatalf("invalid list accepted: %s", raw)
		}
	}
	if _, err := Strings(json.RawMessage(`["a","b"]`), 1); err == nil {
		t.Fatal("list limit ignored")
	}
}

func TestInviteValidation(t *testing.T) {
	for _, value := range []string{"Abcd1234", "https://chat.whatsapp.com/Abcd1234", "https://chat.whatsapp.com/invite/Abcd1234?mode=sample"} {
		code, err := Invite(value)
		if err != nil || code != "Abcd1234" {
			t.Fatalf("%s: %s %v", value, code, err)
		}
	}
	for _, value := range []string{"", "http://chat.whatsapp.com/Abcd1234", "https://chat.whatsapp.com.evil.test/Abcd1234", "https://u@chat.whatsapp.com/Abcd1234", "https://chat.whatsapp.com/a/b", strings.Repeat("a", 129)} {
		if _, err := Invite(value); err == nil {
			t.Fatalf("invalid invitation accepted: %s", value)
		}
	}
}

func TestGroupNotifications(t *testing.T) {
	author := types.NewJID("123", types.DefaultUserServer)
	evt := &events.GroupInfo{JID: types.NewJID("123-456", types.GroupServer), Sender: &author, Timestamp: time.Unix(1000, 0),
		Join: []types.JID{types.NewJID("456", types.DefaultUserServer)}, Leave: []types.JID{types.NewJID("789", types.HiddenUserServer)},
		Promote: []types.JID{types.NewJID("456", types.DefaultUserServer)}, Demote: []types.JID{types.NewJID("789", types.HiddenUserServer)},
		Name: &types.GroupName{Name: "Changed"}, Locked: &types.GroupLocked{IsLocked: false}, Announce: &types.GroupAnnounce{IsAnnounce: true},
	}
	list := Notifications(evt)
	if len(list) != 5 {
		t.Fatalf("missing event changes: %+v", list)
	}
	for i, operation := range []string{"add", "remove", "promote", "demote"} {
		n := list[i]
		if n.Event != "onparticipantschanged" || n.Data["groupId"] != "123-456@g.us" || n.Data["by"] != "123@c.us" || n.Data["operation"] != operation || n.Data["action"] != operation {
			t.Fatal(n)
		}
		who := n.Data["who"].([]string)
		if len(who) != 1 {
			t.Fatal(n)
		}
	}
	changes := list[4].Data["changes"].(map[string]any)
	if changes["restrict"] != false || changes["announcement"] != true || changes["subject"] != "Changed" {
		t.Fatal(changes)
	}
	if evt.Name.Name != "Changed" {
		t.Fatal("event modified")
	}
	for _, value := range []any{nil, (*events.GroupInfo)(nil), (*events.JoinedGroup)(nil), (*events.Picture)(nil), &events.Message{}, &events.Picture{JID: author}} {
		if len(Notifications(value)) != 0 {
			t.Fatalf("unrelated/private event forwarded: %T", value)
		}
	}
}

func TestJoinedGroupVoluntaryLeaveAndInviteChanges(t *testing.T) {
	person := types.NewJID("123", types.DefaultUserServer)
	group := types.NewJID("456", types.GroupServer)
	leave := Notifications(&events.GroupInfo{JID: group, Sender: &person, Leave: []types.JID{person}})
	if leave[0].Data["action"] != "leaver" || leave[0].Data["operation"] != "remove" {
		t.Fatal(leave)
	}
	join := Notifications(&events.GroupInfo{JID: group, JoinReason: "invite", Join: []types.JID{person}})
	if join[0].Data["action"] != "join" {
		t.Fatal(join)
	}
	secret := "private-invite-code"
	n := Notifications(&events.GroupInfo{JID: group, NewInviteLink: &secret})
	encoded, _ := json.Marshal(n)
	if strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), "inviteLinkChanged") {
		t.Fatal(string(encoded))
	}
	added := Notifications(&events.JoinedGroup{Reason: "invite", GroupInfo: types.GroupInfo{JID: group, GroupName: types.GroupName{Name: "Team"}}})
	if added[0].Event != "onaddedtogroup" || added[0].Data["by"] != nil || added[0].Data["group"].(map[string]any)["name"] != "Team" {
		t.Fatal(added)
	}
	picture := Notifications(&events.Picture{JID: group, Remove: true})
	if picture[0].Data["changes"].(map[string]any)["pictureRemoved"] != true {
		t.Fatal(picture)
	}
}
