package session

import (
	"testing"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

func TestStatusBroadcastReachesMessageHandler(t *testing.T) {
	manager, _ := managerForTest(t, t.TempDir())
	seedDevice(t, manager, "status")
	handle, err := manager.Start(t.Context(), "status")
	if err != nil {
		t.Fatal(err)
	}
	var unavailable *events.UndecryptableMessage
	handle.Client.AddEventHandlerWithSuccessStatus(func(event any) bool {
		if msg, ok := event.(*events.UndecryptableMessage); ok {
			unavailable = msg
			// Exercise dispatch without asking the paired device for a retry.
			return false
		}
		return true
	})
	node := &waBinary.Node{
		Tag: "status",
		Attrs: waBinary.Attrs{
			"id": "fixture", "t": "1", "from": types.StatusBroadcastJID,
			"participant": types.NewJID("456", types.DefaultUserServer),
		},
		Content: []waBinary.Node{{Tag: "unavailable", Attrs: waBinary.Attrs{"type": "view_once"}}},
	}
	if err = handle.Client.InjectNode(t.Context(), node); err != nil {
		t.Fatal(err)
	}
	if unavailable == nil || !unavailable.IsUnavailable || unavailable.Info.Chat != types.StatusBroadcastJID || unavailable.Info.StanzaTag != "status" || unavailable.Info.IsNewsletterStatus {
		t.Fatal("status broadcast was discarded before unavailable-message dispatch")
	}
}
