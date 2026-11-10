package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChatwootDefaultSecretLength(t *testing.T) {
	if !validSigningSecret(strings.Repeat("z", 24)) || validSigningSecret("") || validSigningSecret(strings.Repeat("z", 23)) {
		t.Fatal("incompatible Chatwoot signing key validation")
	}
}
func TestChatwootReleaseCallback(t *testing.T) {
	path := os.Getenv("CHATWOOT_SIGNATURE_FIXTURE")
	if path == "" {
		t.Skip("run the Chatwoot release fixture workflow to generate a callback")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Secret, Body string
		Headers      map[string]string
	}
	if json.Unmarshal(raw, &data) != nil || len(data.Secret) != 24 || !validSigningSecret(data.Secret) {
		t.Fatal("invalid Chatwoot release fixture")
	}
	posts := 0
	a, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var send struct{ Phone, Message string }
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&send) != nil || send.Phone != "19995550101" || send.Message != "Olá do Chatwoot" {
			t.Error("incorrect public text operation")
		}
		posts++
		w.WriteHeader(201)
		fmt.Fprint(w, acceptedJSON)
	})
	a.secret = data.Secret
	a.now = time.Now
	for _, body := range []string{data.Body, data.Body, data.Body + " "} {
		req := httptest.NewRequest("POST", "/chatwoot", strings.NewReader(body))
		for k, v := range data.Headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, req)
		want := 204
		if body != data.Body {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("signed event HTTP %d want %d", w.Code, want)
		}
	}
	if posts != 1 {
		t.Fatalf("sent %d times", posts)
	}
}
