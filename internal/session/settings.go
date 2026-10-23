package session

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
)

type Settings struct {
	AutoRead     bool   `json:"autoRead"`
	AlwaysOnline bool   `json:"alwaysOnline"`
	ProxyURL     string `json:"proxyUrl,omitempty"`
}

func ValidName(name string) bool { return validName(name) }

func ValidateSettings(v Settings) error {
	if v.ProxyURL != "" {
		u, err := url.Parse(v.ProxyURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
			return errors.New("proxyUrl must use http, https or socks5")
		}
		if len(v.ProxyURL) > 2048 {
			return errors.New("proxyUrl is too long")
		}
	}
	return nil
}
func (h *Handle) Settings() Settings     { h.mu.RLock(); defer h.mu.RUnlock(); return h.settings }
func (h *Handle) setSettings(v Settings) { h.mu.Lock(); defer h.mu.Unlock(); h.settings = v }
func (m *Manager) Settings(name string) (Settings, error) {
	if !validName(name) {
		return Settings{}, ErrInvalidName
	}
	record, err := m.readRecord(name)
	return record.Settings, err
}
func (m *Manager) UpdateSettings(name string, value Settings) error {
	if !validName(name) {
		return ErrInvalidName
	}
	if err := ValidateSettings(value); err != nil {
		return err
	}
	m.op.Lock()
	defer m.op.Unlock()
	if m.stopping.Load() {
		return ErrStopping
	}
	record, err := m.readRecord(name)
	if err != nil {
		return err
	}
	record.Settings = value
	if err = m.writeRecord(record); err != nil {
		return err
	}
	if h, ok := m.Get(name); ok {
		h.setSettings(value)
	}
	return nil
}
func (m *Manager) Names() ([]string, error) {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		for _, suffix := range []string{".session.json", ".db"} {
			if strings.HasSuffix(entry.Name(), suffix) {
				name := strings.TrimSuffix(entry.Name(), suffix)
				if validName(name) {
					set[name] = true
				}
			}
		}
	}
	for _, h := range m.List() {
		set[h.Name] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (m *Manager) PairPhone(ctx context.Context, name, phone string) (string, error) {
	if len(phone) < 7 || len(phone) > 15 {
		return "", errors.New("phone must contain 7..15 digits")
	}
	for _, c := range phone {
		if c < '0' || c > '9' {
			return "", errors.New("phone must contain digits only")
		}
	}
	h, err := m.Start(ctx, name)
	if err != nil {
		return "", err
	}
	if h.GetStatus() == StatusConnected {
		return "", errors.New("session is already paired")
	}
	pairer, ok := h.runtime.(interface {
		PairPhone(context.Context, string, bool, whatsmeow.PairClientType, string) (string, error)
	})
	if !ok {
		return "", errors.New("phone pairing unavailable")
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for h.GetQRCode() == "" {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("pairing transport not ready; retry when a QR code is available")
		case <-ticker.C:
		}
	}
	return pairer.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
}
