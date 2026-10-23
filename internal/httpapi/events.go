package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/eventbus"
)

func (s *Server) eventRoutes(r chi.Router) {
	r.Get("/api/{session}/events", s.listEvents)
	r.Post("/api/{session}/events/replay", s.replayEvents)
	r.Get("/api/{session}/events/ws", s.streamEvents)
}
func eventQuery(r *http.Request) (int64, int, error) {
	cursor, limit := int64(0), 50
	var err error
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if cursor < 0 || limit < 1 || limit > 200 {
		return 0, 0, errors.New("invalid cursor or limit")
	}
	return cursor, limit, nil
}
func (s *Server) eventAvailable(w http.ResponseWriter) bool {
	if s.events == nil {
		writeJSON(w, 503, map[string]any{"status": "error", "message": "durable event log unavailable"})
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	cursor, limit, err := eventQuery(r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := s.events.Log.List(ctx, cleanSession(chi.URLParam(r, "session")), cursor, limit)
	if err != nil {
		writeJSON(w, 502, map[string]any{"status": "error", "message": "event log read failed"})
		return
	}
	if len(list) > 0 {
		cursor = list[len(list)-1].Sequence
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": list, "nextCursor": cursor})
}

type replayRequest struct {
	EventID string `json:"eventId"`
}

func (s *Server) replayEvents(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	var req replayRequest
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	if len(req.EventID) > 64 || req.EventID == "" {
		mediaError(w, errors.New("eventId is required"), "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err := s.events.Log.Replay(ctx, cleanSession(chi.URLParam(r, "session")), req.EventID, s.events.Targets())
	if err != nil {
		code := 409
		if errors.Is(err, eventbus.ErrMissing) {
			code = 404
		}
		writeJSON(w, code, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, 202, map[string]any{"status": "success", "response": map[string]any{"queued": true, "targets": s.events.Targets()}})
}
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		writeJSON(w, 429, map[string]any{"status": "error", "message": "event stream capacity reached"})
		return
	}
	cursor, _, err := eventQuery(r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	ctx := connection.CloseRead(r.Context())
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := s.events.Log.List(ctx, cleanSession(chi.URLParam(r, "session")), cursor, 50)
		if err != nil {
			connection.Close(websocket.StatusInternalError, "event log unavailable")
			return
		}
		for _, event := range list {
			data, err := json.Marshal(event)
			if err != nil {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = connection.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
			cursor = event.Sequence
		}
		select {
		case <-s.events.Done():
			connection.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
