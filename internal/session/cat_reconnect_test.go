package session

import (
	"testing"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types/events"
)

func TestExpiredCATWithoutRefreshStopsRestoreAndRetainsPairing(t *testing.T) {
	ctx := t.Context()
	manager, runtimes := managerForTest(t, t.TempDir())
	seedDevice(t, manager, "cat")
	handle, err := manager.Start(ctx, "cat")
	if err != nil {
		t.Fatal(err)
	}
	// Use the real SDK parser and the manager's handler on an offline runtime.
	handle.Client.AddEventHandler(runtimes["cat"].emit)
	handle.Client.DangerousInternals().HandleConnectFailure(ctx, &waBinary.Node{
		Tag: "failure", Attrs: waBinary.Attrs{"reason": events.ConnectFailureCATExpired.NumberString()},
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, running := manager.Get("cat"); !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session stayed active after a terminal CAT error")
		}
		time.Sleep(5 * time.Millisecond)
	}
	record, err := manager.readRecord("cat")
	if err != nil || record.Restore || record.Revoked || handle.GetStatus() != StatusClosed {
		t.Fatalf("closed session: restore=%v revoked=%v status=%s err=%v", record.Restore, record.Revoked, handle.GetStatus(), err)
	}
	reopened, err := manager.Start(ctx, "cat")
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.paired {
		t.Fatal("explicit restart lost the paired device")
	}
}
