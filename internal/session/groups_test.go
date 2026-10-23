package session

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type groupSink struct {
	names  []string
	events []any
}

func (*groupSink) OnQR(string, string)               {}
func (*groupSink) OnConnected(string)                {}
func (*groupSink) OnMessage(string, *events.Message) {}
func (s *groupSink) OnGroupEvent(name string, evt any) {
	s.names = append(s.names, name)
	s.events = append(s.events, evt)
}

func TestGroupSinkAndLegacySink(t *testing.T) {
	sink := &groupSink{}
	mgr := &Manager{sink: sink}
	evt := &events.GroupInfo{JID: types.NewJID("123", types.GroupServer)}
	mgr.forwardGroupEvent("alpha", evt)
	mgr.forwardGroupEvent("beta", &events.JoinedGroup{})
	mgr.forwardGroupEvent("alpha", &events.Picture{JID: types.NewJID("123", types.DefaultUserServer)})
	if len(sink.events) != 2 || sink.events[0] != evt || sink.names[0] != "alpha" || sink.names[1] != "beta" {
		t.Fatal("group event/session dispatch mismatch")
	}
	mgr.sink = nil
	mgr.forwardGroupEvent("alpha", evt)
	mgr.sink = &checkingSink{}
	mgr.forwardGroupEvent("alpha", evt)
}
