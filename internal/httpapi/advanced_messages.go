package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/messages"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

type advancedRequest struct {
	Phone        json.RawMessage    `json:"phone"`
	IsGroup      bool               `json:"isGroup"`
	IsLid        bool               `json:"isLid"`
	IsNewsletter bool               `json:"isNewsletter"`
	Contacts     []messages.Contact `json:"contacts"`
	ContactsID   []string           `json:"contactsId"`
	Name         string             `json:"name"`
	Lat          json.RawMessage    `json:"lat"`
	Lng          json.RawMessage    `json:"lng"`
	Title        string             `json:"title"`
	Address      string             `json:"address"`
	Choices      []string           `json:"choices"`
	Options      *struct {
		SelectableCount *int `json:"selectableCount"`
	} `json:"options"`
}

func (s *Server) advancedRoutes(r chi.Router) {
	r.Post("/api/{session}/send-video", s.sendVideo)
	r.Post("/api/{session}/send-video-base64", s.sendVideo)
	r.Post("/api/{session}/send-sticker", s.sendSticker)
	r.Post("/api/{session}/send-sticker-gif", s.sendSticker)
	r.Post("/api/{session}/contact-vcard", s.sendAdvanced)
	r.Post("/api/{session}/send-contact", s.sendAdvanced)
	r.Post("/api/{session}/send-location", s.sendAdvanced)
	r.Post("/api/{session}/send-poll-message", s.sendAdvanced)
}

// sendError preserves accepted IDs when a batch stops. The failed send can
// still be ambiguous (e.g. a timeout after delivery); never automatically retry.
func sendError(w http.ResponseWriter, err error, completed []map[string]any, to string) {
	code, message := http.StatusBadGateway, "message send failed; delivery to the failed recipient may be unknown"
	if errors.Is(err, context.DeadlineExceeded) {
		code, message = http.StatusGatewayTimeout, "message send timed out; delivery may be unknown"
	}
	if errors.Is(err, context.Canceled) {
		code, message = http.StatusRequestTimeout, "message send canceled; delivery may be unknown"
	}
	writeJSON(w, code, map[string]any{"status": "error", "message": message, "completed": completed, "failedRecipient": to})
}

func (s *Server) sendAdvanced(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		mediaError(w, errors.New("expected application/json"), "")
		return
	}
	route := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	allowed := "phone isGroup isLid isNewsletter "
	switch route {
	case "contact-vcard", "send-contact":
		allowed += "contacts contactsId name"
	case "send-location":
		allowed += "lat lng title address"
	case "send-poll-message":
		allowed += "name choices options"
	}
	var raw json.RawMessage
	if err := decodeJSON(r.Body, &raw); err != nil {
		mediaError(w, err, "")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		mediaError(w, errors.New("expected a JSON object"), "")
		return
	}
	for field := range fields {
		if !strings.Contains(" "+allowed+" ", " "+field+" ") {
			mediaError(w, fmt.Errorf("unsupported field %q", field), "")
			return
		}
	}
	if fields["contacts"] != nil && fields["contactsId"] != nil {
		mediaError(w, errors.New("use contacts or contactsId, not both"), "")
		return
	}
	var req advancedRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		mediaError(w, err, "")
		return
	}
	if req.IsLid || req.IsNewsletter {
		mediaError(w, errors.New("LID/newsletter destinations are not supported for advanced sends"), "")
		return
	}
	recipients, err := destinations(req.Phone, req.IsGroup)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	var msg *waE2E.Message
	kind, status := "", http.StatusCreated
	var build func() *waE2E.Message
	switch route {
	case "send-contact", "contact-vcard":
		kind = "vcard"
		if route == "contact-vcard" {
			status = http.StatusOK
		}
		msg, err = messages.Contacts(req.Contacts, req.ContactsID, req.Name)
	case "send-location":
		kind = "location"
		msg, err = messages.Location(req.Lat, req.Lng, req.Title, req.Address)
	case "send-poll-message":
		kind = "poll_creation"
		var selectable *int
		if req.Options != nil {
			selectable = req.Options.SelectableCount
		}
		var count int
		count, err = messages.ValidatePoll(req.Name, req.Choices, selectable)
		if err == nil {
			builder, supported := client.(messages.PollBuilder)
			if !supported {
				writeJSON(w, http.StatusNotImplemented, map[string]any{"status": "error", "message": "poll builder unavailable"})
				return
			}
			build = func() *waE2E.Message { return builder.BuildPollCreation(req.Name, req.Choices, count) }
		}
	}
	if err != nil {
		mediaError(w, err, "")
		return
	}
	if build == nil {
		build = func() *waE2E.Message { return proto.Clone(msg).(*waE2E.Message) }
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	results := make([]map[string]any, 0, len(recipients))
	for _, jid := range recipients {
		response, err := client.SendMessage(ctx, jid, build())
		if err != nil {
			sendError(w, err, results, jid.String())
			return
		}
		s.observeSent(cleanSession(chi.URLParam(r, "session")), jid, response.ID, response.Timestamp)
		results = append(results, map[string]any{"id": response.ID, "to": jid.String(), "type": kind})
	}
	writeJSON(w, status, map[string]any{"status": "success", "response": results, "mapper": "return"})
}
