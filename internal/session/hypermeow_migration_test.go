package session

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waAdv"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
)

//go:embed testdata/whatsmeow-v16.sql
var whatsmeowV16Schema string

func TestHyperMeowUpgradesPreviousRuntimeDatabaseWithoutLosingCredentials(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "existing.db")
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, whatsmeowV16Schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "CREATE TABLE whatsmeow_version (version INTEGER, compat INTEGER); INSERT INTO whatsmeow_version VALUES (16,8)"); err != nil {
		t.Fatal(err)
	}
	container := sqlstore.NewWithDB(db, "sqlite", nil)
	device := container.NewDevice()
	id := types.NewJID("15550001111", types.DefaultUserServer)
	device.ID = &id
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err = device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO whatsmeow_chat_settings (our_jid,chat_jid,wasa_root_secret_id) VALUES (?,?,?)", id.String(), "15550002222@s.whatsapp.net", "existing-root"); err != nil {
		t.Fatal(err)
	}
	if err = container.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := container.GetDevice(ctx, id)
	if err != nil || loaded == nil {
		t.Fatalf("lost device: %v", err)
	}
	if !bytes.Equal(loaded.IdentityKey.Priv[:], device.IdentityKey.Priv[:]) || !bytes.Equal(loaded.NoiseKey.Priv[:], device.NoiseKey.Priv[:]) || !bytes.Equal(loaded.AdvSecretKey, device.AdvSecretKey) {
		t.Fatal("credentials changed during migration")
	}
	var version int
	if err = db.QueryRowContext(ctx, "SELECT version FROM whatsmeow_version").Scan(&version); err != nil || version != 19 {
		t.Fatalf("version=%d, error=%v", version, err)
	}
	var secret string
	if err = db.QueryRowContext(ctx, "SELECT wasa_root_secret_id FROM whatsmeow_chat_settings").Scan(&secret); err != nil || secret != "existing-root" {
		t.Fatalf("root secret was not retained: %v", err)
	}
	var exists bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='index' AND name='whatsmeow_sender_keys_sender_idx')").Scan(&exists); err != nil || !exists {
		t.Fatalf("fork migration index missing: %v", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE whatsmeow_contacts SET username='test'"); err != nil {
		t.Fatal(err)
	}
	if err = container.Upgrade(ctx); err != nil {
		t.Fatalf("repeat upgrade failed: %v", err)
	}
}
