package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type fakeRuntime struct {
	mu                    sync.Mutex
	handler               whatsmeow.EventHandler
	connect               func(context.Context) error
	logout                func(context.Context) error
	loggedIn              bool
	disconnected, removed bool
	qr                    chan whatsmeow.QRChannelItem
}

func (f *fakeRuntime) AddEventHandler(handler whatsmeow.EventHandler) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
	return 1
}
func (f *fakeRuntime) RemoveEventHandlers() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = nil
	f.removed = true
}
func (f *fakeRuntime) emit(evt any) {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler != nil {
		handler(evt)
	}
}
func (f *fakeRuntime) ConnectContext(ctx context.Context) error {
	if f.connect != nil {
		return f.connect(ctx)
	}
	f.mu.Lock()
	f.loggedIn = true
	f.mu.Unlock()
	f.emit(&events.Connected{})
	return nil
}
func (f *fakeRuntime) Disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnected = true
	f.loggedIn = false
}
func (f *fakeRuntime) IsLoggedIn() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.loggedIn }
func (f *fakeRuntime) Logout(ctx context.Context) error {
	if f.logout != nil {
		return f.logout(ctx)
	}
	return nil
}
func (f *fakeRuntime) GetQRChannel(context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	return f.qr, nil
}

func managerForTest(t *testing.T, path string) (*Manager, map[string]*fakeRuntime) {
	t.Helper()
	m, err := NewManager(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := make(map[string]*fakeRuntime)
	m.open = func(ctx context.Context, name string) (*Handle, error) {
		h, err := m.openHandle(ctx, name)
		if err != nil {
			return nil, err
		}
		client := &fakeRuntime{qr: make(chan whatsmeow.QRChannelItem, 8)}
		client.logout = func(ctx context.Context) error { return h.Client.Store.Delete(ctx) }
		h.runtime = client
		clients[name] = client
		return h, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return m, clients
}

func seedDevice(t *testing.T, m *Manager, name string) {
	t.Helper()
	c, err := m.openContainer(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	d := c.NewDevice()
	id := types.NewJID("123", types.DefaultUserServer)
	d.ID = &id
	d.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err = d.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRestartRestoresCredentialsAndChatIndexButNotClosedSessions(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	m, clients := managerForTest(t, path)
	for _, name := range []string{"active", "closed", "legacy"} {
		seedDevice(t, m, name)
	}
	h, err := m.Start(ctx, "active")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Directory.Chats.Message(ctx, &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.NewJID("456", types.DefaultUserServer)}, ID: "persisted", Timestamp: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Start(ctx, "closed"); err != nil {
		t.Fatal(err)
	}
	if err = m.Close("closed"); err != nil {
		t.Fatal(err)
	}
	// An unpaired SQLite store must not trigger a QR on reboot.
	c, err := m.openContainer(ctx, "empty")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err = m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if !clients["active"].disconnected || !clients["active"].removed {
		t.Fatal("client leaked after shutdown")
	}
	if _, err = h.Directory.Chats.List(ctx); err == nil {
		t.Fatal("chat database still open")
	}
	if _, err = h.Client.Store.Contacts.GetAllContacts(ctx); err == nil {
		t.Fatal("credential database still open")
	}
	next, _ := managerForTest(t, path)
	count, err := next.Restore(ctx)
	if err != nil || count != 2 {
		t.Fatalf("restored=%d err=%v", count, err)
	}
	if _, ok := next.Get("closed"); ok {
		t.Fatal("explicitly closed session restored")
	}
	if _, ok := next.Get("empty"); ok {
		t.Fatal("unpaired session restored")
	}
	active, ok := next.Get("active")
	if !ok || !active.paired {
		t.Fatal("paired session not restored")
	}
	list, err := active.Directory.Chats.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("chat index lost: %+v %v", list, err)
	}
	if _, err = next.Start(ctx, "closed"); err != nil {
		t.Fatal(err)
	}
	record, err := next.readRecord("closed")
	if err != nil || !record.Restore {
		t.Fatal("manual start did not reactivate restoration")
	}
}

func TestLogoutFailurePreservesCredentialsAndSuccessRevokes(t *testing.T) {
	ctx := context.Background()
	m, clients := managerForTest(t, t.TempDir())
	seedDevice(t, m, "alpha")
	h, err := m.Start(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	logout := clients["alpha"].logout
	clients["alpha"].logout = func(context.Context) error { return errors.New("rejected") }
	if err = m.Logout(ctx, "alpha"); err == nil {
		t.Fatal("revocation error hidden")
	}
	if current, ok := m.Get("alpha"); !ok || current != h || h.Client.Store.ID == nil {
		t.Fatal("failed logout discarded credentials")
	}
	clients["alpha"].logout = logout
	if err = m.Logout(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get("alpha"); ok {
		t.Fatal("logged out handle retained")
	}
	record, err := m.readRecord("alpha")
	if err != nil || !record.Revoked || record.Restore {
		t.Fatal(record, err)
	}
	fresh, err := m.Start(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.paired || fresh.Client.Store.ID != nil {
		t.Fatal("revoked credentials reused")
	}
}

func TestStartCancellationAndShutdownDoNotLeakResources(t *testing.T) {
	m, _ := managerForTest(t, t.TempDir())
	var opened *Handle
	started := make(chan struct{})
	m.open = func(ctx context.Context, name string) (*Handle, error) {
		h, err := m.openHandle(ctx, name)
		if err != nil {
			return nil, err
		}
		opened = h
		h.runtime = &fakeRuntime{connect: func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }, qr: make(chan whatsmeow.QRChannelItem)}
		return h, nil
	}
	done := make(chan error, 1)
	go func() { _, err := m.Start(context.Background(), "alpha"); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("failed start retained handle")
	}
	if _, err := opened.Directory.Chats.List(context.Background()); err == nil {
		t.Fatal("failed start leaked database")
	}
	if _, err := m.Start(context.Background(), "beta"); !errors.Is(err, ErrStopping) {
		t.Fatal(err)
	}
}

func TestSessionNamesAndCorruptRestoreIsolation(t *testing.T) {
	m, _ := managerForTest(t, t.TempDir())
	for _, name := range []string{"../alpha", "a/b", "a\\b", "a:b", ".", ""} {
		if _, err := m.Start(context.Background(), name); !errors.Is(err, ErrInvalidName) {
			t.Fatal(name, err)
		}
	}
	seedDevice(t, m, "good")
	if err := os.WriteFile(filepath.Join(m.dataDir, "broken.db"), []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := m.Restore(context.Background())
	if count != 1 || err == nil {
		t.Fatalf("restored=%d err=%v", count, err)
	}
	if _, ok := m.Get("good"); !ok {
		t.Fatal("one corrupt store blocked all sessions")
	}
}

func TestSuccessfulStartOutlivesRequestAndConcurrentStartsReuseHandle(t *testing.T) {
	m, _ := managerForTest(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	h, err := m.Start(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if h.ctx.Err() != nil {
		t.Fatal("HTTP request completion stopped the session")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			current, e := m.Start(context.Background(), "alpha")
			if e != nil || current != h {
				t.Errorf("concurrent start: %p %v", current, e)
			}
		}()
	}
	wg.Wait()
	if len(m.List()) != 1 {
		t.Fatal("duplicate sessions")
	}
}
