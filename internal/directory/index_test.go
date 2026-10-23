package directory

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func testIndex(t *testing.T) *Index {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "chats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func conversation(id string, ts uint64, unread uint32) *waHistorySync.Conversation {
	return &waHistorySync.Conversation{ID: proto.String(id), Name: proto.String("Team"), LastMsgTimestamp: proto.Uint64(ts), UnreadCount: proto.Uint32(unread), Archived: proto.Bool(false), Pinned: proto.Uint32(0)}
}
func eventMessage(id string, ts int64, own bool) *events.Message {
	return &events.Message{Info: types.MessageInfo{ID: id, Timestamp: time.Unix(ts, 0), MessageSource: types.MessageSource{Chat: types.NewJID("123", types.DefaultUserServer), IsFromMe: own}}}
}
func TestHistoryLiveReadAndOutOfOrderEvents(t *testing.T) {
	s := testIndex(t)
	ctx := context.Background()
	jid := types.NewJID("123", types.DefaultUserServer)
	if err := s.Event(ctx, &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{conversation(jid.String(), 100, 4)}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Message(ctx, eventMessage("new", 110, false)); err != nil {
		t.Fatal(err)
	}
	_ = s.Message(ctx, eventMessage("new", 110, false))
	_ = s.Message(ctx, eventMessage("outgoing", 120, true))
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].Unread != 5 || list[0].T != 120 || !list[0].UnreadKnown {
		t.Fatalf("live index: %+v %v", list, err)
	}
	_ = s.Event(ctx, &events.MarkChatAsRead{JID: jid, Timestamp: time.Unix(130, 0), Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(true)}})
	_ = s.Event(ctx, &events.Pin{JID: jid, Timestamp: time.Unix(140, 0), Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}})
	_ = s.Event(ctx, &events.Archive{JID: jid, Timestamp: time.Unix(135, 0), Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}})
	_ = s.Event(ctx, &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{conversation(jid.String(), 120, 9)}}})
	list, _ = s.List(ctx)
	if list[0].Unread != 0 || !list[0].Pinned || !list[0].Archived {
		t.Fatalf("old history overwrote newer read/settings: %+v", list)
	}
	_ = s.Event(ctx, &events.DeleteChat{JID: jid, Timestamp: time.Unix(150, 0)})
	list, _ = s.List(ctx)
	if len(list) != 0 {
		t.Fatal("deleted chat still visible")
	}
	_ = s.Event(ctx, &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{conversation(jid.String(), 120, 9)}}})
	list, _ = s.List(ctx)
	if len(list) != 0 {
		t.Fatal("old history revived deleted chat")
	}
	_ = s.Message(ctx, eventMessage("new-after-delete", 160, false))
	list, _ = s.List(ctx)
	if len(list) != 1 {
		t.Fatal("new message did not recreate chat")
	}
}

func TestPersistentIndexIsolationAndBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Message(context.Background(), eventMessage("id", 100, false)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Unread != 1 || list[0].UnreadKnown {
		t.Fatalf("reopened: %+v %v", list, err)
	}
	other := testIndex(t)
	empty, _ := other.List(context.Background())
	if len(empty) != 0 {
		t.Fatal("chat metadata leaked to another session")
	}
	convs := make([]*waHistorySync.Conversation, 0, MaxChats+1)
	for i := 0; i <= MaxChats; i++ {
		convs = append(convs, conversation(fmt.Sprintf("%d@g.us", i+1), uint64(i+1), 0))
	}
	if err = s.Event(context.Background(), &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: convs}}); err != nil {
		t.Fatal(err)
	}
	list, err = s.List(context.Background())
	if err != nil || len(list) != MaxChats || list[0].T != MaxChats+1 {
		t.Fatalf("index bound/order: %d %v", len(list), err)
	}
}

func TestConcurrentIndexUpdates(t *testing.T) {
	s := testIndex(t)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if err := s.Message(context.Background(), eventMessage(fmt.Sprintf("%d-%d", worker, i), int64(100+i), false)); err != nil {
					t.Error(err)
				}
			}
		}(worker)
	}
	wg.Wait()
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Unread != 40 {
		t.Fatalf("lost concurrent updates: %+v %v", list, err)
	}
}

func TestHistoryConversationTimestampFallback(t *testing.T) {
	s := testIndex(t)
	conv := conversation("123@s.whatsapp.net", 0, 2)
	conv.ConversationTimestamp = proto.Uint64(200)
	if err := s.Event(context.Background(), &events.HistorySync{Data: &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{conv}}}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].T != 200 {
		t.Fatalf("conversation timestamp not indexed: %+v %v", list, err)
	}
}
