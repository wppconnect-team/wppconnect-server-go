package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestAdvancedMessageWebhooks(t *testing.T) {
	received := make(chan map[string]any, 8)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		received <- payload
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	d := New(receiver.URL)
	defer d.Close(context.Background())
	for _, tc := range []struct {
		kind string
		msg  *waE2E.Message
	}{
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("film"), MediaKey: []byte("private")}}},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{IsAnimated: proto.Bool(true), MediaKey: []byte("private")}}},
		{"vcard", &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String("Ana")}}},
		{"location", &waE2E.Message{LocationMessage: &waE2E.LocationMessage{DegreesLatitude: proto.Float64(0)}}},
		{"poll_creation", (&whatsmeow.Client{}).BuildPollCreation("Q", []string{"A", "B"}, 1)},
	} {
		d.OnMessage("alpha", &events.Message{Info: types.MessageInfo{ID: "incoming-" + tc.kind}, Message: tc.msg})
		select {
		case got := <-received:
			if got["type"] != tc.kind || got["event"] != "onmessage" || got["session"] != "alpha" || got["id"] != "incoming-"+tc.kind {
				t.Fatalf("webhook: %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing advanced webhook")
		}
	}
}

func TestIncomingMediaWebhook(t *testing.T) {
	received := make(chan map[string]any, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	New(receiver.URL).OnMessage("alpha", &events.Message{
		Info:    types.MessageInfo{ID: "incoming-id", MessageSource: types.MessageSource{Sender: types.NewJID("123", types.DefaultUserServer)}},
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("photo caption"), Mimetype: proto.String("image/png"), MediaKey: []byte("private-key")}},
	})
	select {
	case payload := <-received:
		if payload["event"] != "onmessage" || payload["session"] != "alpha" || payload["type"] != "image" || payload["body"] != "photo caption" || payload["id"] != "incoming-id" {
			t.Fatalf("unexpected webhook: %+v", payload)
		}
		if _, exists := payload["mediaKey"]; exists {
			t.Fatal("key leaked to webhook")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook was not delivered")
	}
}

func TestGroupWebhooks(t *testing.T) {
	received := make(chan map[string]any, 3)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		received <- payload
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	d := New(receiver.URL)
	jid := types.NewJID("123-456", types.GroupServer)
	sender := types.NewJID("123", types.DefaultUserServer)
	d.OnGroupEvent("alpha", &events.GroupInfo{JID: jid, Sender: &sender, Promote: []types.JID{sender}, Name: &types.GroupName{Name: "New subject"}})
	d.OnGroupEvent("beta", &events.JoinedGroup{GroupInfo: types.GroupInfo{JID: jid}})
	seen := make(map[string]map[string]any)
	for i := 0; i < 3; i++ {
		select {
		case payload := <-received:
			seen[payload["event"].(string)] = payload
		case <-time.After(5 * time.Second):
			t.Fatal("missing group webhook")
		}
	}
	if seen["onparticipantschanged"]["session"] != "alpha" || seen["onparticipantschanged"]["operation"] != "promote" || seen["ongroupchange"]["groupId"] != "123-456@g.us" || seen["onaddedtogroup"]["session"] != "beta" {
		t.Fatal(seen)
	}
}

func TestPresenceWebhooks(t *testing.T) {
	received := make(chan map[string]any, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]any
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Error(err)
			return
		}
		received <- data
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	d := New(receiver.URL)
	jid := types.NewJID("123", types.DefaultUserServer)
	d.OnDirectoryEvent("alpha", &events.Presence{From: jid, Unavailable: true})
	d.OnDirectoryEvent("beta", &events.ChatPresence{MessageSource: types.MessageSource{Chat: types.NewJID("456", types.GroupServer), Sender: jid}, State: types.ChatPresenceComposing, Media: types.ChatPresenceMediaAudio})
	for i := 0; i < 2; i++ {
		select {
		case data := <-received:
			if data["event"] != "onpresencechanged" {
				t.Fatal(data)
			}
			if data["session"] == "alpha" {
				if data["isOnline"] != false || data["lastSeen"] != nil {
					t.Fatal(data)
				}
			} else {
				if data["state"] != "recording" || data["isOnline"] != nil || data["isGroup"] != true {
					t.Fatal(data)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing presence webhook")
		}
	}
}
