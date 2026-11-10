package vault

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

// Simulate the reply being lost after the underlying store committed the upload.
type lostUploadReply struct {
	Objects
	puts int
}

func (o *lostUploadReply) Put(ctx context.Context, key string, data []byte, mime string) error {
	o.puts++
	if err := o.Objects.Put(ctx, key, data, mime); err != nil {
		return err
	}
	return errors.New("upload reply lost")
}
func TestArchiveRecoversOriginalObjectAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	first, err := history.Open(path, history.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	memory := &memoryObjects{data: map[string][]byte{}}
	objects := &lostUploadReply{Objects: memory}
	v := &Vault{Objects: objects, Retention: time.Hour * 24}
	data := []byte("private fixture")
	const chat = "123@s.whatsapp.net"
	if _, err = v.Archive(ctx, first, "alpha", chat, "media-id", "text/plain", data); err == nil {
		t.Fatal("lost acknowledgement returned success")
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(path, history.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.ActiveMediaReservation(ctx, chat, "media-id")
	if err != nil || pending.Ready {
		t.Fatal("pending generation missing", err)
	}
	for _, input := range []struct{ session, mime, data string }{
		{"beta", "text/plain", string(data)}, {"alpha", "image/jpeg", string(data)}, {"alpha", "text/plain", "different"},
	} {
		if _, err := v.Archive(ctx, store, input.session, chat, "media-id", input.mime, []byte(input.data)); err == nil {
			t.Fatal("accepted different identity")
		}
	}
	if memory.gets != 0 {
		t.Fatal("read an unrelated object")
	}
	memory.data[pending.Key] = []byte("wrong")
	if _, err = v.Archive(ctx, store, "alpha", chat, "media-id", "text/plain", data); err == nil {
		t.Fatal("accepted corrupted data")
	}
	if _, err = store.Media(ctx, chat, "media-id"); !errors.Is(err, history.ErrNotFound) {
		t.Fatal("corrupt object made visible")
	}
	memory.data[pending.Key] = data
	confirmed, err := v.Archive(ctx, store, "alpha", chat, "media-id", "text/plain", data)
	if err != nil || !confirmed.Ready || confirmed.Key != pending.Key || confirmed.Expires != time.UnixMilli(pending.Expires).Add(v.Retention-time.Hour).UnixMilli() {
		t.Fatal("did not recover original generation", err)
	}
	again, err := v.Archive(ctx, store, "alpha", chat, "media-id", "text/plain", data)
	if err != nil || again != confirmed || objects.puts != 1 {
		t.Fatal("unexpected new upload or retention change", err)
	}
}
