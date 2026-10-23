package directory

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestPresenceExpiryAndUnknownLastSeen(t *testing.T) {
	s := NewPresenceCache()
	now := time.Now()
	s.now = func() time.Time { return now }
	jid := types.NewJID("123", types.DefaultUserServer)
	if _, known := s.Get(jid); known {
		t.Fatal("unknown contact considered online/offline")
	}
	s.Observe(&events.Presence{From: jid})
	p, known := s.Get(jid)
	if !known || !p.Online || !p.LastSeen.IsZero() {
		t.Fatal(p)
	}
	now = now.Add(PresenceTTL)
	if _, known = s.Get(jid); known {
		t.Fatal("stale online presence returned")
	}
	s.Observe(&events.Presence{From: jid, Unavailable: true, LastSeen: time.Unix(100, 0)})
	p, known = s.Get(jid)
	if !known || p.Online || p.LastSeen.Unix() != 100 {
		t.Fatal(p)
	}
	s.Clear()
	if _, known = s.Get(jid); known {
		t.Fatal("presence retained after disconnect")
	}
	for _, evt := range []any{&events.Presence{From: jid}, &events.ChatPresence{MessageSource: types.MessageSource{Chat: types.NewJID("456", types.GroupServer), Sender: jid}, State: types.ChatPresenceComposing, Media: types.ChatPresenceMediaAudio}} {
		name, payload := Notification(evt)
		if name != "onpresencechanged" || payload["id"] == "" {
			t.Fatal(payload)
		}
	}
}

type signalClient struct {
	SDK
	mu      sync.Mutex
	calls   []types.ChatPresence
	started chan struct{}
}

func (s *signalClient) SendChatPresence(_ context.Context, _ types.JID, state types.ChatPresence, _ types.ChatPresenceMedia) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, state)
	select {
	case s.started <- struct{}{}:
	default:
	}
	return nil
}

func TestRecordingTimeoutDoesNotPauseNewerCommand(t *testing.T) {
	s := NewState(testIndex(t))
	client := &signalClient{started: make(chan struct{}, 4)}
	jid := types.NewJID("123", types.DefaultUserServer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- s.Signal(ctx, client, jid, types.ChatPresenceComposing, types.ChatPresenceMediaAudio, time.Minute)
	}()
	<-client.started
	if err := s.Signal(context.Background(), client, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText, 0); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 {
		t.Fatalf("old recording paused newer typing: %v", client.calls)
	}
}

func TestRecordingCancellationSendsPause(t *testing.T) {
	s := NewState(testIndex(t))
	client := &signalClient{started: make(chan struct{}, 4)}
	jid := types.NewJID("123", types.DefaultUserServer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Signal(ctx, client, jid, types.ChatPresenceComposing, types.ChatPresenceMediaAudio, time.Second)
	}()
	<-client.started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 || client.calls[1] != types.ChatPresencePaused {
		t.Fatal(client.calls)
	}
}
