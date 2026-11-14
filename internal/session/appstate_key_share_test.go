package session

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/polymorfa/hypermeow/proto/waE2E"
)

func TestAppStateKeySharePreservesExistingKey(t *testing.T) {
	manager, _ := managerForTest(t, t.TempDir())
	seedDevice(t, manager, "sync")
	handle, err := manager.Start(t.Context(), "sync")
	if err != nil {
		t.Fatal(err)
	}
	id := []byte("test-key-id")
	first := bytes.Repeat([]byte{1}, 32)
	conflicting := bytes.Repeat([]byte{2}, 32)
	share := func(data []byte, timestamp int64) {
		handle.Client.DangerousInternals().HandleAppStateSyncKeyShare(t.Context(), &waE2E.AppStateSyncKeyShare{
			Keys: []*waE2E.AppStateSyncKey{{
				KeyID:   &waE2E.AppStateSyncKeyId{KeyID: id},
				KeyData: &waE2E.AppStateSyncKeyData{KeyData: data, Timestamp: proto.Int64(timestamp), Fingerprint: &waE2E.AppStateSyncKeyFingerprint{RawID: proto.Uint32(1)}},
			}},
		})
	}
	share(first, 1)
	seed, err := handle.Client.Store.AppStateKeys.GetAppStateSyncKey(t.Context(), id)
	if err != nil || seed == nil || !bytes.Equal(seed.Data, first) || seed.Timestamp != 1 {
		t.Fatalf("initial share failed: %v", err)
	}
	share(conflicting, 2)
	stored, err := handle.Client.Store.AppStateKeys.GetAppStateSyncKey(t.Context(), id)
	if err != nil || stored == nil || !bytes.Equal(stored.Data, first) || stored.Timestamp != 1 {
		t.Fatalf("conflicting share changed stored key or timestamp: %v", err)
	}
	share(first, 3)
	stored, err = handle.Client.Store.AppStateKeys.GetAppStateSyncKey(t.Context(), id)
	if err != nil || stored == nil || !bytes.Equal(stored.Data, first) || stored.Timestamp != 3 {
		t.Fatalf("matching share failed to refresh timestamp: %v", err)
	}
}
