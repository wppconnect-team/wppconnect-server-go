package vault

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

type memoryObjects struct {
	data map[string][]byte
	fail bool
	gets int
}

func (m *memoryObjects) Put(ctx context.Context, key string, data []byte, mime string) error {
	if m.fail {
		return errors.New("offline")
	}
	m.data[key] = bytes.Clone(data)
	return nil
}
func (m *memoryObjects) Get(ctx context.Context, key string) ([]byte, error) {
	m.gets++
	data, ok := m.data[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return bytes.Clone(data), nil
}
func (m *memoryObjects) Delete(ctx context.Context, key string) error {
	if m.fail {
		return errors.New("offline")
	}
	delete(m.data, key)
	return nil
}
func historyStore(t *testing.T) *history.Store {
	t.Helper()
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"), history.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
func TestVaultOwnershipIntegrityAndRetention(t *testing.T) {
	store := historyStore(t)
	objects := &memoryObjects{data: map[string][]byte{}}
	v := &Vault{Objects: objects, Retention: time.Hour}
	ctx := context.Background()
	chat := "123@s.whatsapp.net"
	meta, err := v.Archive(ctx, store, "alpha", chat, "message", "text/plain", []byte("private bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = v.Download(ctx, store, "beta", chat, "message"); err == nil || objects.gets != 0 {
		t.Fatal("cross-session storage read")
	}
	_, data, err := v.Download(ctx, store, "alpha", chat, "message")
	if err != nil || string(data) != "private bytes" {
		t.Fatal("download", err)
	}
	objects.data[meta.Key] = []byte("tampered")
	if _, data, err = v.Download(ctx, store, "alpha", chat, "message"); err == nil || data != nil {
		t.Fatal("corrupted bytes returned")
	}
	meta.Expires = time.Now().Add(-time.Second).UnixMilli()
	if err = store.FinishMedia(ctx, meta); err != nil {
		t.Fatal(err)
	}
	if err = v.Cleanup(ctx, store, "alpha"); err != nil {
		t.Fatal(err)
	}
	if len(objects.data) != 0 {
		t.Fatal("retention did not remove object")
	}
	if _, err = store.Media(ctx, chat, "message"); !errors.Is(err, history.ErrNotFound) {
		t.Fatal("retained metadata after cleanup")
	}
}
func TestFailedUploadKeepsBoundedCleanupReservation(t *testing.T) {
	store := historyStore(t)
	objects := &memoryObjects{data: map[string][]byte{}, fail: true}
	v := &Vault{Objects: objects, Retention: 24 * time.Hour}
	ctx := context.Background()
	if _, err := v.Archive(ctx, store, "alpha", "123@s.whatsapp.net", "failed", "text/plain", []byte("bytes")); err == nil {
		t.Fatal("upload failure hidden")
	}
	list, err := store.ExpiredMedia(ctx, time.Now().Add(2*time.Hour).UnixMilli())
	if err != nil || len(list) != 1 || list[0].Ready {
		t.Fatal("failed reservation missing", err)
	}
	objects.fail = false
	if err = v.Delete(ctx, store, "alpha", list[0]); err != nil {
		t.Fatal(err)
	}
}

func TestStaleDeletionCannotRemoveRearchivedObject(t *testing.T) {
	ctx := context.Background()
	store := historyStore(t)
	objects := &memoryObjects{data: map[string][]byte{}}
	v := &Vault{Objects: objects, Retention: time.Hour}
	old, err := v.Archive(ctx, store, "alpha", "123@s.whatsapp.net", "id", "text/plain", []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Delete(ctx, store, "alpha", old); err != nil {
		t.Fatal(err)
	}
	current, err := v.Archive(ctx, store, "alpha", old.Chat, old.ID, "text/plain", []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Key == current.Key {
		t.Fatal("object generation reused")
	}
	if err = v.Delete(ctx, store, "alpha", old); err != nil {
		t.Fatal(err)
	}
	_, data, err := v.Download(ctx, store, "alpha", old.Chat, old.ID)
	if err != nil || string(data) != "new" {
		t.Fatalf("stale cleanup removed new archive: %q %v", data, err)
	}
}
