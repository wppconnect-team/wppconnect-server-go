package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/messages"
	"go.mau.fi/whatsmeow/appstate"
)

type chatMutationRequest struct {
	Phone     string `json:"phone"`
	IsGroup   bool   `json:"isGroup"`
	Value     *bool  `json:"value"`
	Duration  int    `json:"duration"`
	LabelID   string `json:"labelId"`
	Name      string `json:"name"`
	Color     int32  `json:"color"`
	MessageID string `json:"messageId"`
	Deleted   bool   `json:"deleted"`
}

func (s *Server) mutationRoutes(r chi.Router) {
	for _, name := range []string{"archive-chat", "pin-chat", "mute-chat", "mark-chat-unread", "delete-chat", "edit-label", "label-chat", "label-message"} {
		r.Post("/api/{session}/"+name, s.chatMutation(name))
	}
	r.Get("/api/{session}/labels", s.labels)
	r.Get("/api/{session}/chat-labels/{phone}", s.labels)
}
func (s *Server) labels(w http.ResponseWriter, r *http.Request) {
	store, close, ok := s.archive(w, r)
	if !ok {
		return
	}
	defer close()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var out any
	var err error
	if chi.URLParam(r, "phone") != "" {
		jid, e := directoryJID(r, "phone")
		if e != nil {
			mediaError(w, e, "")
			return
		}
		out, err = store.ChatLabels(ctx, jid.String(), r.URL.Query().Get("messageId"))
	} else {
		out, err = store.Labels(ctx)
	}
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": out})
}
func (s *Server) chatMutation(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.accountConnection(w, r)
		if !ok {
			return
		}
		var req chatMutationRequest
		if err := readStructured(w, r, &req); err != nil {
			mediaError(w, err, "")
			return
		}
		store, close, ok := s.archive(w, r)
		if !ok {
			return
		}
		defer close()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var patch appstate.PatchInfo
		var after func() error
		if kind == "edit-label" {
			id, err := strconv.ParseUint(req.LabelID, 10, 16)
			if err != nil || id == 0 || req.Color < 0 || req.Color > 19 {
				mediaError(w, errors.New("labelId must be 1..65535 and color 0..19"), "")
				return
			}
			if !req.Deleted {
				if err = validateLabelName(req.Name); err != nil {
					mediaError(w, err, "")
					return
				}
			}
			patch = appstate.BuildLabelEdit(req.LabelID, req.Name, req.Color, req.Deleted)
			after = func() error {
				return store.SetLabel(ctx, history.Label{ID: req.LabelID, Name: req.Name, Color: req.Color, Deleted: req.Deleted})
			}
		} else {
			jid, err := directory.Parse(req.Phone, req.IsGroup, false)
			if err != nil {
				mediaError(w, err, "")
				return
			}
			value := false
			if req.Value != nil {
				value = *req.Value
			} else if kind != "delete-chat" {
				mediaError(w, errors.New("value boolean is required"), "")
				return
			}
			now := time.Now()
			switch kind {
			case "archive-chat":
				patch = appstate.BuildArchive(jid, value, time.Time{}, nil)
			case "pin-chat":
				patch = appstate.BuildPin(jid, value)
			case "mute-chat":
				if req.Duration < 0 || req.Duration > 365*86400 {
					mediaError(w, errors.New("duration must be 0..31536000 seconds (zero means forever)"), "")
					return
				}
				patch = appstate.BuildMute(jid, value, time.Duration(req.Duration)*time.Second)
			case "mark-chat-unread":
				patch = appstate.BuildMarkChatAsRead(jid, !value, time.Time{}, nil)
			case "delete-chat":
				page, err := store.List(ctx, history.Query{Chat: jid.String(), Limit: 1})
				if err != nil || len(page.Messages) == 0 {
					historyError(w, history.ErrNotFound)
					return
				}
				last := page.Messages[0]
				patch = appstate.BuildDeleteChat(jid, time.Unix(last.Timestamp, 0), messages.MessageKey(last), false)
			case "label-chat", "label-message":
				if kind == "label-chat" && req.MessageID != "" {
					mediaError(w, errors.New("messageId is only accepted for label-message"), "")
					return
				}
				labels, err := store.Labels(ctx)
				if err != nil {
					historyError(w, err)
					return
				}
				found := false
				for _, v := range labels {
					if v.ID == req.LabelID {
						found = true
					}
				}
				if !found {
					historyError(w, errors.New("label does not exist in this session"))
					return
				}
				if kind == "label-message" {
					if _, err = store.Get(ctx, jid.String(), req.MessageID); err != nil {
						historyError(w, err)
						return
					}
					patch = appstate.BuildLabelMessage(jid, req.LabelID, req.MessageID, value)
				} else {
					patch = appstate.BuildLabelChat(jid, req.LabelID, value)
				}
				after = func() error { return store.Associate(ctx, req.LabelID, jid.String(), req.MessageID, value, 0) }
			}
			if after == nil {
				after = func() error {
					if kind == "delete-chat" {
						if err := store.Purge(ctx, jid.String()); err != nil {
							return err
						}
					}
					if s.directorySession == nil {
						return nil
					}
					_, state, ok := s.directorySession(cleanSession(chi.URLParam(r, "session")))
					if !ok {
						return nil
					}
					return state.Chats.Update(ctx, jid, func(c *directory.Chat) {
						switch kind {
						case "archive-chat":
							if c.ArchiveAt > now.Unix() {
								return
							}
							c.Archived = value
							c.ArchiveAt = now.Unix()
							if value {
								c.Pinned = false
								c.PinAt = now.Unix()
							}
						case "pin-chat":
							if c.PinAt > now.Unix() {
								return
							}
							c.Pinned = value
							c.PinAt = now.Unix()
						case "mute-chat":
							if c.MuteAt > now.Unix() {
								return
							}
							c.Muted, c.MuteAt, c.MuteUntil = value, now.Unix(), 0
							if value && req.Duration > 0 {
								c.MuteUntil = now.Add(time.Duration(req.Duration) * time.Second).Unix()
							}
						case "mark-chat-unread":
							if c.ReadAt > now.Unix() {
								return
							}
							c.ReadAt = now.Unix()
							if value {
								c.Unread = 1
							} else {
								c.Unread = 0
							}
							c.UnreadKnown = true
						case "delete-chat":
							c.DeletedAt = now.Unix()
						}
					})
				}
			}
		}
		if err := client.SendAppState(ctx, patch); err != nil {
			directoryError(w, err)
			return
		}
		if err := after(); err != nil {
			writeJSON(w, 500, map[string]any{"status": "error", "message": "remote mutation accepted; local index update failed", "completed": true})
			return
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"acknowledged": true}})
	}
}
func validateLabelName(name string) error {
	return account.Text(name, 100)
}
