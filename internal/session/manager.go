// Package session manages WhatsApp sessions backed by whatsmeow, mirroring the
// SessionManager concept from the Node server's provider layer.
package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Status mirrors the connection states exposed by the Node server so HTTP
// responses stay compatible.
type Status string

const (
	StatusInitializing Status = "INITIALIZING"
	StatusQRCode       Status = "QRCODE"
	StatusConnected    Status = "CONNECTED"
	StatusClosed       Status = "CLOSED"
)

// Handle is the Go counterpart of the Node SessionHandle: it owns one
// whatsmeow client plus the session's current status and last QR code.
type Handle struct {
	Name   string
	Client *whatsmeow.Client
	Status Status
	QRCode string
}

// EventSink receives normalized session events (QR, message, connection). The
// webhook dispatcher implements this.
type EventSink interface {
	OnQR(session, code string)
	OnConnected(session string)
	OnMessage(session string, msg *events.Message)
}

// Manager owns all live sessions, keyed by name.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Handle
	dataDir  string
	sink     EventSink
	log      waLog.Logger
}

// NewManager creates a session manager backed by one SQLite store per session.
func NewManager(_ context.Context, dataDir string, sink EventSink) (*Manager, error) {
	logger := waLog.Stdout("wppgo", "INFO", true)
	return &Manager{
		sessions: make(map[string]*Handle),
		dataDir:  dataDir,
		sink:     sink,
		log:      logger,
	}, nil
}

// Get returns the handle for a session, if it exists.
func (m *Manager) Get(name string) (*Handle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.sessions[name]
	return h, ok
}

func (m *Manager) List() []*Handle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Handle, 0, len(m.sessions))
	for _, h := range m.sessions {
		out = append(out, h)
	}
	return out
}

func (m *Manager) openContainer(ctx context.Context, name string) (*sqlstore.Container, error) {
	if err := os.MkdirAll(m.dataDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(m.dataDir, safeName(name)+".db")
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+dbPath+"?_foreign_keys=on", m.log)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return container, nil
}

func safeName(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "." || name == "" {
		return "default"
	}
	return name
}

// Start creates (or reuses) a session and begins connecting. It registers the
// event handlers that forward QR / connection / message events to the sink.
func (m *Manager) Start(ctx context.Context, name string) (*Handle, error) {
	m.mu.Lock()
	if h, ok := m.sessions[name]; ok {
		m.mu.Unlock()
		return h, nil
	}

	container, err := m.openContainer(ctx, name)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("get device: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, m.log)
	h := &Handle{Name: name, Client: client, Status: StatusInitializing}
	m.sessions[name] = h
	m.mu.Unlock()

	client.AddEventHandler(func(evt any) {
		switch v := evt.(type) {
		case *events.Connected:
			h.Status = StatusConnected
			m.sink.OnConnected(name)
		case *events.Message:
			m.sink.OnMessage(name, v)
		}
	})

	if client.Store.ID == nil {
		// Not logged in: stream QR codes until the user scans.
		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					h.Status = StatusQRCode
					h.QRCode = evt.Code
					m.sink.OnQR(name, evt.Code)
				}
			}
		}()
	} else if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return h, nil
}

// Close disconnects and forgets a session.
func (m *Manager) Close(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.sessions[name]; ok {
		h.Client.Disconnect()
		h.Status = StatusClosed
		delete(m.sessions, name)
	}
}
