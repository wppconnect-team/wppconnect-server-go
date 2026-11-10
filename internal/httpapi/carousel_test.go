package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCarouselAllCardsValidatedBeforeAnyUploadAndReplay(t *testing.T) {
	h := newMediaHarness(t)
	png := base64.StdEncoding.EncodeToString(pngData(t))
	q := carouselRequest{Phone: "123", Body: "Choose", Cards: []carouselCard{{Body: "One", Image: png, Buttons: []carouselButton{{ID: "one", Label: "Select"}}}, {Body: "Two", Image: png, Buttons: []carouselButton{{ID: "two", Label: "Select"}}}}}
	if r := h.post(t, "send-carousel", q); r.Code != 501 || h.client.uploads != 0 {
		t.Fatal("default gate", r.Code)
	}
	ledger, err := sendledger.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	h.router = (&Server{cfg: config.Config{SecretKey: "test-secret", ExperimentalInteractives: true}, sends: ledger, mediaSession: func(name string) (media.Client, *media.Store, bool) { return h.client, h.stores[name], name == "alpha" }}).router()
	q.Cards[1].Buttons[0].ID = "one"
	if r := h.post(t, "send-carousel", q); r.Code != 400 || h.client.uploads != 0 {
		t.Fatal("duplicate reply IDs caused upload", r.Code)
	}
	q.Cards[1].Buttons[0].ID = "two"
	q.Cards[1].Image = "https://example.invalid/image"
	if r := h.post(t, "send-carousel", q); r.Code != 400 || h.client.uploads != 0 {
		t.Fatal("invalid second image caused upload", r.Code)
	}
	q.Cards[1].Image = png
	post := func(key, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(q)
		r := httptest.NewRequest("POST", "/api/alpha/send-carousel", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, r)
		return w
	}
	if w := post("one", h.tokens["beta"]); w.Code != 401 || h.client.uploads != 0 {
		t.Fatal("cross-session upload")
	}
	first := post("one", h.tokens["alpha"])
	second := post("one", h.tokens["alpha"])
	if first.Code != 201 || first.Body.String() != second.Body.String() || second.Header().Get("Idempotency-Replayed") != "true" || h.client.uploads != 2 || len(h.client.sent) != 1 {
		t.Fatal("incorrect send or replay", first.Body.String())
	}
	cards := h.client.sent[0].GetInteractiveMessage().GetCarouselMessage().GetCards()
	if len(cards) != 2 || cards[1].GetHeader().GetImageMessage().GetDirectPath() != "/media" || !strings.Contains(cards[1].GetNativeFlowMessage().GetButtons()[0].GetButtonParamsJSON(), `"id":"two"`) {
		t.Fatal("incorrect carousel payload")
	}
	h.client.uploadErr = errors.New("private fixture")
	w := post("fail", h.tokens["alpha"])
	if w.Code != 502 || len(h.client.sent) != 1 || strings.Contains(w.Body.String(), "private fixture") {
		t.Fatal("failed upload sent a message or disclosed error")
	}
}
