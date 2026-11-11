package history

import (
	"context"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"testing"
	"time"
)

func TestObservedStarSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := Open(path, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	chat := types.NewJID("12345", types.GroupServer)
	sender := types.NewJID("111", types.DefaultUserServer)
	now := time.Now()
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: true}, Timestamp: now, ID: "observed"}
	if err = store.Put(ctx, info, &waE2E.Message{Conversation: proto.String("fixture")}); err != nil {
		t.Fatal(err)
	}
	check := func(want *bool) {
		t.Helper()
		r, err := store.Get(ctx, chat.String(), info.ID)
		if err != nil || (r.Starred == nil) != (want == nil) || want != nil && *want != *r.Starred {
			t.Fatalf("wrong state for %v: %v", want, err)
		}
	}
	apply := func(from types.JID, at time.Time, value bool) {
		t.Helper()
		if err := store.ObserveStar(ctx, &events.Star{ChatJID: chat, MessageID: info.ID, SenderJID: from, Timestamp: at, Action: &waSyncAction.StarAction{Starred: proto.Bool(value)}}); err != nil {
			t.Fatal(err)
		}
	}
	apply(types.EmptyJID, now, true)
	apply(types.NewJID("222", types.DefaultUserServer), now, true)
	check(nil)
	if err = store.SnapshotStar(ctx, info, false); err != nil {
		t.Fatal(err)
	}
	check(proto.Bool(false))
	apply(sender, now, true)
	apply(sender, now.Add(-time.Hour), false)
	check(proto.Bool(true))
	if err = store.SnapshotStar(ctx, info, false); err != nil {
		t.Fatal(err)
	}
	check(proto.Bool(true))
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	check(proto.Bool(true))
	apply(sender, now.Add(time.Second), false)
	check(proto.Bool(false))
}
