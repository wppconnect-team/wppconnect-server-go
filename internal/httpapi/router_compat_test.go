package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"golang.org/x/crypto/bcrypt"
)

func TestRouterMatchesNodeCompatibilitySurface(t *testing.T) {
	mgr, err := session.NewManager(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	router := NewRouter(config.Config{SecretKey: "secret"}, mgr)
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router does not implement chi.Routes")
	}
	seen := map[string]bool{}

	err = chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	if got, want := len(seen), 157; got != want {
		t.Fatalf("route count = %d, want %d", got, want)
	}

	for _, route := range []string{
		"GET /api/dashboard/stats",
		"POST /api/{session}/start-session",
		"GET /api/{session}/status-session",
		"POST /api/{session}/send-message",
		"POST /api/{session}/send-file-base64",
		"POST /api/{session}/send-buttons",
		"GET /api/{session}/all-groups",
		"GET /api/{session}/get-products",
		"DELETE /api/{session}/newsletter/{id}",
		"GET /api-docs",
		"GET /metrics",
		"GET /unhealthy",
		"GET /healthz",
	} {
		if !seen[route] {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestUnsupportedCompatibilityRouteReturnsJSON501(t *testing.T) {
	mgr, err := session.NewManager(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	router := NewRouter(config.Config{SecretKey: "secret"}, mgr)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if got, want := res.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d, body=%s", got, want, res.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["status"] != "not_supported" || body["runtime"] != "wppconnect-server-go" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestFunctionalCompatibilityRoutesDoNotReturnNotSupported(t *testing.T) {
	mgr, err := session.NewManager(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	router := NewRouter(config.Config{SecretKey: "secret"}, mgr)
	token, err := bcrypt.GenerateFromPassword([]byte("demo"+"secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/demo/send-location", `{"phone":"5511999999999","lat":-23.5,"lng":-46.6}`},
		{http.MethodPost, "/api/demo/send-file-base64", `{"phone":"5511999999999","base64":"aGk=","mimetype":"text/plain","filename":"hi.txt"}`},
		{http.MethodPost, "/api/demo/send-file", `{"phone":"5511999999999","base64":"aGk=","mimetype":"text/plain","filename":"hi.txt"}`},
		{http.MethodGet, "/api/demo/group-info/123@g.us", ``},
		{http.MethodGet, "/api/demo/group-admins/123@g.us", ``},
		{http.MethodGet, "/api/demo/group-members-ids/123@g.us", ``},
		{http.MethodPost, "/api/demo/subscribe-presence", `{"phone":"5511999999999"}`},
		{http.MethodPost, "/api/demo/set-online-presence", `{"online":true}`},
		{http.MethodGet, "/api/demo/contact/5511999999999", ``},
		{http.MethodGet, "/api/demo/profile-pic/5511999999999", ``},
		{http.MethodGet, "/api/demo/blocklist", ``},
		{http.MethodPost, "/api/demo/block-contact", `{"phone":"5511999999999"}`},
		{http.MethodGet, "/api/demo/get-phone-number", ``},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		req.Header.Set("Authorization", "Bearer "+string(token))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()

		router.ServeHTTP(res, req)
		if res.Code == http.StatusNotImplemented {
			t.Fatalf("%s %s returned 501: %s", tt.method, tt.path, res.Body.String())
		}

		var body map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s response is not JSON: %v", tt.method, tt.path, err)
		}
		if body["status"] == "not_supported" {
			t.Fatalf("%s %s returned not_supported body: %#v", tt.method, tt.path, body)
		}
	}
}
