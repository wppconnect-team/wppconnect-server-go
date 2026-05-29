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
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
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

	// Mirrors POST /api/:session/start-session
	r.Post("/api/{session}/start-session", s.startSession)
	// Mirrors GET /api/:session/status-session
	r.Get("/api/{session}/status-session", s.statusSession)
	// Mirrors POST /api/:session/send-message
	r.Post("/api/{session}/send-message", s.sendMessage)
	// Mirrors POST /api/:session/close-session
	r.Post("/api/{session}/close-session", s.closeSession)
	// Mirrors POST /api/:session/send-image (base64)
	r.Post("/api/{session}/send-image", s.sendImage)
	// Mirrors POST /api/:session/send-seen
	r.Post("/api/{session}/send-seen", s.sendSeen)
	// Mirrors GET /api/:session/check-number-status/:phone
	r.Get("/api/{session}/check-number-status/{phone}", s.checkNumber)
	// Mirrors GET /api/:session/all-groups
	r.Get("/api/{session}/all-groups", s.allGroups)

	return r
}

// connected returns the live handle for a connected session, or writes the
// standard "disconnected" response and returns false.
func (s *Server) connected(w http.ResponseWriter, name string) (*session.Handle, bool) {
	h, ok := s.mgr.Get(name)
	if !ok || h.Status != session.StatusConnected {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "disconnected", "session": name,
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
	name := chi.URLParam(r, "session")
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
	name := chi.URLParam(r, "session")
	h, ok := s.mgr.Get(name)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "CLOSED", "session": name})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.Status), "session": name, "urlcode": h.QRCode,
	})
}

type sendMessageReq struct {
	Phone   any    `json:"phone"` // string or []string, like the Node server
	Message string `json:"message"`
	IsGroup bool   `json:"isGroup"`
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "session")
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
	name := chi.URLParam(r, "session")
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
		"status":       "success",
		"response":     map[string]any{"numberExists": numberExists, "id": wid},
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
