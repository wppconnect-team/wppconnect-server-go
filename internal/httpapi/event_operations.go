package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/eventbus"
	"net/http"
	"slices"
	"time"
)

func (s *Server) eventDeliveries(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	cursor, limit, err := eventQuery(r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	target, state := r.URL.Query().Get("target"), r.URL.Query().Get("state")
	if state == "" {
		state = "failed"
	}
	if !slices.Contains([]string{"webhook", "nats", "rabbitmq", "redis-streams"}, target) || !slices.Contains([]string{"failed", "pending", "delivered"}, state) {
		mediaError(w, errors.New("target must be webhook/nats/rabbitmq/redis-streams and state failed/pending/delivered"), "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	deliveries, err := s.events.Log.Deliveries(ctx, cleanSession(chi.URLParam(r, "session")), target, state, cursor, limit)
	if err != nil {
		writeJSON(w, 503, map[string]any{"status": "error", "message": "delivery log unavailable"})
		return
	}
	if len(deliveries) > 0 {
		cursor = deliveries[len(deliveries)-1].Sequence
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": deliveries, "nextCursor": cursor})
}

type failedReplayRequest struct {
	EventID string `json:"eventId"`
	Target  string `json:"target"`
}

func (s *Server) retryFailedEvent(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	var req failedReplayRequest
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	if req.EventID == "" || len(req.EventID) > 64 || !slices.Contains(s.events.Targets(), req.Target) {
		mediaError(w, errors.New("eventId and an enabled target are required"), "")
		return
	}
	actor := "session-token"
	if s.adminCredentials(r, true) {
		actor = "administrator"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := s.events.Log.ReplayFailed(ctx, cleanSession(chi.URLParam(r, "session")), req.EventID, req.Target, actor)
	if err != nil {
		code, message := 503, "delivery log unavailable"
		if errors.Is(err, eventbus.ErrMissing) {
			code, message = 404, "event not found in this session"
		} else if errors.Is(err, eventbus.ErrNotFailed) {
			code, message = 409, "delivery must be failed and not currently leased"
		}
		writeJSON(w, code, map[string]any{"status": "error", "message": message})
		return
	}
	writeJSON(w, 202, map[string]any{"status": "success", "response": map[string]any{"queued": true, "eventId": req.EventID, "target": req.Target}})
}
func (s *Server) eventReplayAudit(w http.ResponseWriter, r *http.Request) {
	if !s.eventAvailable(w) {
		return
	}
	cursor, limit, err := eventQuery(r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	records, err := s.events.Log.ReplayAudit(ctx, cleanSession(chi.URLParam(r, "session")), cursor, limit)
	if err != nil {
		writeJSON(w, 503, map[string]any{"status": "error", "message": "replay audit unavailable"})
		return
	}
	if len(records) > 0 {
		cursor = records[len(records)-1].Cursor
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": records, "nextCursor": cursor})
}
