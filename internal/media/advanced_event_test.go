package media

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestAdvancedIncomingNormalization(t *testing.T) {
	poll := (&whatsmeow.Client{}).BuildPollCreation("Lunch?", []string{"A", "B"}, 1)
	contact := &waE2E.ContactMessage{DisplayName: proto.String("Ana"), Vcard: proto.String("BEGIN:VCARD\r\nEND:VCARD")}
	for _, tc := range []struct {
		kind  string
		msg   *waE2E.Message
		field string
		want  any
	}{
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("film"), Mimetype: proto.String("video/mp4"), Seconds: proto.Uint32(2), MediaKey: []byte("private-secret")}}, "duration", uint32(2)},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp"), IsAnimated: proto.Bool(true), MediaKey: []byte("private-secret")}}, "isAnimated", true},
		{"vcard", &waE2E.Message{ContactMessage: contact}, "displayName", "Ana"},
		{"multi_vcard", &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: []*waE2E.ContactMessage{contact, contact}}}, "truncated", false},
		{"location", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{DegreesLatitude: proto.Float64(0), DegreesLongitude: proto.Float64(-46), Name: proto.String("Office")}}, "lat", float64(0)},
		{"poll_creation", poll, "name", "Lunch?"},
		{"poll_creation", &waE2E.Message{PollCreationMessageV3: poll.GetPollCreationMessage()}, "name", "Lunch?"},
		{"poll_creation", &waE2E.Message{PollCreationMessageV4: &waE2E.FutureProofMessage{Message: poll}}, "name", "Lunch?"},
		{"poll_update", &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("poll-id")}, Vote: &waE2E.PollEncValue{EncPayload: []byte("private-secret")}}}, "pollId", "poll-id"},
	} {
		t.Run(tc.kind+tc.field, func(t *testing.T) {
			evt := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), Sender: types.NewJID("456", types.DefaultUserServer), IsGroup: true}, ID: "incoming"}, Message: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: tc.msg}}}
			got := Event(evt)
			if got["type"] != tc.kind || got[tc.field] != tc.want || got["id"] != "incoming" || got["isGroupMsg"] != true {
				t.Fatalf("event: %+v", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"private-secret", "cHJpdmF0ZS1zZWNyZXQ=", "messageSecret", "mediaKey", "encPayload", "directPath"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("secret leaked: %s", encoded)
				}
			}
		})
	}
}
