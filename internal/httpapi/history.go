package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

func (s *Server) historyRoutes(r chi.Router) {
	r.Get("/api/{session}/messages", s.messageHistory)
	r.Get("/api/{session}/messages/{messageId}", s.messageHistory)
	r.Get("/api/{session}/message-receipts/{messageId}", s.messageHistory)
	r.Get("/api/{session}/poll-results/{messageId}", s.messageHistory)
	r.Delete("/api/{session}/messages", s.pruneHistory)
}
func (s *Server) archive(w http.ResponseWriter, r *http.Request) (*history.Store, func(), bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.historySession == nil {
		writeJSON(w, 501, map[string]any{"status": "not_supported", "capability": "message_history", "message": "message history is unavailable in this provider"})
		return nil, func() {}, false
	}
	store, close, err := s.historySession(cleanSession(chi.URLParam(r, "session")))
	if err != nil {
		code := 502
		if errors.Is(err, os.ErrNotExist) {
			code = 404
		}
		writeJSON(w, code, map[string]any{"status": "error", "message": "session history is unavailable"})
		return nil, func() {}, false
	}
	return store, close, true
}
func historyError(w http.ResponseWriter, err error) {
	code := 400
	if errors.Is(err, history.ErrNotFound) {
		code = 404
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = 504
	}
	writeJSON(w, code, map[string]any{"status": "error", "message": err.Error()})
}
func (s *Server) messageHistory(w http.ResponseWriter, r *http.Request) {
	store, close, ok := s.archive(w, r)
	if !ok {
		return
	}
	defer close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	chat := r.URL.Query().Get("chatId")
	if phone := chi.URLParam(r, "phone"); phone != "" {
		jid, err := directoryJID(r, "phone")
		if err != nil {
			historyError(w, err)
			return
		}
		chat = jid.String()
	}
	id := chi.URLParam(r, "messageId")
	if id != "" {
		var record history.Record
		var err error
		if chat != "" {
			record, err = store.Get(ctx, chat, id)
		} else {
			record, err = store.Find(ctx, id)
		}
		if err != nil {
			historyError(w, err)
			return
		}
		var response any = record
		if strings.Contains(r.URL.Path, "/poll-results/") {
			response, err = store.PollResults(ctx, record.Chat, id)
		}
		if strings.Contains(r.URL.Path, "/message-receipts/") {
			response, err = store.Receipts(ctx, record.Chat, id)
		}
		if err != nil {
			historyError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": response, "mapper": "return"})
		return
	}
	limit := 50
	cursor := int64(0)
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			historyError(w, errors.New("invalid limit"))
			return
		}
	}
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			historyError(w, errors.New("invalid cursor"))
			return
		}
	}
	unread := strings.Contains(r.URL.Path, "unread") || strings.Contains(r.URL.Path, "all-new-messages")
	page, err := store.List(ctx, history.Query{Chat: chat, Before: cursor, Limit: limit, Unread: unread})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": page.Messages, "nextCursor": page.NextCursor, "isPartial": true, "order": "ingestion-desc", "mapper": "return"})
}
func (s *Server) pruneHistory(w http.ResponseWriter, r *http.Request) {
	store, close, ok := s.archive(w, r)
	if !ok {
		return
	}
	defer close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	chat := r.URL.Query().Get("chatId")
	if chat == "" && r.URL.Query().Get("all") != "true" {
		historyError(w, errors.New("chatId is required, or set all=true to purge this session's retained history"))
		return
	}
	if err := store.Purge(ctx, chat); err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success"})
}
