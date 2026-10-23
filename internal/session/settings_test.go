package session

import (
	"context"
	"go.mau.fi/whatsmeow"
	"testing"
	"time"
)

func (f *fakeRuntime) PairPhone(ctx context.Context, phone string, _ bool, _ whatsmeow.PairClientType, _ string) (string, error) {
	return "1234-5678", ctx.Err()
}

func TestSettingsPersistAcrossCloseAndRestore(t *testing.T) {
	path := t.TempDir()
	m, _ := managerForTest(t, path)
	h, err := m.Start(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	value := Settings{AutoRead: true, AlwaysOnline: true, ProxyURL: "socks5://user:secret@127.0.0.1:1080"}
	if err = m.UpdateSettings("alpha", value); err != nil {
		t.Fatal(err)
	}
	if h.Settings() != value {
		t.Fatal("active settings not updated")
	}
	if err = m.Close("alpha"); err != nil {
		t.Fatal(err)
	}
	next, _ := managerForTest(t, path)
	got, err := next.Settings("alpha")
	if err != nil || got != value {
		t.Fatalf("persisted settings %#v %v", got, err)
	}
	for _, proxy := range []string{"file:///secret", "http:///missing", "https://"} {
		if err = next.UpdateSettings("alpha", Settings{ProxyURL: proxy}); err == nil {
			t.Fatal("invalid proxy accepted", proxy)
		}
	}
}

func TestPhonePairingValidationAndCancellation(t *testing.T) {
	m, clients := managerForTest(t, t.TempDir())
	for _, phone := range []string{"123", "+5511999999999", "5511abc99999"} {
		if _, err := m.PairPhone(context.Background(), "alpha", phone); err == nil {
			t.Fatal("invalid phone accepted")
		}
	}
	h, err := m.Start(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	clients["alpha"].mu.Lock()
	clients["alpha"].loggedIn = false
	clients["alpha"].mu.Unlock()
	h.SetStatus(StatusQRCode)
	h.mu.Lock()
	h.qrCode = "synthetic-qr"
	h.mu.Unlock()
	code, err := m.PairPhone(context.Background(), "alpha", "5511999999999")
	if err != nil || code != "1234-5678" {
		t.Fatalf("pairing %q %v", code, err)
	}
	h.mu.Lock()
	h.qrCode = ""
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = m.PairPhone(ctx, "alpha", "5511999999999"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
