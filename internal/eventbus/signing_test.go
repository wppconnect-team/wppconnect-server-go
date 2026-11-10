package eventbus

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSignedWebhookBindsBodyIdentityAndAttempt(t *testing.T) {
	secret := strings.Repeat("test-key", 4)
	body := []byte("{\"message\":\"Ol\u00e1\", \"count\": 1}\n")
	var signatures []string
	received := make(chan string, 2)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil || string(raw) != string(body) {
			t.Error("delivery changed the raw body")
		}
		timestamp := r.Header.Get("X-WPP-Timestamp")
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Since(time.Unix(seconds, 0)) < -time.Second || time.Since(time.Unix(seconds, 0)) > 5*time.Second {
			t.Error("missing or stale delivery timestamp")
		}
		id, attempt := r.Header.Get("X-WPP-Event-ID"), r.Header.Get("X-WPP-Attempt")
		if id != "event-123" || (attempt != "1" && attempt != "2") {
			t.Error("unexpected delivery identity")
		}
		signature := r.Header.Get("X-WPP-Signature")
		verify := func(key, stamp, eventID, deliveryAttempt string, data []byte) bool {
			mac := hmac.New(sha256.New, []byte(key))
			mac.Write([]byte(strings.Join([]string{stamp, eventID, deliveryAttempt, ""}, ".")))
			mac.Write(data)
			return hmac.Equal([]byte(signature), []byte("v1="+hex.EncodeToString(mac.Sum(nil))))
		}
		if !verify(secret, timestamp, id, attempt, raw) {
			t.Error("receiver could not authenticate the original delivery")
		}
		if verify("different-key", timestamp, id, attempt, raw) ||
			verify(secret, timestamp, "other-event", attempt, raw) ||
			verify(secret, "0", id, attempt, raw) ||
			verify(secret, timestamp, id, "99", raw) ||
			verify(secret, timestamp, id, attempt, append(raw, ' ')) {
			t.Error("tampered delivery authenticated")
		}
		received <- signature
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	target, err := NewHTTP(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	target.SigningSecret = secret
	for attempt := 1; attempt <= 2; attempt++ {
		if err := target.Deliver(context.Background(), Event{ID: "event-123", Data: body, Attempt: attempt}); err != nil {
			t.Fatal(err)
		}
		signatures = append(signatures, <-received)
	}
	if len(signatures) != 2 || signatures[0] == signatures[1] {
		t.Fatal("each attempt must be signed independently")
	}
}

func TestWebhookSigningIsOptionalAndRedirectsStayDisabled(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-WPP-Signature") != "" || r.Header.Get("X-WPP-Timestamp") != "" {
			t.Error("unsigned mode sent signing headers")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	target, err := NewHTTP(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := target.Deliver(context.Background(), Event{ID: "unsigned", Data: []byte("{}"), Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, receiver.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	target.URL = redirect.URL
	target.SigningSecret = strings.Repeat("k", 32)
	if err := target.Deliver(context.Background(), Event{ID: "signed", Data: []byte("{}"), Attempt: 1}); err == nil {
		t.Fatal("signed delivery followed a redirect")
	}
}
