package directory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const PresenceTTL = 5 * time.Minute

type Presence struct {
	Online   bool
	LastSeen time.Time
	Observed time.Time
}
type PresenceCache struct {
	mu      sync.Mutex
	entries map[types.JID]Presence
	now     func() time.Time
}

func NewPresenceCache() *PresenceCache {
	return &PresenceCache{entries: make(map[types.JID]Presence), now: time.Now}
}
func (s *PresenceCache) Observe(evt *events.Presence) {
	if evt == nil || !Supported(evt.From) || evt.From.Server == types.GroupServer {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= MaxChats {
		for id, p := range s.entries {
			if s.now().Sub(p.Observed) >= PresenceTTL {
				delete(s.entries, id)
			}
		}
		if len(s.entries) >= MaxChats {
			for id := range s.entries {
				delete(s.entries, id)
				break
			}
		}
	}
	s.entries[evt.From.ToNonAD()] = Presence{Online: !evt.Unavailable, LastSeen: evt.LastSeen, Observed: s.now()}
}
func (s *PresenceCache) Get(jid types.JID) (Presence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.entries[jid.ToNonAD()]
	if !ok {
		return Presence{}, false
	}
	if s.now().Sub(p.Observed) >= PresenceTTL {
		delete(s.entries, jid.ToNonAD())
		return Presence{}, false
	}
	return p, true
}
func (s *PresenceCache) Clear() { s.mu.Lock(); defer s.mu.Unlock(); clear(s.entries) }

// A delayed pause must not overwrite a more recent typing/recording command.
func (s *State) Signal(ctx context.Context, client Client, jid types.JID, presence types.ChatPresence, medium types.ChatPresenceMedia, duration time.Duration) error {
	s.commands.Lock()
	if s.closed {
		s.commands.Unlock()
		return errors.New("session directory is closed")
	}
	if err := ctx.Err(); err != nil {
		s.commands.Unlock()
		return err
	}
	if err := client.SendChatPresence(ctx, jid, presence, medium); err != nil {
		s.commands.Unlock()
		return err
	}
	s.generation++
	generation := s.generation
	if len(s.owners) >= MaxChats {
		for id := range s.owners {
			delete(s.owners, id)
			break
		}
	}
	s.owners[jid] = generation
	s.commands.Unlock()
	if duration == 0 || presence == types.ChatPresencePaused {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	var waitErr error
	select {
	case <-timer.C:
	case <-ctx.Done():
		waitErr = ctx.Err()
	}
	s.commands.Lock()
	defer s.commands.Unlock()
	if s.closed || s.owners[jid] != generation {
		return waitErr
	}
	delete(s.owners, jid)
	pauseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.SendChatPresence(pauseCtx, jid, types.ChatPresencePaused, types.ChatPresenceMediaText)
	if waitErr != nil {
		return waitErr
	}
	return err
}

func Notification(evt any) (string, map[string]any) {
	var jid types.JID
	state := ""
	var online any
	data := make(map[string]any)
	switch v := evt.(type) {
	case *events.Presence:
		if v == nil || !Supported(v.From) || v.From.Server == types.GroupServer {
			return "", nil
		}
		jid = v.From
		online = !v.Unavailable
		state = "available"
		if v.Unavailable {
			state = "unavailable"
		}
		data["lastSeen"] = groups.Timestamp(v.LastSeen)
	case *events.ChatPresence:
		if v == nil || !Supported(v.Chat) {
			return "", nil
		}
		jid = v.Chat
		state = "paused"
		if v.State == types.ChatPresenceComposing {
			state = "composing"
			if v.Media == types.ChatPresenceMediaAudio {
				state = "recording"
			}
		}
		data["author"] = groups.ID(v.Sender)
		if v.Chat.Server == types.GroupServer && !v.Sender.IsEmpty() {
			data["participants"] = []map[string]any{{"id": groups.ID(v.Sender), "state": state, "shortName": ""}}
		}
	default:
		return "", nil
	}
	data["id"], data["isOnline"], data["state"] = groups.ID(jid), online, state
	data["isGroup"], data["isUser"], data["t"] = jid.Server == types.GroupServer, jid.Server != types.GroupServer, time.Now().UnixMilli()
	return "onpresencechanged", data
}
