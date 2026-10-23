// Package session manages WhatsApp sessions backed by whatsmeow, mirroring the
// SessionManager concept from the Node server's provider layer.
package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "modernc.org/sqlite"
)

// Status mirrors the connection states exposed by the Node server so HTTP
// responses stay compatible.
type Status string

const (
	StatusInitializing Status = "INITIALIZING"
	StatusQRCode       Status = "QRCODE"
	StatusConnected    Status = "CONNECTED"
	StatusClosed       Status = "CLOSED"
	StatusDisconnected Status = "DISCONNECTED"
	StatusUnpaired     Status = "UNPAIRED"
)

// Handle is the Go counterpart of the Node SessionHandle: it owns one
// whatsmeow client plus the session's current status and last QR code.
//
// Status and QRCode are written concurrently by the event-handler goroutine
// (events.Connected) and the QR-forwarding goroutine (GetQRChannel), while
// being read from HTTP handler goroutines via GetStatus/GetQRCode. mu guards
// both fields; Name and Client are set once before the handle is published
// and never mutated afterwards, so they can be read without holding mu.
type Handle struct {
	Name      string
	Client    *whatsmeow.Client
	Media     *media.Store
	Directory *directory.State
	History   *history.Store

	mu        sync.RWMutex
	status    Status
	qrCode    string
	runtime   runtimeClient
	container io.Closer
	cancel    context.CancelFunc
	ctx       context.Context
	paired    bool
	events    sync.RWMutex
	closing   bool
	workers   sync.WaitGroup
	settings  Settings
}

// GetStatus returns the handle's current status.
func (h *Handle) GetStatus() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

// SetStatus updates the handle's current status.
func (h *Handle) SetStatus(s Status) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = s
}

// GetQRCode returns the handle's last QR code.
func (h *Handle) GetQRCode() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.qrCode
}

// SetQRCode updates the handle's last QR code.
func (h *Handle) SetQRCode(code string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.qrCode = code
}

// EventSink receives normalized session events (QR, message, connection). The
// webhook dispatcher implements this.
type EventSink interface {
	OnQR(session, code string)
	OnConnected(session string)
	OnMessage(session string, msg *events.Message)
}

// GroupEventSink extends sinks that consume group lifecycle and metadata events.
type GroupEventSink interface {
	OnGroupEvent(session string, evt any)
}

type DirectoryEventSink interface{ OnDirectoryEvent(session string, evt any) }

type StatusEventSink interface {
	OnStatus(session string, status Status)
}

type runtimeClient interface {
	ConnectContext(context.Context) error
	Disconnect()
	Logout(context.Context) error
	IsLoggedIn() bool
	GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error)
	AddEventHandler(whatsmeow.EventHandler) uint32
	RemoveEventHandlers()
}

var (
	ErrStopping     = errors.New("session manager is shutting down")
	ErrDisconnected = errors.New("start and connect the session before logging out")
	ErrInvalidName  = errors.New("session must contain 1 to 64 ASCII letters, digits, underscores or hyphens")
)

// Manager owns all live sessions, keyed by name.
type Manager struct {
	mu             sync.RWMutex
	sessions       map[string]*Handle
	dataDir        string
	sink           EventSink
	log            waLog.Logger
	op             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	stopping       atomic.Bool
	restoring      atomic.Bool
	shutdownOnce   sync.Once
	shutdownDone   chan struct{}
	shutdownErr    error
	open           func(context.Context, string) (*Handle, error)
	historyOptions history.Options
}

// NewManager creates a session manager backed by one SQLite store per session.
func NewManager(ctx context.Context, dataDir string, sink EventSink) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	logger := waLog.Stdout("wppgo", "INFO", true)
	life, cancel := context.WithCancel(ctx)
	m := &Manager{
		sessions: make(map[string]*Handle),
		dataDir:  dataDir,
		sink:     sink,
		log:      logger,
		ctx:      life, cancel: cancel, shutdownDone: make(chan struct{}),
		historyOptions: history.DefaultOptions(),
	}
	m.open = m.openHandle
	return m, nil
}

func (m *Manager) IsStopping() bool { return m.stopping.Load() }
func (m *Manager) IsReady() bool    { return !m.stopping.Load() && !m.restoring.Load() }

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
	if !validName(name) {
		return nil, ErrInvalidName
	}
	if err := os.MkdirAll(m.dataDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(m.dataDir, name+".db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	db.SetMaxOpenConns(1)
	container := sqlstore.NewWithDB(db, "sqlite", m.log)
	if err = container.Upgrade(ctx); err != nil {
		container.Close()
		return nil, fmt.Errorf("upgrade store: %w", err)
	}
	return container, nil
}

var sessionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validName(name string) bool {
	return sessionNamePattern.MatchString(name) && filepath.IsLocal(name)
}

func (m *Manager) openHandle(ctx context.Context, name string) (*Handle, error) {
	container, err := m.openContainer(ctx, name)
	if err != nil {
		return nil, err
	}
	record, err := m.readRecord(name)
	if err != nil {
		container.Close()
		return nil, err
	}
	if record.Revoked {
		devices, e := container.GetAllDevices(ctx)
		if e != nil {
			container.Close()
			return nil, e
		}
		for _, device := range devices {
			if e = device.Delete(ctx); e != nil {
				container.Close()
				return nil, e
			}
		}
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		container.Close()
		return nil, fmt.Errorf("get device: %w", err)
	}
	index, err := directory.Open(filepath.Join(m.dataDir, name+".directory.db"))
	if err != nil {
		container.Close()
		return nil, fmt.Errorf("open chat index: %w", err)
	}
	client := whatsmeow.NewClient(deviceStore, m.log)
	if err = ValidateSettings(record.Settings); err != nil {
		index.Close()
		container.Close()
		return nil, err
	}
	if record.Settings.ProxyURL != "" {
		if err = client.SetProxyAddress(record.Settings.ProxyURL); err != nil {
			index.Close()
			container.Close()
			return nil, errors.New("session proxy configuration failed")
		}
	}
	archive, err := history.Open(filepath.Join(m.dataDir, name+".history.db"), m.historyOptions)
	if err != nil {
		index.Close()
		container.Close()
		return nil, fmt.Errorf("open message history: %w", err)
	}
	client.BackgroundEventCtx = ctx
	client.InitialAutoReconnect = deviceStore.ID != nil
	return &Handle{Name: name, Client: client, runtime: client, container: container, paired: deviceStore.ID != nil,
		Media: media.NewStore(), History: archive, Directory: directory.NewState(index), settings: record.Settings, status: StatusInitializing}, nil
}

// Start creates (or reuses) a session and begins connecting. It registers the
// event handlers that forward QR / connection / message events to the sink.
func (m *Manager) Start(ctx context.Context, name string) (*Handle, error) {
	if !validName(name) {
		return nil, ErrInvalidName
	}
	m.op.Lock()
	defer m.op.Unlock()
	return m.startLocked(ctx, name)
}

func (m *Manager) startLocked(ctx context.Context, name string) (*Handle, error) {
	if m.stopping.Load() || m.ctx.Err() != nil {
		return nil, ErrStopping
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h, ok := m.Get(name); ok {
		return h, nil
	}
	if len(m.List()) >= 100 {
		return nil, errors.New("limit of 100 live sessions reached")
	}
	life, cancel := context.WithCancel(m.ctx)
	stopRequest := context.AfterFunc(ctx, cancel)
	defer stopRequest()
	timer := time.AfterFunc(30*time.Second, cancel)
	defer timer.Stop()
	h, err := m.open(life, name)
	if err != nil {
		cancel()
		return nil, err
	}
	h.ctx, h.cancel = life, cancel
	record, recordErr := m.readRecord(name)
	if recordErr != nil {
		m.release(h)
		return nil, recordErr
	}
	record.Restore = true
	record.Revoked = false
	if err = m.writeRecord(record); err != nil {
		m.release(h)
		return nil, err
	}
	m.mu.Lock()
	m.sessions[name] = h
	m.mu.Unlock()
	fail := func(err error) (*Handle, error) {
		m.release(h)
		m.mu.Lock()
		delete(m.sessions, name)
		m.mu.Unlock()
		return nil, err
	}
	h.runtime.AddEventHandler(func(evt any) {
		h.events.RLock()
		defer h.events.RUnlock()
		if h.closing {
			return
		}
		switch v := evt.(type) {
		case *events.Connected:
			h.SetStatus(StatusConnected)
			h.SetQRCode("")
			if h.Settings().AlwaysOnline && h.Client != nil {
				ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
				h.Client.SendPresence(ctx, types.PresenceAvailable)
				cancel()
			}
			if m.sink != nil {
				m.sink.OnConnected(name)
			}
		case *events.Disconnected:
			m.status(h, StatusDisconnected)
			h.SetQRCode("")
			h.Directory.ResetPresence()
		case events.PermanentDisconnect:
			state := StatusClosed
			_, revoked := evt.(*events.LoggedOut)
			if revoked {
				state = StatusUnpaired
			}
			m.status(h, state)
			go m.permanent(h, revoked)
		case *events.Message:
			m.forwardMessage(h, v)
		case *events.Receipt:
			if h.History != nil {
				ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
				err := h.History.Receipt(ctx, v)
				cancel()
				if err != nil && m.log != nil {
					m.log.Warnf("receipt persistence failed: %v", err)
				}
			}
			m.forwardProtocolEvent(h, v)
		case *events.GroupInfo, *events.JoinedGroup, *events.Picture:
			m.observeDirectory(h, v)
			m.forwardGroupEvent(name, v)
			if _, ok := evt.(*events.Picture); ok {
				m.forwardProtocolEvent(h, evt)
			}
		case *events.HistorySync, *events.Archive, *events.Pin, *events.Mute, *events.MarkChatAsRead, *events.DeleteChat, *events.Presence, *events.ChatPresence:
			if historyEvent, ok := evt.(*events.HistorySync); ok {
				m.importHistory(h, historyEvent)
			}
			m.forwardDirectoryEvent(h, v)
		case *events.LabelEdit, *events.LabelAssociationChat, *events.LabelAssociationMessage:
			if h.History != nil {
				ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
				err := h.History.ObserveLabels(ctx, evt)
				cancel()
				if err != nil && m.log != nil {
					m.log.Warnf("label persistence failed: %v", err)
				}
			}
			m.forwardProtocolEvent(h, evt)
		case *events.CallOffer, *events.CallAccept, *events.CallTerminate, *events.CallReject, *events.NewsletterJoin, *events.NewsletterLeave, *events.NewsletterMuteChange, *events.NewsletterLiveUpdate:
			m.forwardProtocolEvent(h, evt)
		case *events.PrivacySettings, *events.Blocklist, *events.UserAbout:
			m.forwardProtocolEvent(h, evt)
		}
	})

	if !h.paired {
		// Not logged in: stream QR codes until the user scans.
		qrChan, err := h.runtime.GetQRChannel(life)
		if err != nil {
			return fail(fmt.Errorf("QR channel: %w", err))
		}
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			for {
				select {
				case <-life.Done():
					return
				case evt, ok := <-qrChan:
					if !ok {
						return
					}
					h.events.RLock()
					if h.closing {
						h.events.RUnlock()
						return
					}
					if evt.Event == "code" {
						h.SetStatus(StatusQRCode)
						h.SetQRCode(evt.Code)
						if m.sink != nil {
							m.sink.OnQR(name, evt.Code)
						}
					} else {
						h.SetQRCode("")
						if evt.Event != "success" {
							go m.permanent(h, false)
						}
					}
					h.events.RUnlock()
				}
			}
		}()
	}
	if err := h.runtime.ConnectContext(life); err != nil {
		return fail(fmt.Errorf("connect: %w", err))
	}
	if err := life.Err(); err != nil {
		return fail(err)
	}
	return h, nil
}

func (m *Manager) forwardGroupEvent(name string, evt any) {
	if picture, ok := evt.(*events.Picture); ok && (picture == nil || picture.JID.Server != types.GroupServer) {
		return
	}
	if sink, ok := m.sink.(GroupEventSink); ok {
		sink.OnGroupEvent(name, evt)
	}
}

func (m *Manager) forwardMessage(h *Handle, evt *events.Message) {
	if evt == nil {
		return
	}
	m.observeDirectory(h, evt)
	if h.History != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		message := evt.Message
		if message == nil {
			message = evt.RawMessage
		}
		if err := h.History.Put(ctx, evt.Info, message); err != nil && m.log != nil {
			m.log.Warnf("message history update failed: %v", err)
		}
		m.applyMessageChange(ctx, h, evt)
		if message.GetPollUpdateMessage() != nil && h.Client != nil {
			vote, err := h.Client.DecryptPollVote(ctx, evt)
			if err == nil {
				voter := evt.Info.Sender.ToNonAD()
				if evt.Info.SenderAlt.Server == types.DefaultUserServer {
					voter = evt.Info.SenderAlt.ToNonAD()
				}
				err = h.History.Vote(ctx, evt.Info.Chat.String(), message.GetPollUpdateMessage().GetPollCreationMessageKey().GetID(), voter.String(), message.GetPollUpdateMessage().GetSenderTimestampMS(), vote.GetSelectedOptions())
			}
			if err != nil && m.log != nil {
				m.log.Warnf("poll vote unavailable: %v", err)
			}
		}
		cancel()
	}
	// Register before dispatch so webhook consumers can download immediately.
	message := evt.Message
	if message == nil {
		message = evt.RawMessage
	}
	h.Media.Remember(evt.Info.ID, message)
	if m.sink != nil {
		m.sink.OnMessage(h.Name, evt)
	}
	if h.Settings().AutoRead && !evt.Info.IsFromMe && h.Client != nil && evt.Info.ID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		sender := evt.Info.Sender
		if !evt.Info.IsGroup {
			sender = types.EmptyJID
		}
		err := h.Client.MarkRead(ctx, []types.MessageID{evt.Info.ID}, time.Now(), evt.Info.Chat, sender)
		if err == nil && h.History != nil {
			h.History.MarkRead(ctx, evt.Info.Chat.String(), []string{evt.Info.ID})
		}
		cancel()
	}
}

func (m *Manager) observeDirectory(h *Handle, evt any) {
	if err := h.Directory.Observe(evt); err != nil && m.log != nil {
		m.log.Warnf("chat index update failed: %v", err)
	}
}

func (m *Manager) forwardDirectoryEvent(h *Handle, evt any) {
	m.observeDirectory(h, evt)
	if sink, ok := m.sink.(DirectoryEventSink); ok {
		sink.OnDirectoryEvent(h.Name, evt)
	}
}

func (m *Manager) status(h *Handle, status Status) {
	h.SetStatus(status)
	if sink, ok := m.sink.(StatusEventSink); ok {
		sink.OnStatus(h.Name, status)
	}
}

func (m *Manager) release(h *Handle) error {
	h.events.Lock()
	h.closing = true
	h.SetQRCode("")
	h.events.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
	if h.runtime != nil {
		h.runtime.Disconnect()
		h.runtime.RemoveEventHandlers()
	}
	h.workers.Wait()
	var err error
	if h.History != nil {
		err = h.History.Close()
	}
	if h.Directory != nil {
		err = errors.Join(err, h.Directory.Close())
	}
	if h.container != nil {
		err = errors.Join(err, h.container.Close())
	}
	return err
}

// Close preserves credentials but disables automatic restoration until Start.
func (m *Manager) Close(name string) error {
	if !validName(name) {
		return ErrInvalidName
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
	record.Restore = false
	if err = m.writeRecord(record); err != nil {
		return err
	}
	if h, ok := m.Get(name); ok {
		m.status(h, StatusClosed)
		err = m.release(h)
		m.mu.Lock()
		delete(m.sessions, name)
		m.mu.Unlock()
	}
	return err
}

// Logout revokes the linked device. Failed revocation preserves the live handle.
func (m *Manager) Logout(ctx context.Context, name string) error {
	if !validName(name) {
		return ErrInvalidName
	}
	m.op.Lock()
	defer m.op.Unlock()
	if m.stopping.Load() {
		return ErrStopping
	}
	h, ok := m.Get(name)
	if !ok || !h.runtime.IsLoggedIn() {
		return ErrDisconnected
	}
	if err := h.runtime.Logout(ctx); err != nil {
		return err
	}
	err := m.writeRecord(sessionRecord{Name: name, Revoked: true, Settings: h.Settings()})
	m.status(h, StatusUnpaired)
	err = errors.Join(err, m.release(h))
	m.mu.Lock()
	delete(m.sessions, name)
	m.mu.Unlock()
	return err
}

func (m *Manager) permanent(h *Handle, revoked bool) {
	m.op.Lock()
	defer m.op.Unlock()
	if current, ok := m.Get(h.Name); !ok || current != h {
		return
	}
	if err := m.writeRecord(sessionRecord{Name: h.Name, Revoked: revoked, Settings: h.Settings()}); err != nil {
		m.log.Errorf("persist session stop: %v", err)
	}
	if err := m.release(h); err != nil {
		m.log.Warnf("close session: %v", err)
	}
	m.mu.Lock()
	delete(m.sessions, h.Name)
	m.mu.Unlock()
}

// Shutdown retains restoration intent and drains owned resources once.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.shutdownOnce.Do(func() {
		m.stopping.Store(true)
		m.cancel()
		go func() {
			defer close(m.shutdownDone)
			m.op.Lock()
			defer m.op.Unlock()
			for _, h := range m.List() {
				m.status(h, StatusClosed)
				m.shutdownErr = errors.Join(m.shutdownErr, m.release(h))
				m.mu.Lock()
				delete(m.sessions, h.Name)
				m.mu.Unlock()
			}
		}()
	})
	select {
	case <-m.shutdownDone:
		return m.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
