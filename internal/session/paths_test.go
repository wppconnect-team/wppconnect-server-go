package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordStorageRejectsUnsafeNamesAtFileBoundary(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := &Manager{dataDir: dataDir}
	outside := filepath.Join(parent, "outside.session.json")
	original := []byte(`{"name":"../outside","restore":true}`)
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "..\\outside", "/absolute", "C:\\absolute", "alpha/beta", "alpha\\beta", "..", "", "a:b", "alpha\n"} {
		t.Run(name, func(t *testing.T) {
			if _, err := m.readRecord(name); !errors.Is(err, ErrInvalidName) {
				t.Errorf("read accepted unsafe name %q: %v", name, err)
			}
			if err := m.writeRecord(sessionRecord{Name: name, Restore: false}); !errors.Is(err, ErrInvalidName) {
				t.Errorf("write accepted unsafe name %q: %v", name, err)
			}
			if _, err := m.openContainer(context.Background(), name); !errors.Is(err, ErrInvalidName) {
				t.Errorf("database accepted unsafe name %q: %v", name, err)
			}
		})
	}
	after, err := os.ReadFile(outside)
	if err != nil || string(after) != string(original) {
		t.Fatal("outside file was modified", err)
	}
}

func TestRecordReplacementDoesNotFollowDestinationSymlink(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "private.json")
	original := []byte(`{"name":"private","restore":true}`)
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dataDir, "alpha.session.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := &Manager{dataDir: dataDir}
	if err := m.writeRecord(sessionRecord{Name: "alpha", Restore: true}); err != nil {
		t.Fatal(err)
	}
	record, err := m.readRecord("alpha")
	if err != nil || !record.Restore || record.Name != "alpha" {
		t.Fatal(record, err)
	}
	after, err := os.ReadFile(outside)
	if err != nil || string(after) != string(original) {
		t.Fatal("symlink target modified", err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files leaked", entries, err)
	}
}

func TestRecordStorageCannotReadSymlinkOutsideDataDir(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "private.json")
	if err := os.WriteFile(outside, []byte(`{"name":"alpha","restore":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dataDir, "alpha.session.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m := &Manager{dataDir: dataDir}
	if _, err := m.readRecord("alpha"); err == nil {
		t.Fatal("record symlink escaped DATA_DIR")
	}
}
