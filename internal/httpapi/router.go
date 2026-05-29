// Package httpapi exposes the HTTP routes, kept compatible with the Node
// wppconnect-server contract (same paths, same payload field names) so existing
// clients can point at the Go server with minimal changes.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
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

	return r
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
