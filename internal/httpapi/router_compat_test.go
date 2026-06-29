package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
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
