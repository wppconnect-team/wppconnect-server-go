package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

func contractRouter(t *testing.T) routers.Router {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(OpenAPISpec())
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The deployment-relative server URL is resolved by clients; the test server
	// uses a random authority, so match paths independently of that authority.
	doc.Servers = nil
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func TestOpenAPIValidAndExportCurrent(t *testing.T) {
	contractRouter(t)
	static, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(static, OpenAPISpec()) {
		t.Fatal("regenerate docs/openapi.json with go run ./cmd/openapi")
	}
}

// This runs the actual HTTP stack over TCP; only the WhatsApp transport is simulated.
// Existing per-capability tests cover protocol faults, limits and isolation in depth.
func TestCapabilitiesE2EAgainstOpenAPI(t *testing.T) {
	contract := contractRouter(t)
	groups := newGroupHarness(t)
	contacts := newDirectoryHarness(t)
	media := newMediaHarness(t)
	sessionRouter := testRouter(t, "test-secret")
	for _, tc := range []struct {
		name, method, path, token, body string
		handler                         http.Handler
		status                          int
	}{
		{"auth-bootstrap", "POST", "/api/alpha/test-secret/generate-token", "", "", sessionRouter, 201},
		{"openapi", "GET", "/openapi.json", "", "", sessionRouter, 200},
		{"health", "GET", "/healthz", "", "", sessionRouter, 200},
		{"ready", "GET", "/readyz", "", "", sessionRouter, 200},
		{"close", "POST", "/api/alpha/close-session", media.tokens["alpha"], "", sessionRouter, 200},
		{"logout-disconnected", "POST", "/api/alpha/logout-session", media.tokens["alpha"], "", sessionRouter, 409},
		{"group-list", "GET", "/api/alpha/all-groups", groups.tokens["alpha"], "", groups.router, 200},
		{"group-create", "POST", "/api/alpha/create-group", groups.tokens["alpha"], `{"name":"E2E","participants":["5511001"]}`, groups.router, 201},
		{"contacts", "GET", "/api/alpha/all-contacts", contacts.tokens["alpha"], "", contacts.router, 200},
		{"chats", "POST", "/api/alpha/list-chats", contacts.tokens["alpha"], `{"count":2}`, contacts.router, 200},
		{"presence-unknown", "GET", "/api/alpha/chat-is-online/111", contacts.tokens["alpha"], "", contacts.router, 200},
		{"typing", "POST", "/api/alpha/typing", contacts.tokens["alpha"], `{"phone":"111","value":true}`, contacts.router, 200},
		{"history-unsupported", "GET", "/api/alpha/all-new-messages", contacts.tokens["alpha"], "", contacts.router, 501},
		{"media-send", "POST", "/api/alpha/send-image", media.tokens["alpha"], `{"phone":"123","base64":"` + base64.StdEncoding.EncodeToString(pngData(t)) + `"}`, media.router, 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			req, err := http.NewRequest(tc.method, server.URL+tc.path, bytes.NewBufferString(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			route, params, err := contract.FindRoute(req)
			if err != nil {
				t.Fatal(err)
			}
			input := &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
			if err = openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatalf("request contract: %v", err)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("HTTP %d: %s", resp.StatusCode, body)
			}
			validation := &openapi3filter.ResponseValidationInput{RequestValidationInput: input, Status: resp.StatusCode, Header: resp.Header, Body: io.NopCloser(bytes.NewReader(body)), Options: &openapi3filter.Options{IncludeResponseStatus: true}}
			if err = openapi3filter.ValidateResponse(context.Background(), validation); err != nil {
				t.Fatalf("response contract: %v\n%s", err, body)
			}
		})
	}
}
