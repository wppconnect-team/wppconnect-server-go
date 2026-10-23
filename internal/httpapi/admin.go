package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
)

type adminSession struct {
	Expires time.Time
	CSRF    string
}
type loginAttempts struct {
	Until    time.Time
	Failures int
}

func equalSecret(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == r.Host
}
func (s *Server) adminCredentials(r *http.Request, csrf bool) bool {
	if s.cfg.AdminToken == "" {
		return false
	}
	if parts := strings.Fields(r.Header.Get("Authorization")); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && equalSecret(parts[1], s.cfg.AdminToken) {
		return true
	}
	cookie, err := r.Cookie("wppgo_admin")
	if err != nil || !sameOrigin(r) {
		return false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	value, ok := s.adminSessions[key]
	if !ok || time.Now().After(value.Expires) {
		delete(s.adminSessions, key)
		return false
	}
	if csrf && !equalSecret(value.CSRF, r.Header.Get("X-CSRF-Token")) {
		return false
	}
	return true
}
func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s.cfg.AdminToken == "" {
			writeJSON(w, 403, map[string]any{"status": "error", "message": "administration requires ADMIN_TOKEN"})
			return
		}
		if !s.adminCredentials(r, r.Method != "GET" && r.Method != "HEAD") {
			writeJSON(w, 401, map[string]any{"status": "error", "message": "administrator authentication required"})
			return
		}
		next(w, r)
	}
}
func (s *Server) adminRoutes(r chi.Router) {
	r.Post("/api/admin/login", s.adminLogin)
	r.Get("/api/admin/auth", s.adminOnly(s.adminAuth))
	r.Post("/api/admin/logout", s.adminOnly(s.adminLogout))
	r.Get("/api/admin/sessions", s.adminOnly(s.adminSessionsList))
	r.Post("/api/admin/sessions", s.adminOnly(s.adminCreateSession))
	r.Get("/api/admin/session-settings/{name}", s.adminOnly(s.sessionSettings))
	r.Put("/api/admin/session-settings/{name}", s.adminOnly(s.sessionSettings))
	r.Get("/metrics", s.adminOnly(s.metrics))
	r.Get("/api/admin/capabilities", s.adminOnly(s.capabilities))
}

type adminLoginRequest struct {
	Token string `json:"token"`
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg.AdminToken == "" {
		writeJSON(w, 403, map[string]any{"status": "error", "message": "administration requires ADMIN_TOKEN"})
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, 403, map[string]any{"status": "error", "message": "cross-origin login rejected"})
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := time.Now()
	s.adminMu.Lock()
	for key, value := range s.loginAttempts {
		if now.After(value.Until) {
			delete(s.loginAttempts, key)
		}
	}
	rate := s.loginAttempts[ip]
	blocked := rate.Failures >= 8 && now.Before(rate.Until)
	if len(s.loginAttempts) >= 1000 && rate.Until.IsZero() {
		blocked = true
	}
	if !blocked {
		if rate.Until.IsZero() || now.After(rate.Until) {
			rate = loginAttempts{Until: now.Add(time.Minute)}
		}
		rate.Failures++
		s.loginAttempts[ip] = rate
	}
	s.adminMu.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]any{"status": "error", "message": "too many login attempts"})
		return
	}
	var req adminLoginRequest
	if err = readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	if !equalSecret(req.Token, s.cfg.AdminToken) {
		writeJSON(w, 401, map[string]any{"status": "error", "message": "invalid administrator token"})
		return
	}
	token, csrf := randomToken(), randomToken()
	key := sha256.Sum256([]byte(token))
	value := adminSession{Expires: now.Add(8 * time.Hour), CSRF: csrf}
	s.adminMu.Lock()
	for k, v := range s.adminSessions {
		if now.After(v.Expires) {
			delete(s.adminSessions, k)
		}
	}
	if len(s.adminSessions) >= 128 {
		s.adminMu.Unlock()
		writeJSON(w, 429, map[string]any{"status": "error", "message": "administrator session limit reached"})
		return
	}
	s.adminSessions[key] = value
	delete(s.loginAttempts, ip)
	s.adminMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wppgo_admin", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	writeJSON(w, 200, map[string]any{"status": "success", "csrfToken": csrf, "expiresAt": value.Expires.Unix()})
}
func (s *Server) adminAuth(w http.ResponseWriter, r *http.Request) {
	csrf := ""
	if cookie, err := r.Cookie("wppgo_admin"); err == nil {
		key := sha256.Sum256([]byte(cookie.Value))
		s.adminMu.Lock()
		csrf = s.adminSessions[key].CSRF
		s.adminMu.Unlock()
	}
	writeJSON(w, 200, map[string]any{"status": "success", "authenticated": true, "csrfToken": csrf})
}
func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("wppgo_admin"); err == nil {
		key := sha256.Sum256([]byte(cookie.Value))
		s.adminMu.Lock()
		delete(s.adminSessions, key)
		s.adminMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "wppgo_admin", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, 200, map[string]any{"status": "success"})
}
func (s *Server) adminSessionsList(w http.ResponseWriter, r *http.Request) {
	if s.mgr == nil {
		writeJSON(w, 503, map[string]any{"status": "error", "message": "session manager unavailable"})
		return
	}
	names, err := s.mgr.Names()
	if err != nil {
		writeJSON(w, 502, map[string]any{"status": "error", "message": "session enumeration failed"})
		return
	}
	list := []map[string]any{}
	for _, name := range names {
		status := session.StatusClosed
		if h, ok := s.mgr.Get(name); ok {
			status = h.GetStatus()
		}
		settings, e := s.mgr.Settings(name)
		if e != nil {
			writeJSON(w, 502, map[string]any{"status": "error", "message": "session record unavailable"})
			return
		}
		list = append(list, map[string]any{"name": name, "status": status, "settings": map[string]any{"autoRead": settings.AutoRead, "alwaysOnline": settings.AlwaysOnline, "hasProxy": settings.ProxyURL != ""}})
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": list})
}

type adminCreateRequest struct {
	Name string `json:"name"`
}

func (s *Server) adminCreateSession(w http.ResponseWriter, r *http.Request) {
	var req adminCreateRequest
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := s.mgr.Start(ctx, req.Name); err != nil {
		lifecycleError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"status": "success", "response": map[string]any{"name": req.Name}})
}
func (s *Server) sessionSettings(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if r.Method == http.MethodPut {
		var patch settingsRequest
		if err := readStructured(w, r, &patch); err != nil {
			mediaError(w, err, "")
			return
		}
		value, err := s.mgr.Settings(name)
		if err != nil {
			lifecycleError(w, err)
			return
		}
		if patch.AutoRead != nil {
			value.AutoRead = *patch.AutoRead
		}
		if patch.AlwaysOnline != nil {
			value.AlwaysOnline = *patch.AlwaysOnline
		}
		if patch.ProxyURL != nil {
			value.ProxyURL = *patch.ProxyURL
		}
		if err := s.mgr.UpdateSettings(name, value); err != nil {
			mediaError(w, err, "")
			return
		}
	}
	value, err := s.mgr.Settings(name)
	if err != nil {
		lifecycleError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"autoRead": value.AutoRead, "alwaysOnline": value.AlwaysOnline, "hasProxy": value.ProxyURL != "", "proxyAppliesOnRestart": true}})
}

type settingsRequest struct {
	AutoRead     *bool   `json:"autoRead"`
	AlwaysOnline *bool   `json:"alwaysOnline"`
	ProxyURL     *string `json:"proxyUrl"`
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	connected, total := 0, 0
	if s.mgr != nil {
		for _, h := range s.mgr.List() {
			total++
			if h.GetStatus() == session.StatusConnected {
				connected++
			}
		}
	}
	fmt.Fprintf(w, "# TYPE wppconnect_sessions gauge\nwppconnect_sessions %d\n# TYPE wppconnect_connected_sessions gauge\nwppconnect_connected_sessions %d\n", total, connected)
	if s.events != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		stats, err := s.events.Log.Stats(ctx)
		if err == nil {
			for _, state := range []string{"pending", "delivered", "failed"} {
				fmt.Fprintf(w, "wppconnect_event_deliveries{state=%q} %d\n", state, stats[state])
			}
		}
		fmt.Fprintf(w, "wppconnect_rejected_events_total %d\n", s.events.Rejected())
	}
}
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"nativeMessaging": true, "messageActions": true, "history": true, "accountPrivacy": true, "labels": true, "phonePairing": true, "durableEvents": s.events != nil, "s3Media": s.mediaVault != nil, "websocket": s.events != nil, "manager": s.cfg.ManagerEnabled, "universalInteractiveDelivery": false}})
}
func (s *Server) pairPhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	code, err := s.mgr.PairPhone(ctx, cleanSession(chi.URLParam(r, "session")), req.Phone)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"code": code}})
}
