package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

type starTarget struct {
	ChatID    string `json:"chatId"`
	MessageID string `json:"messageId"`
}

func (s *Server) starTarget(w http.ResponseWriter, r *http.Request, target starTarget) (history.Record, bool) {
	jid, err := types.ParseJID(target.ChatID)
	if err != nil || jid.User == "" || target.ChatID != jid.ToNonAD().String() || (jid.Server != types.GroupServer && jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) || !catalogValue(target.MessageID) {
		mediaError(w, errors.New("provide a direct or group chatId and a messageId"), "")
		return history.Record{}, false
	}
	store, close, ok := s.archive(w, r)
	if !ok {
		return history.Record{}, false
	}
	defer close()
	record, err := store.Get(r.Context(), target.ChatID, target.MessageID)
	if err != nil {
		historyError(w, err)
		return history.Record{}, false
	}
	if record.Deleted {
		historyError(w, history.ErrNotFound)
		return history.Record{}, false
	}
	return record, true
}

func (s *Server) messageStarState(w http.ResponseWriter, r *http.Request) {
	var q starTarget
	if err := readStructured(w, r, &q); err != nil {
		mediaError(w, err, "")
		return
	}
	record, ok := s.starTarget(w, r, q)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"starred": record.Starred}})
}

func (s *Server) starMessage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Idempotency-Key") == "" {
		commandError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "provide Idempotency-Key")
		return
	}
	client, ok := s.accountConnection(w, r)
	if !ok {
		return
	}
	var q struct {
		starTarget
		Star *bool `json:"star"`
	}
	if err := readStructured(w, r, &q); err != nil {
		mediaError(w, err, "")
		return
	}
	if q.Star == nil {
		mediaError(w, errors.New("provide explicit star boolean"), "")
		return
	}
	record, ok := s.starTarget(w, r, q.starTarget)
	if !ok {
		return
	}
	info := record.Info()
	sender := info.Chat // BuildStar encodes no participant as "0" when sender matches chat.
	if info.IsGroup && !info.IsFromMe {
		sender = info.Sender
		if sender.User == "" || (sender.Server != types.DefaultUserServer && sender.Server != types.HiddenUserServer) {
			commandError(w, 409, "SENDER_UNAVAILABLE", "original group sender unavailable")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := client.SendAppState(ctx, appstate.BuildStar(info.Chat, sender, info.ID, info.IsFromMe, *q.Star)); err != nil {
		writeJSON(w, 502, map[string]any{"status": "unconfirmed", "message": "favorite update was not confirmed; consult message-star-state before retrying"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"requested": *q.Star}})
}
