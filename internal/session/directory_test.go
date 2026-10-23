package session

import (
	"path/filepath"
	"testing"

	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type directorySink struct {
	state  *directory.State
	t      *testing.T
	name   string
	called bool
}

func (*directorySink) OnQR(string, string)               {}
func (*directorySink) OnConnected(string)                {}
func (*directorySink) OnMessage(string, *events.Message) {}
func (s *directorySink) OnDirectoryEvent(name string, evt any) {
	s.called = true
	s.name = name
	if p, ok := evt.(*events.Presence); ok {
		if _, known := s.state.Presence.Get(p.From); !known {
			s.t.Fatal("presence webhook preceded the cache update")
		}
	}
}
func TestPresenceIndexedBeforeDirectoryWebhook(t *testing.T) {
	index, err := directory.Open(filepath.Join(t.TempDir(), "directory.db"))
	if err != nil {
		t.Fatal(err)
	}
	state := directory.NewState(index)
	defer state.Close()
	sink := &directorySink{state: state, t: t}
	mgr := &Manager{sink: sink}
	h := &Handle{Name: "alpha", Directory: state}
	mgr.forwardDirectoryEvent(h, &events.Presence{From: types.NewJID("123", types.DefaultUserServer)})
	if !sink.called || sink.name != "alpha" {
		t.Fatal("event lost its session")
	}
}
