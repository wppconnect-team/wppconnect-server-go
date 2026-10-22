// Package httpapi exposes the HTTP routes, kept compatible with the Node
// wppconnect-server contract (same paths, same payload field names) so existing
// clients can point at the Go server with minimal changes.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"
	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// Server wires the routes to the session manager.
type Server struct {
	cfg              config.Config
	mgr              *session.Manager
	mediaSession     func(string) (media.Client, *media.Store, bool)
	mediaSlots       chan struct{}
	groupSession     func(string) (groups.Client, bool)
	directorySession func(string) (directory.Client, *directory.State, bool)
}

// NewRouter builds the chi router with the compatible endpoints.
func NewRouter(cfg config.Config, mgr *session.Manager) http.Handler {
	s := &Server{cfg: cfg, mgr: mgr}
	s.mediaSession = func(name string) (media.Client, *media.Store, bool) {
		h, ok := mgr.Get(name)
		if !ok || h.GetStatus() != session.StatusConnected {
			return nil, nil, false
		}
		return h.Client, h.Media, true
	}
	s.groupSession = func(name string) (groups.Client, bool) {
		h, ok := mgr.Get(name)
		if !ok || h.GetStatus() != session.StatusConnected {
			return nil, false
		}
		return h.Client, true
	}
	s.directorySession = func(name string) (directory.Client, *directory.State, bool) {
		h, ok := mgr.Get(name)
		if !ok || h.GetStatus() != session.StatusConnected {
			return nil, nil, false
		}
		return directory.SDK{Client: h.Client}, h.Directory, true
	}
	return s.router()
}

func (s *Server) router() http.Handler {
	s.mediaSlots = make(chan struct{}, 4)
	r := chi.NewRouter()
	r.Get("/openapi.json", s.openAPI)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if s.mgr != nil && s.mgr.IsStopping() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.mgr != nil && !s.mgr.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/api/dashboard/stats", s.dashboardStats)
	r.Post("/api/{session}/{secretkey}/generate-token", s.generateToken)

	r.Group(func(protected chi.Router) {
		protected.Use(s.auth)
		// Mirrors POST /api/:session/start-session
		protected.Post("/api/{session}/start-session", s.startSession)
		// Mirrors GET /api/:session/status-session
		protected.Get("/api/{session}/status-session", s.statusSession)
		protected.Get("/api/{session}/qrcode-session", s.qrcodeSession)
		// Mirrors POST /api/:session/send-message
		protected.Post("/api/{session}/send-message", s.sendMessage)
		// Mirrors POST /api/:session/close-session and logout-session
		protected.Post("/api/{session}/close-session", s.closeSession)
		protected.Post("/api/{session}/logout-session", s.logoutSession)
		// Mirrors POST /api/:session/send-image (base64)
		protected.Post("/api/{session}/send-image", s.sendImage)
		protected.Post("/api/{session}/send-file", s.sendFile)
		protected.Post("/api/{session}/send-file-base64", s.sendFile)
		protected.Post("/api/{session}/send-voice-base64", s.sendVoice)
		protected.Post("/api/{session}/send-voice", s.sendVoice)
		protected.Post("/api/{session}/send-audio-base64", s.sendAudio)
		protected.Post("/api/{session}/download-media", s.downloadMedia)
		protected.Get("/api/{session}/get-media-by-message/{messageId}", s.downloadMedia)
		// Mirrors POST /api/:session/send-seen
		protected.Post("/api/{session}/send-seen", s.sendSeen)
		// Mirrors GET /api/:session/check-number-status/:phone
		protected.Get("/api/{session}/check-number-status/{phone}", s.checkNumber)
		s.groupRoutes(protected)
		s.directoryRoutes(protected)
	})

	return r
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionParam := chi.URLParam(r, "session")
		sessionName := cleanSession(sessionParam)
		token := ""
		if parts := strings.SplitN(sessionParam, ":", 2); len(parts) == 2 {
			token = parts[1]
		} else if parts := strings.Fields(r.Header.Get("Authorization")); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = parts[1]
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"message": "Token is not present. Check your header and try again",
			})
			return
		}
		if err := auth.Verify(sessionName, s.cfg.SecretKey, token); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "Check that the Session and Token are correct",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// generateToken mirrors the Node server's secret-key bootstrap endpoint.
func (s *Server) generateToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if subtle.ConstantTimeCompare([]byte(chi.URLParam(r, "secretkey")), []byte(s.cfg.SecretKey)) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"response": false, "message": "The SECRET_KEY is incorrect",
		})
		return
	}
	name := chi.URLParam(r, "session")
	if name == "" || strings.Contains(name, ":") || len(name+s.cfg.SecretKey) > 72 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"status": "error", "message": "session must not contain ':' and session + SECRET_KEY must fit in 72 bytes",
		})
		return
	}
	token, err := auth.Generate(name, s.cfg.SecretKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": "could not generate token"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "success", "session": name, "token": token, "full": name + ":" + token,
	})
}

func cleanSession(name string) string {
	if idx := strings.Index(name, ":"); idx >= 0 {
		return name[:idx]
	}
	return name
}

// connected returns the live handle for a connected session, or writes the
// standard "disconnected" response and returns false.
func (s *Server) connected(w http.ResponseWriter, name string) (*session.Handle, bool) {
	h, ok := s.mgr.Get(cleanSession(name))
	if !ok || h.GetStatus() != session.StatusConnected {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "disconnected", "session": cleanSession(name),
		})
		return nil, false
	}
	return h, true
}

func userJID(phone string) types.JID {
	phone = strings.Split(phone, "@")[0]
	return types.NewJID(phone, types.DefaultUserServer)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, err := s.mgr.Start(r.Context(), name)
	if err != nil {
		lifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.GetStatus()), "session": name, "qrcode": h.GetQRCode(),
	})
}

func (s *Server) statusSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "CLOSED", "session": name})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.GetStatus()), "session": name, "urlcode": h.GetQRCode(),
	})
}

func (s *Server) qrcodeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	qrCode := ""
	if ok {
		qrCode = h.GetQRCode()
	}
	if !ok || qrCode == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "QRCODE_NOT_AVAILABLE",
			"message": "QRCode is not available...",
		})
		return
	}
	png, err := qrcode.Encode(qrCode, qrcode.Medium, 500)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

type sendMessageReq struct {
	Phone   any    `json:"phone"` // string or []string, like the Node server
	Message string `json:"message"`
	IsGroup bool   `json:"isGroup"`
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok || h.GetStatus() != session.StatusConnected {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "disconnected", "session": name,
		})
		return
	}

	var req sendMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error"})
		return
	}

	for _, phone := range toList(req.Phone) {
		jid := types.NewJID(phone, types.DefaultUserServer)
		if req.IsGroup {
			jid = types.NewJID(phone, types.GroupServer)
		}
		resp, err := h.Client.SendMessage(r.Context(), jid, &waE2E.Message{
			Conversation: &req.Message,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"status": "error", "message": err.Error(),
			})
			return
		}
		s.observeSent(name, jid, resp.ID, resp.Timestamp)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	if err := s.mgr.Close(name); err != nil {
		lifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "session": name})
}

func (s *Server) logoutSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.mgr.Logout(ctx, name); err != nil {
		lifecycleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "session": name})
}

func lifecycleError(w http.ResponseWriter, err error) {
	code, message := http.StatusBadGateway, "session operation failed"
	switch {
	case errors.Is(err, session.ErrInvalidName):
		code, message = 400, err.Error()
	case errors.Is(err, session.ErrStopping):
		code, message = 503, "server is shutting down"
	case errors.Is(err, session.ErrDisconnected):
		code, message = 409, err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		code, message = 504, "session operation timed out"
	case errors.Is(err, context.Canceled):
		code, message = 503, "session operation canceled"
	}
	writeJSON(w, code, map[string]any{"status": "error", "message": message})
}

type sendSeenReq struct {
	Phone string `json:"phone"`
}

// sendSeen marks a chat as read. Mirrors POST send-seen.
func (s *Server) sendSeen(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req sendSeenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error"})
		return
	}
	// MarkRead with no specific message IDs marks the chat as seen.
	err := h.Client.MarkRead(
		context.Background(),
		nil,
		time.Now(),
		userJID(req.Phone),
		types.EmptyJID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// checkNumber reports whether a phone is registered on WhatsApp.
// Mirrors GET check-number-status/:phone.
func (s *Server) checkNumber(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	phone := chi.URLParam(r, "phone")
	resp, err := h.Client.IsOnWhatsApp(
		context.Background(),
		[]string{"+" + strings.TrimPrefix(phone, "+")},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	numberExists := false
	var wid string
	if len(resp) > 0 {
		numberExists = resp[0].IsIn
		wid = resp[0].JID.String()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"response": map[string]any{"numberExists": numberExists, "id": wid},
	})
}

func (s *Server) dashboardStats(w http.ResponseWriter, _ *http.Request) {
	handles := s.mgr.List()
	sessions := make([]map[string]any, 0, len(handles))
	connected := 0
	for _, h := range handles {
		status := h.GetStatus()
		if status == session.StatusConnected {
			connected++
		}
		sessions = append(sessions, map[string]any{
			"session":   h.Name,
			"origin":    "unknown",
			"runtime":   "wppconnect-server-go",
			"provider":  "whatsmeow",
			"status":    status,
			"connected": status == session.StatusConnected,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
		"overview": map[string]any{
			"runtime":       "wppconnect-server-go",
			"sessions":      len(sessions),
			"totalSessions": len(sessions),
			"connected":     connected,
			"disconnected":  len(sessions) - connected,
			"byProvider":    map[string]int{"whatsmeow": len(sessions)},
		},
		"sessions": sessions,
	})
}

func (s *Server) notSupported(capability string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"status":     "not_supported",
			"provider":   "whatsmeow",
			"capability": capability,
			"message":    "Capability is not supported by wppconnect-server-go yet.",
		})
	}
}

func toList(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
