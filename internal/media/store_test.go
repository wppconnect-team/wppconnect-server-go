package media

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func imageMessage() *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/media"), Mimetype: proto.String("image/png"),
		MediaKey: []byte("key"), FileLength: proto.Uint64(12), JPEGThumbnail: make([]byte, 1024),
	}}
}

func TestStoreSnapshotsAndEviction(t *testing.T) {
	s := NewStore()
	msg := imageMessage()
	if !s.Remember("id", msg) {
		t.Fatal("image not indexed")
	}
	msg.ImageMessage.MediaKey[0] = 'X'
	a, ok := s.Get("id")
	if !ok || string(a.Message.GetMediaKey()) != "key" {
		t.Fatal("cache aliases incoming bytes")
	}
	if len(a.Message.(*waE2E.ImageMessage).GetJPEGThumbnail()) != 0 {
		t.Fatal("cache retains thumbnails")
	}
	for i := 0; i < CacheEntries; i++ {
		s.Remember(fmt.Sprint(i), msg)
	}
	if _, ok := s.Get("id"); ok {
		t.Fatal("oldest descriptor not evicted")
	}
	if _, ok := s.Get(fmt.Sprint(CacheEntries - 1)); !ok {
		t.Fatal("new descriptor lost")
	}
	if s.Remember("text", &waE2E.Message{Conversation: proto.String("hello")}) {
		t.Fatal("text indexed as attachment")
	}
}

func TestStoreExpiryAndSessionIsolation(t *testing.T) {
	now := time.Now()
	s := NewStore()
	s.now = func() time.Time { return now }
	s.Remember("same-id", imageMessage())
	other := NewStore()
	if _, ok := other.Get("same-id"); ok {
		t.Fatal("descriptor leaked between stores")
	}
	now = now.Add(CacheTTL)
	if _, ok := s.Get("same-id"); ok {
		t.Fatal("expired descriptor returned")
	}
	s.Remember("same-id", imageMessage())
	if _, ok := s.Get("same-id"); !ok {
		t.Fatal("refreshed descriptor missing")
	}
}

func TestStoreConcurrentReadersAndWriters(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			msg := imageMessage()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("%d-%d", worker, i)
				s.Remember(id, msg)
				s.Get(id)
			}
		}(worker)
	}
	wg.Wait()
	if s.order.Len() > CacheEntries {
		t.Fatal("cache exceeded capacity")
	}
}

func TestStoreRejectsArbitraryURLsAndOversizedDescriptors(t *testing.T) {
	msg := imageMessage()
	msg.ImageMessage.DirectPath = proto.String("@127.0.0.1/secret")
	if NewStore().Remember("id", msg) {
		t.Fatal("direct path could override the SDK host")
	}
	for _, value := range []string{"http://127.0.0.1/secret", "https://whatsapp.net.evil.test/media", "https://user@mmg.whatsapp.net/media", "https://mmg.whatsapp.net:444/media"} {
		msg := imageMessage()
		msg.ImageMessage.DirectPath = nil
		msg.ImageMessage.URL = &value
		if NewStore().Remember("id", msg) {
			t.Fatalf("untrusted URL cached: %s", value)
		}
	}
	msg = imageMessage()
	msg.ImageMessage.URL = proto.String("http://127.0.0.1/secret")
	s := NewStore()
	if !s.Remember("id", msg) {
		t.Fatal("safe direct path rejected")
	}
	a, _ := s.Get("id")
	if a.Message.(*waE2E.ImageMessage).GetURL() != "" {
		t.Fatal("URL should be dropped when direct path exists")
	}
	msg.ImageMessage.MediaKey = make([]byte, 65)
	if s.Remember("bad", msg) {
		t.Fatal("unbounded descriptor cached")
	}
}

func TestEventContentAndWrappers(t *testing.T) {
	for _, tc := range []struct {
		name, kind, body, mime string
		message                *waE2E.Message
	}{
		{"text", "chat", "plain", "", &waE2E.Message{Conversation: proto.String("plain")}},
		{"extended", "chat", "extended", "", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("extended")}}},
		{"image", "image", "photo", "image/png", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("photo"), Mimetype: proto.String("image/png")}}},
		{"document", "document", "report", "application/pdf", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("report"), Mimetype: proto.String("application/pdf"), FileName: proto.String("../report.pdf")}}},
		{"audio", "audio", "", "audio/mpeg", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/mpeg"), Seconds: proto.Uint32(3)}}},
		{"voice", "ptt", "", "audio/ogg", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg"), PTT: proto.Bool(true)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: tc.message}}}}
			evt := &events.Message{Info: types.MessageInfo{ID: "id", MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), Sender: types.NewJID("456", types.DefaultUserServer), IsGroup: true}}, Message: wrapped}
			payload := Event(evt)
			if payload["type"] != tc.kind || payload["body"] != tc.body || payload["from"] != "123@g.us" || payload["author"] != "456@s.whatsapp.net" || payload["id"] != "id" {
				t.Fatalf("unexpected event: %+v", payload)
			}
			if tc.mime != "" && (payload["mimetype"] != tc.mime || payload["isMedia"] != true) {
				t.Fatalf("missing media fields: %+v", payload)
			}
			if evt.Message != wrapped {
				t.Fatal("normalization modified shared event")
			}
			for _, field := range []string{"url", "directPath", "mediaKey", "fileEncSHA256"} {
				if _, exists := payload[field]; exists {
					t.Fatal("encryption metadata leaked")
				}
			}
		})
	}
	if Event(nil) != nil {
		t.Fatal("nil event should be ignored")
	}
	if got := Event(&events.Message{}); got["body"] != "" {
		t.Fatal("empty event panicked or changed body")
	}
}
