package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandedCapabilitiesTCPAgainstOpenAPI(t *testing.T) {
	contract := contractRouter(t)
	h := newMediaHarness(t)
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"), history.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("a", 32)
	fake := &roadmapAccount{}
	s := &Server{cfg: config.Config{SecretKey: "test-secret", AdminToken: token}, historySession: func(string) (*history.Store, func(), error) { return store, func() {}, nil }, mediaSession: func(name string) (media.Client, *media.Store, bool) { v, ok := h.stores[name]; return h.client, v, ok }, accountSession: func(string) (account.Client, bool) { return fake, true }}
	server := httptest.NewServer(s.router())
	defer server.Close()
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"GET", "/api/alpha/messages?limit=2", "", h.tokens["alpha"], 200},
		{"GET", "/api/alpha/all-new-messages", "", h.tokens["alpha"], 200},
		{"POST", "/api/alpha/send-message", `{"phone":"123","message":"text","mentionedList":["456"]}`, h.tokens["alpha"], 201},
		{"POST", "/api/alpha/privacy-settings", `{"name":"online","value":"match_last_seen"}`, h.tokens["alpha"], 200},
		{"POST", "/api/alpha/edit-label", `{"labelId":"1","name":"Test","color":1}`, h.tokens["alpha"], 200},
		{"POST", "/api/alpha/send-status-text", `{"message":"A status"}`, h.tokens["alpha"], 201},
		{"POST", "/api/alpha/send-status-media", `{"base64":"` + base64.StdEncoding.EncodeToString(pngData(t)) + `"}`, h.tokens["alpha"], 201},
		{"POST", "/api/alpha/set-profile-picture", `{"base64":"` + base64.StdEncoding.EncodeToString(pngData(t)) + `"}`, h.tokens["alpha"], 200},
		{"POST", "/api/alpha/send-carousel", `{}`, h.tokens["alpha"], 501},
		{"GET", "/api/admin/capabilities", "", token, 200},
		{"POST", "/api/admin/login", `{"token":"` + token + `"}`, "", 200},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("Authorization", "Bearer "+tc.token)
			route, params, err := contract.FindRoute(req)
			if err != nil {
				t.Fatal(err)
			}
			input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
			if err = openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatal("request contract", err)
			}
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("HTTP %d %s", response.StatusCode, body)
			}
			validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: response.StatusCode, Header: response.Header, Body: io.NopCloser(bytes.NewReader(body)), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
			if err = openapi3filter.ValidateResponse(context.Background(), validation); err != nil {
				t.Fatalf("response contract %v %s", err, body)
			}
		})
	}
}
