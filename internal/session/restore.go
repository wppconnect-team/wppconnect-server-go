package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type sessionRecord struct {
	Name     string   `json:"name"`
	Restore  bool     `json:"restore"`
	Revoked  bool     `json:"revoked,omitempty"`
	Settings Settings `json:"settings,omitempty"`
}

func (m *Manager) readRecord(name string) (sessionRecord, error) {
	record := sessionRecord{Name: name}
	if !validName(name) {
		return record, ErrInvalidName
	}
	root, err := os.OpenRoot(m.dataDir)
	if err != nil {
		return record, err
	}
	defer root.Close()
	// Root confines resolution even when a valid filename is a symbolic link.
	f, err := root.Open(name + ".session.json")
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return record, err
	}
	if len(b) > 4096 {
		return record, errors.New("session record exceeds 4 KiB")
	}
	if err = json.Unmarshal(b, &record); err != nil {
		return record, err
	}
	if record.Name != name {
		return record, errors.New("session record name mismatch")
	}
	return record, nil
}

func (m *Manager) writeRecord(record sessionRecord) error {
	if !validName(record.Name) {
		return ErrInvalidName
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(m.dataDir)
	if err != nil {
		return err
	}
	defer root.Close()
	tempName := ".session-" + uuid.NewString() + ".tmp"
	f, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tempName)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.Rename(tempName, record.Name+".session.json")
}

// Restore migrates legacy paired stores, honoring explicit close/logout records.
// Empty stores never initiate unsolicited QR pairing during startup.
func (m *Manager) Restore(ctx context.Context) (int, error) {
	m.restoring.Store(true)
	defer m.restoring.Store(false)
	return m.restore(ctx)
}

type RestoreResult struct {
	Count int
	Err   error
}

func (m *Manager) RestoreAsync(ctx context.Context) <-chan RestoreResult {
	m.restoring.Store(true)
	done := make(chan RestoreResult, 1)
	go func() {
		defer m.restoring.Store(false)
		count, err := m.restore(ctx)
		done <- RestoreResult{Count: count, Err: err}
		close(done)
	}()
	return done
}

func (m *Manager) restore(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".db")
		if validName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	count := 0
	var failures []error
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return count, errors.Join(append(failures, err)...)
		}
		m.op.Lock()
		if m.stopping.Load() {
			m.op.Unlock()
			return count, ErrStopping
		}
		record, e := m.readRecord(name)
		_, statErr := os.Stat(filepath.Join(m.dataDir, name+".session.json"))
		if e == nil && (statErr == nil && (!record.Restore || record.Revoked)) {
			m.op.Unlock()
			continue
		}
		paired := false
		if e == nil {
			container, openErr := m.openContainer(ctx, name)
			e = openErr
			if e == nil {
				devices, deviceErr := container.GetAllDevices(ctx)
				e = errors.Join(deviceErr, container.Close())
				paired = len(devices) > 0
			}
		}
		if e == nil && paired {
			_, e = m.startLocked(ctx, name)
			if e == nil {
				count++
			}
		}
		m.op.Unlock()
		if e != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", name, e))
		}
	}
	return count, errors.Join(failures...)
}
