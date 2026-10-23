package history

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func openTest(t *testing.T, options Options) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "history.db"), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func info(id string) types.MessageInfo {
	return types.MessageInfo{ID: id, Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: types.NewJID("123", types.DefaultUserServer), Sender: types.NewJID("456", types.DefaultUserServer)}}
}

func TestHistoryPersistencePaginationAndIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err = s.Put(ctx, info(id), &waE2E.Message{Conversation: proto.String(id), MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: []byte("private-secret")}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Put(ctx, info("a"), &waE2E.Message{Conversation: proto.String("overwrite")}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page, err := s.List(ctx, Query{Limit: 2})
	if err != nil || len(page.Messages) != 2 || page.Messages[0].ID != "c" || page.NextCursor == 0 {
		t.Fatalf("page %+v %v", page, err)
	}
	second, err := s.List(ctx, Query{Limit: 2, Before: page.NextCursor})
	if err != nil || len(second.Messages) != 1 || second.Messages[0].ID != "a" || second.Messages[0].Content["body"] != "a" {
		t.Fatalf("page2 %+v %v", second, err)
	}
	b, _ := json.Marshal(page)
	if strings.Contains(string(b), "private-secret") || strings.Contains(string(b), "messageSecret") {
		t.Fatal("protocol exposed")
	}
	other := openTest(t, DefaultOptions())
	if _, err = other.Get(ctx, info("a").Chat.String(), "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-session data")
	}
	if err = s.MarkRead(ctx, info("a").Chat.String(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	unread, err := s.List(ctx, Query{Unread: true})
	if err != nil || len(unread.Messages) != 2 {
		t.Fatal("read persistence", err)
	}
}

func TestHistoryRetentionQuotaAndChanges(t *testing.T) {
	ctx := context.Background()
	options := DefaultOptions()
	options.MaxMessages = 2
	s := openTest(t, options)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Put(ctx, info(id), &waE2E.Message{Conversation: proto.String(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Get(ctx, info("a").Chat.String(), "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("quota did not evict oldest")
	}
	if err := s.Change(ctx, info("c").Chat.String(), "c", &waE2E.Message{Conversation: proto.String("edit")}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, info("c").Chat.String(), "c")
	if err != nil || !r.Edited || r.Content["body"] != "edit" {
		t.Fatal("edit", err)
	}
	if err = s.Change(ctx, r.Chat, r.ID, nil); err != nil {
		t.Fatal(err)
	}
	r, err = s.Get(ctx, r.Chat, r.ID)
	if err != nil || !r.Deleted || r.Message.GetConversation() != "" {
		t.Fatal("revoke", err)
	}
	s.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	if err = s.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, Query{})
	if err != nil || len(page.Messages) != 0 {
		t.Fatal("retention", err)
	}
}

func TestHistoryLabelsReceiptsVotesAndOutOfOrder(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, DefaultOptions())
	chat := info("poll").Chat.String()
	if err := s.SetLabel(ctx, Label{ID: "1", Name: "Support", Color: 3, Updated: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel(ctx, Label{ID: "1", Name: "Old", Color: 3, Updated: 90}); err != nil {
		t.Fatal(err)
	}
	if err := s.Associate(ctx, "1", chat, "", true, 100); err != nil {
		t.Fatal(err)
	}
	if err := s.Associate(ctx, "1", chat, "", false, 90); err != nil {
		t.Fatal(err)
	}
	labels, _ := s.Labels(ctx)
	links, _ := s.ChatLabels(ctx, chat, "")
	if len(labels) != 1 || labels[0].Name != "Support" || len(links) != 1 {
		t.Fatal("stale label won")
	}
	poll := (&whatsmeow.Client{}).BuildPollCreation("Q", []string{"A", "B"}, 1)
	if err := s.Put(ctx, info("poll"), poll); err != nil {
		t.Fatal(err)
	}
	a, b := sha256.Sum256([]byte("A")), sha256.Sum256([]byte("B"))
	voter := "456@s.whatsapp.net"
	now := time.Now().UnixMilli()
	if err := s.Vote(ctx, chat, "poll", voter, now, [][]byte{a[:]}); err != nil {
		t.Fatal(err)
	}
	if err := s.Vote(ctx, chat, "poll", voter, now-10, [][]byte{b[:]}); err != nil {
		t.Fatal(err)
	}
	result, err := s.PollResults(ctx, chat, "poll")
	if err != nil || result["counts"].(map[string]int)["A"] != 1 {
		t.Fatal("vote result", err)
	}
	if err = s.Vote(ctx, chat, "poll", voter, now+10, nil); err != nil {
		t.Fatal(err)
	}
	result, _ = s.PollResults(ctx, chat, "poll")
	if result["voters"] != 0 {
		t.Fatal("vote withdrawal")
	}
	if err = s.Vote(ctx, chat, "poll", voter, now, [][]byte{[]byte("unknown")}); err == nil {
		t.Fatal("accepted unknown option")
	}
	receipt := &events.Receipt{MessageSource: info("poll").MessageSource, MessageIDs: []types.MessageID{"poll"}, Timestamp: time.Now(), Type: types.ReceiptTypeRead}
	if err = s.Receipt(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	if err = s.Receipt(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	receipts, _ := s.Receipts(ctx, chat, "poll")
	if len(receipts) != 1 {
		t.Fatal("receipt dedup")
	}
}

func TestHistoryConcurrentAndInvalidInput(t *testing.T) {
	s := openTest(t, DefaultOptions())
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Put(ctx, info("same"), &waE2E.Message{Conversation: proto.String("text")}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	page, err := s.List(ctx, Query{})
	if err != nil || len(page.Messages) != 1 {
		t.Fatal("dedup race", err)
	}
	bad := info("bad")
	bad.Chat = types.NewJID("bad", "evil")
	if err = s.Put(ctx, bad, &waE2E.Message{}); err == nil {
		t.Fatal("invalid chat accepted")
	}
	if err = s.Put(ctx, info("huge"), &waE2E.Message{Conversation: proto.String(strings.Repeat("x", 257<<10))}); !errors.Is(err, ErrTooLarge) {
		t.Fatal("large message accepted")
	}
}
