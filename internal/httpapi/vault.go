package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
)

type archiveRequest struct {
	MessageID string `json:"messageId"`
	ChatID    string `json:"chatId"`
}

func (s *Server) vaultRoutes(r chi.Router) {
	r.Post("/api/{session}/archive-media", s.archiveMedia)
	r.Get("/api/{session}/stored-media/{messageId}", s.storedMedia)
	r.Delete("/api/{session}/stored-media/{messageId}", s.storedMedia)
}
func (s *Server) vaultAvailable(w http.ResponseWriter) bool {
	if s.mediaVault == nil {
		writeJSON(w, 503, map[string]any{"status": "error", "message": "S3 media storage is not configured"})
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (s *Server) archiveMedia(w http.ResponseWriter, r *http.Request) {
	if !s.vaultAvailable(w) {
		return
	}
	client, _, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	var req archiveRequest
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	if !history.ValidChat(req.ChatID) || req.MessageID == "" {
		mediaError(w, errors.New("chatId and messageId are required"), "")
		return
	}
	store, close, ok := s.archive(w, r)
	if !ok {
		return
	}
	defer close()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	record, err := store.Get(ctx, req.ChatID, req.MessageID)
	if err != nil {
		historyError(w, err)
		return
	}
	attachment, ok := media.Descriptor(record.Message)
	if !ok || record.Deleted {
		mediaError(w, errors.New("message has no retained media descriptor"), "")
		return
	}
	data, err := media.Download(ctx, client, attachment)
	if err != nil {
		mediaError(w, err, "media download failed")
		return
	}
	metadata, err := s.mediaVault.Archive(ctx, store, cleanSession(chi.URLParam(r, "session")), record.Chat, record.ID, attachment.Mimetype, data)
	if err != nil {
		writeJSON(w, 502, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"status": "success", "response": metadata})
}
func (s *Server) storedMedia(w http.ResponseWriter, r *http.Request) {
	if !s.vaultAvailable(w) || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	chat, id, name := r.URL.Query().Get("chatId"), chi.URLParam(r, "messageId"), cleanSession(chi.URLParam(r, "session"))
	if !history.ValidChat(chat) || id == "" {
		mediaError(w, errors.New("valid chatId and messageId are required"), "")
		return
	}
	store, close, ok := s.archive(w, r)
	if !ok {
		return
	}
	defer close()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if r.Method == http.MethodDelete {
		metadata, err := store.Media(ctx, chat, id)
		if err != nil {
			historyError(w, err)
			return
		}
		if err = s.mediaVault.Delete(ctx, store, name, metadata); err != nil {
			writeJSON(w, 502, map[string]any{"status": "error", "message": "stored media deletion failed"})
			return
		}
		writeJSON(w, 200, map[string]any{"status": "success"})
		return
	}
	metadata, data, err := s.mediaVault.Download(ctx, store, name, chat, id)
	if err != nil {
		if errors.Is(err, history.ErrNotFound) {
			historyError(w, err)
		} else {
			writeJSON(w, 502, map[string]any{"status": "error", "message": "stored media download failed"})
		}
		return
	}
	writeJSON(w, 200, map[string]any{"base64": base64.StdEncoding.EncodeToString(data), "mimetype": metadata.MIME, "expiresAt": metadata.Expires})
}
