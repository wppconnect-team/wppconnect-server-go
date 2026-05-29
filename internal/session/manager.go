// Package session manages WhatsApp sessions backed by whatsmeow, mirroring the
// SessionManager concept from the Node server's provider layer.
package session

import (
	"context"
	"fmt"
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
	mu        sync.RWMutex
	sessions  map[string]*Handle
	container *sqlstore.Container
	sink      EventSink
	log       waLog.Logger
}

// NewManager creates a session manager backed by a SQLite store at dbPath.
func NewManager(ctx context.Context, dbPath string, sink EventSink) (*Manager, error) {
	logger := waLog.Stdout("wppgo", "INFO", true)
	container, err := sqlstore.New(ctx, "sqlite3", "file:"+dbPath+"?_foreign_keys=on", logger)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return &Manager{
		sessions:  make(map[string]*Handle),
		container: container,
		sink:      sink,
		log:       logger,
	}, nil
}

// Get returns the handle for a session, if it exists.
func (m *Manager) Get(name string) (*Handle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.sessions[name]
	return h, ok
}

// Start creates (or reuses) a session and begins connecting. It registers the
// event handlers that forward QR / connection / message events to the sink.
func (m *Manager) Start(ctx context.Context, name string) (*Handle, error) {
	m.mu.Lock()
	if h, ok := m.sessions[name]; ok {
		m.mu.Unlock()
		return h, nil
	}

	deviceStore, err := m.container.GetFirstDevice(ctx)
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
