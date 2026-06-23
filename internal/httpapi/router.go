// Package httpapi exposes the HTTP routes, kept compatible with the Node
// wppconnect-server contract (same paths, same payload field names) so existing
// clients can point at the Go server with minimal changes.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/crypto/bcrypt"
)

// Server wires the routes to the session manager.
type Server struct {
	cfg config.Config
	mgr *session.Manager
}

// NewRouter builds the chi router with the compatible endpoints.
func NewRouter(cfg config.Config, mgr *session.Manager) http.Handler {
	s := &Server{cfg: cfg, mgr: mgr}
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/api/dashboard/stats", s.dashboardStats)

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
		protected.Post("/api/{session}/logout-session", s.closeSession)
		// Mirrors POST /api/:session/send-image (base64)
		protected.Post("/api/{session}/send-image", s.sendImage)
		// Mirrors POST /api/:session/send-seen
		protected.Post("/api/{session}/send-seen", s.sendSeen)
		// Mirrors GET /api/:session/check-number-status/:phone
		protected.Get("/api/{session}/check-number-status/{phone}", s.checkNumber)
		// Mirrors GET /api/:session/all-groups
		protected.Get("/api/{session}/all-groups", s.allGroups)
		protected.Get("/api/{session}/group-members/{groupId}", s.notSupported("groups"))
		protected.Post("/api/{session}/create-group", s.notSupported("groups"))
	})

	return r
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionName := cleanSession(chi.URLParam(r, "session"))
		auth := r.Header.Get("Authorization")
		token := ""
		if parts := strings.SplitN(auth, " ", 2); len(parts) == 2 {
			token = parts[1]
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"message": "Token is not present. Check your header and try again",
			})
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(token), []byte(sessionName+s.cfg.SecretKey)); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "Check that the Session and Token are correct",
			})
			return
		}
		next.ServeHTTP(w, r)
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
	if !ok || h.Status != session.StatusConnected {
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
	h, err := s.mgr.Start(context.Background(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.Status), "session": name, "qrcode": h.QRCode,
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
		"status": string(h.Status), "session": name, "urlcode": h.QRCode,
	})
}

func (s *Server) qrcodeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok || h.QRCode == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "QRCODE_NOT_AVAILABLE",
			"message": "QRCode is not available...",
		})
		return
	}
	png, err := qrcode.Encode(h.QRCode, qrcode.Medium, 500)
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
	if !ok || h.Status != session.StatusConnected {
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
		_, err := h.Client.SendMessage(context.Background(), jid, &waE2E.Message{
			Conversation: &req.Message,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"status": "error", "message": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	s.mgr.Close(name)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "session": name})
}

type sendImageReq struct {
	Phone   string `json:"phone"`
	Base64  string `json:"base64"`
	Caption string `json:"caption"`
	IsGroup bool   `json:"isGroup"`
}

// sendImage uploads a base64 image and sends it. Mirrors POST send-image (the
// base64 variant of the Node server).
func (s *Server) sendImage(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req sendImageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Base64 == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error"})
		return
	}

	// Accept raw base64 or a data URL (data:image/png;base64,....).
	payload := req.Base64
	if i := strings.Index(payload, ","); strings.HasPrefix(payload, "data:") && i >= 0 {
		payload = payload[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid base64"})
		return
	}

	ctx := context.Background()
	uploaded, err := h.Client.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}

	jid := userJID(req.Phone)
	if req.IsGroup {
		jid = types.NewJID(strings.Split(req.Phone, "@")[0], types.GroupServer)
	}
	mimetype := "image/jpeg"
	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:       &req.Caption,
		Mimetype:      &mimetype,
		URL:           &uploaded.URL,
		DirectPath:    &uploaded.DirectPath,
		MediaKey:      uploaded.MediaKey,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    &uploaded.FileLength,
	}}
	if _, err := h.Client.SendMessage(ctx, jid, msg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
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

// allGroups lists the joined groups. Mirrors GET all-groups.
func (s *Server) allGroups(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	groups, err := h.Client.GetJoinedGroups(context.Background())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, map[string]any{
			"id":           g.JID.String(),
			"name":         g.Name,
			"participants": len(g.Participants),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": out})
}

func (s *Server) dashboardStats(w http.ResponseWriter, _ *http.Request) {
	handles := s.mgr.List()
	sessions := make([]map[string]any, 0, len(handles))
	connected := 0
	for _, h := range handles {
		if h.Status == session.StatusConnected {
			connected++
		}
		sessions = append(sessions, map[string]any{
			"session":   h.Name,
			"origin":    "unknown",
			"runtime":   "wppconnect-server-go",
			"provider":  "whatsmeow",
			"status":    h.Status,
			"connected": h.Status == session.StatusConnected,
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
