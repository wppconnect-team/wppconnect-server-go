package session

import (
	"strings"
	"testing"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
	waLog "github.com/polymorfa/hypermeow/util/log"
)

type rerequestLogger struct {
	waLog.Logger
	attempted bool
}

func (l *rerequestLogger) Warnf(format string, args ...any) {
	if strings.HasPrefix(format, "Failed to send request for unavailable message") {
		l.attempted = true
	}
	l.Logger.Warnf(format, args...)
}

func TestUnavailableMessagesRespectPhoneRerequestPolicy(t *testing.T) {
	manager, _ := managerForTest(t, t.TempDir())
	seedDevice(t, manager, "unavailable")
	handle, err := manager.Start(t.Context(), "unavailable")
	if err != nil {
		t.Fatal(err)
	}
	client := handle.Client
	client.SynchronousAck = true
	logger := &rerequestLogger{Logger: client.Log}
	client.Log = logger
	for _, tc := range []struct {
		name        string
		enable      bool
		kind        string
		wantAttempt bool
	}{
		{name: "default off", kind: "ordinary"},
		{name: "view once with rerequests enabled", enable: true, kind: "view_once"},
		{name: "ordinary with rerequests enabled", enable: true, kind: "ordinary", wantAttempt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger.attempted = false
			client.AutomaticMessageRerequestFromPhone = tc.enable
			node := &waBinary.Node{
				Tag:     "message",
				Attrs:   waBinary.Attrs{"id": "fixture", "t": "1", "from": types.NewJID("456", types.DefaultUserServer)},
				Content: []waBinary.Node{{Tag: "unavailable", Attrs: waBinary.Attrs{"type": tc.kind}}},
			}
			// The test client's socket is unconnected. An attempted phone request
			// therefore reports a send failure without reaching the network.
			if err := client.InjectNode(t.Context(), node); err != nil {
				t.Fatal(err)
			}
			if logger.attempted != tc.wantAttempt {
				t.Fatalf("attempted phone request: got %t, want %t", logger.attempted, tc.wantAttempt)
			}
		})
	}
}
