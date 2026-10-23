package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/messages"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func readStructured(w http.ResponseWriter, r *http.Request, value any) error {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		return errors.New("expected application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	return requireEOF(decoder)
}
func requireEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("expected one JSON object")
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *Server) actionRoutes(r chi.Router) {
	for _, name := range []string{"send-reaction", "edit-message", "delete-message", "mark-read", "mark-played"} {
		r.Post("/api/{session}/"+name, s.messageAction(name))
	}
	r.Post("/api/{session}/send-reply", s.sendText)
}

type textRequest struct {
	Phone    json.RawMessage `json:"phone"`
	Message  string          `json:"message"`
	IsGroup  bool            `json:"isGroup"`
	Mentions []string        `json:"mentionedList"`
	Quoted   string          `json:"quotedMessageId"`
}

func (s *Server) sendText(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	var req textRequest
	if err := readStructured(w, r, &req); err != nil {
		mediaError(w, err, "")
		return
	}
	to, err := destinations(req.Phone, req.IsGroup)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	if strings.HasSuffix(r.URL.Path, "/send-reply") && req.Quoted == "" {
		mediaError(w, errors.New("quotedMessageId is required"), "")
		return
	}
	mentions := make([]string, 0, len(req.Mentions))
	for _, value := range req.Mentions {
		j, e := directory.Parse(value, false, false)
		if e != nil || j.Server == types.GroupServer {
			mediaError(w, errors.New("invalid mentionedList"), "")
			return
		}
		mentions = append(mentions, j.String())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	prepared := make([]*waE2E.Message, len(to))
	var store *history.Store
	if req.Quoted != "" {
		var close func()
		store, close, ok = s.archive(w, r)
		if !ok {
			return
		}
		defer close()
	}
	for i, jid := range to {
		var quote *history.Record
		if store != nil {
			record, e := store.Get(ctx, jid.String(), req.Quoted)
			if e != nil {
				historyError(w, e)
				return
			}
			quote = &record
		}
		prepared[i], err = messages.Text(req.Message, mentions, quote)
		if err != nil {
			mediaError(w, err, "")
			return
		}
	}
	results := make([]map[string]any, 0, len(to))
	for i, jid := range to {
		resp, err := client.SendMessage(ctx, jid, prepared[i])
		if err != nil {
			sendError(w, err, results, jid.String())
			return
		}
		s.observeSent(cleanSession(chi.URLParam(r, "session")), jid, resp.ID, resp.Timestamp, prepared[i])
		results = append(results, map[string]any{"id": resp.ID, "to": jid.String(), "type": "chat"})
	}
	writeJSON(w, 201, map[string]any{"status": "success", "response": results, "mapper": "return"})
}

type actionRequest struct {
	Phone      string   `json:"phone"`
	IsGroup    bool     `json:"isGroup"`
	MessageID  string   `json:"messageId"`
	MessageIDs []string `json:"messageIds"`
	Message    string   `json:"message"`
	Reaction   string   `json:"reaction"`
}
type receiptClient interface {
	MarkRead(context.Context, []types.MessageID, time.Time, types.JID, types.JID, ...types.ReceiptType) error
}

func (s *Server) messageAction(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, _, ok := s.mediaConnection(w, r)
		if !ok || !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		var req actionRequest
		if err := readStructured(w, r, &req); err != nil {
			mediaError(w, err, "")
			return
		}
		jid, err := directory.Parse(req.Phone, req.IsGroup, false)
		if err != nil {
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
		if kind == "mark-read" || kind == "mark-played" {
			ids := req.MessageIDs
			if len(ids) == 0 && req.MessageID != "" {
				ids = []string{req.MessageID}
			}
			if len(ids) == 0 || len(ids) > 100 {
				mediaError(w, errors.New("provide 1..100 messageIds"), "")
				return
			}
			records := make([]history.Record, len(ids))
			for i, id := range ids {
				records[i], err = store.Get(ctx, jid.String(), id)
				if err != nil {
					historyError(w, err)
					return
				}
				if records[i].FromMe {
					mediaError(w, errors.New("receipts require received messages"), "")
					return
				}
				if kind == "mark-played" && records[i].Message.GetAudioMessage() == nil {
					mediaError(w, errors.New("played receipts require received audio messages"), "")
					return
				}
			}
			receipts, supported := client.(receiptClient)
			if !supported {
				writeJSON(w, 501, map[string]any{"status": "not_supported"})
				return
			}
			completed := []string{}
			receiptType := types.ReceiptTypeRead
			if kind == "mark-played" {
				receiptType = types.ReceiptTypePlayed
			}
			for _, record := range records {
				sender := record.Info().Sender
				if jid.Server != types.GroupServer {
					sender = types.EmptyJID
				}
				if err = receipts.MarkRead(ctx, []types.MessageID{record.ID}, time.Now(), jid, sender, receiptType); err != nil {
					writeJSON(w, 502, map[string]any{"status": "error", "message": "receipt failed", "completed": completed})
					return
				}
				completed = append(completed, record.ID)
			}
			if kind == "mark-read" {
				if err = store.MarkRead(ctx, jid.String(), completed); err != nil {
					writeJSON(w, 500, map[string]any{"status": "error", "message": "receipts sent; local read update failed", "completed": completed})
					return
				}
			}
			writeJSON(w, 200, map[string]any{"status": "success", "response": completed})
			return
		}
		record, err := store.Get(ctx, jid.String(), req.MessageID)
		if err != nil {
			historyError(w, err)
			return
		}
		value := req.Message
		if kind == "send-reaction" {
			value = req.Reaction
		}
		message, err := messages.Action(kind, record, value, time.Now())
		if err != nil {
			mediaError(w, err, "")
			return
		}
		resp, err := client.SendMessage(ctx, jid, message)
		if err != nil {
			sendError(w, err, []map[string]any{}, jid.String())
			return
		}
		if kind == "edit-message" {
			replacement, _ := messages.Text(req.Message, nil, nil)
			err = store.Change(ctx, jid.String(), record.ID, replacement)
		} else if kind == "delete-message" {
			err = store.Change(ctx, jid.String(), record.ID, nil)
		}
		if err != nil {
			writeJSON(w, 500, map[string]any{"status": "error", "message": "action sent; local history update failed", "completed": []map[string]any{{"id": resp.ID, "to": jid.String()}}})
			return
		}
		writeJSON(w, 201, map[string]any{"status": "success", "response": []map[string]any{{"id": resp.ID, "to": jid.String(), "type": kind}}, "mapper": "return"})
	}
}
