package session

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

// ConfigureHistory must run before restoration or accepting requests.
func (m *Manager) ConfigureHistory(options history.Options) error {
	m.op.Lock()
	defer m.op.Unlock()
	if len(m.List()) > 0 {
		return errors.New("history settings require an idle manager")
	}
	m.historyOptions = options
	return nil
}

// OpenHistory owns a short-lived connection independent of session connectivity.
// The caller must close it. Unknown sessions never create empty history stores.
func (m *Manager) OpenHistory(name string) (*history.Store, error) {
	if !validName(name) {
		return nil, ErrInvalidName
	}
	if m.stopping.Load() {
		return nil, ErrStopping
	}
	path := filepath.Join(m.dataDir, name+".history.db")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidName
	}
	return history.Open(path, m.historyOptions)
}
