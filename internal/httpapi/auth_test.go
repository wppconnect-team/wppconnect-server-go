package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"golang.org/x/crypto/bcrypt"
)

func testRouter(t *testing.T, secret string) http.Handler {
	t.Helper()
	mgr, err := session.NewManager(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(config.Config{SecretKey: secret}, mgr)
}

func request(router http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestGenerateTokenAndAuthenticate(t *testing.T) {
	const secret = "test-secret"
	router := testRouter(t, secret)
	w := request(router, http.MethodPost, "/api/alpha/"+secret+"/generate-token", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("generate-token: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response must disable caching")
	}
	var result struct {
		Status  string `json:"status"`
		Session string `json:"session"`
		Token   string `json:"token"`
		Full    string `json:"full"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.Session != "alpha" || result.Full != "alpha:"+result.Token {
		t.Fatalf("unexpected token response: %+v", result)
	}
	raw := strings.NewReplacer("_", "/", "-", "+").Replace(result.Token)
	if err := bcrypt.CompareHashAndPassword([]byte(raw), []byte("alpha"+secret)); err != nil {
		t.Fatalf("not a Node-compatible bcrypt hash: %v", err)
	}
	if cost, err := bcrypt.Cost([]byte(raw)); err != nil || cost != 10 {
		t.Fatalf("bcrypt cost = %d, err = %v", cost, err)
	}
	for _, token := range []string{result.Token, raw} {
		w = request(router, http.MethodGet, "/api/alpha/status-session", "Bearer "+token)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"CLOSED"`) {
			t.Fatalf("generated token rejected: %d %s", w.Code, w.Body.String())
		}
		w = request(router, http.MethodGet, "/api/beta/status-session", "Bearer "+token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("token authenticated another session: %d", w.Code)
		}
	}
	w = request(router, http.MethodGet, "/api/"+url.PathEscape(result.Full)+"/status-session", "")
	if w.Code != http.StatusOK {
		t.Fatalf("full token rejected: %d %s", w.Code, w.Body.String())
	}
	w = request(testRouter(t, "rotated-secret"), http.MethodGet, "/api/alpha/status-session", "Bearer "+result.Token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token accepted after secret rotation: %d", w.Code)
	}
}

func TestTokenRejections(t *testing.T) {
	router := testRouter(t, "test-secret")
	for _, path := range []string{
		"/api/alpha/wrong-secret/generate-token",
		"/api/alpha:other/test-secret/generate-token",
		"/api/" + strings.Repeat("a", 73) + "/test-secret/generate-token",
	} {
		w := request(router, http.MethodPost, path, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), `"token":`) {
			t.Error("rejected request returned a token")
		}
	}
	for _, header := range []string{"", "Bearer", "Bearer invalid", "Basic invalid"} {
		w := request(router, http.MethodGet, "/api/alpha/status-session", header)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("authorization %q: want 401, got %d", header, w.Code)
		}
	}
	if w := request(router, http.MethodGet, "/healthz", ""); w.Code != http.StatusOK {
		t.Fatalf("health check should be public: %d", w.Code)
	}
}
