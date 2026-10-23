package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/messages"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type interactiveRequest struct {
	Phone      string            `json:"phone"`
	IsGroup    bool              `json:"isGroup"`
	Title      string            `json:"title"`
	Message    string            `json:"message"`
	Footer     string            `json:"footer"`
	ButtonText string            `json:"buttonText"`
	Choices    []messages.Choice `json:"choices"`
}

func (s *Server) statusRoutes(r chi.Router) {
	r.Post("/api/{session}/send-status-text", s.sendStatus(false))
	r.Post("/api/{session}/send-status-media", s.sendStatus(true))
	for _, name := range []string{"send-buttons", "send-list"} {
		r.Post("/api/{session}/"+name, s.sendInteractive(name))
	}
	r.Post("/api/{session}/send-carousel", s.notSupported("carousel_delivery"))
}
func (s *Server) sendInteractive(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.ExperimentalInteractives {
			writeJSON(w, 501, map[string]any{"status": "not_supported", "capability": "experimental_interactives", "message": "reply buttons/lists require ENABLE_EXPERIMENTAL_INTERACTIVES; delivery varies by client/account"})
			return
		}
		client, _, ok := s.mediaConnection(w, r)
		if !ok || !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		var req interactiveRequest
		if err := readStructured(w, r, &req); err != nil {
			mediaError(w, err, "")
			return
		}
		jid, err := directory.Parse(req.Phone, req.IsGroup, false)
		if err != nil {
			mediaError(w, err, "")
			return
		}
		msg, err := messages.Interactive(kind, req.Title, req.Message, req.Footer, req.ButtonText, req.Choices)
		if err != nil {
			mediaError(w, err, "")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		resp, err := client.SendMessage(ctx, jid, msg)
		if err != nil {
			sendError(w, err, []map[string]any{}, jid.String())
			return
		}
		s.observeSent(cleanSession(chi.URLParam(r, "session")), jid, resp.ID, resp.Timestamp, msg)
		writeJSON(w, 201, map[string]any{"status": "success", "response": []map[string]any{{"id": resp.ID, "to": jid.String(), "type": kind}}, "experimental": true, "mapper": "return"})
	}
}
func (s *Server) sendStatus(upload bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, _, ok := s.mediaConnection(w, r)
		if !ok || !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		var msg *waE2E.Message
		if !upload {
			var req struct {
				Message string `json:"message"`
			}
			if err := readStructured(w, r, &req); err != nil {
				mediaError(w, err, "")
				return
			}
			if len([]rune(req.Message)) > 1000 {
				mediaError(w, errors.New("status text is limited to 1000 characters"), "")
				return
			}
			var err error
			msg, err = messages.Text(req.Message, nil, nil)
			if err != nil {
				mediaError(w, err, "")
				return
			}
		} else {
			req, data, err := readMedia(w, r)
			if err != nil {
				mediaError(w, err, "")
				return
			}
			kind := media.Image
			if http.DetectContentType(data) == "video/mp4" {
				kind = media.Video
			}
			mime, err := media.MIME(data, req.Mimetype, kind)
			if err != nil {
				mediaError(w, err, "")
				return
			}
			var info media.Info
			if kind == media.Video {
				info, err = media.InspectVideo(data)
				if err != nil {
					mediaError(w, err, "")
					return
				}
			}
			uploaded, err := client.Upload(ctx, data, media.UploadType(kind))
			if err != nil {
				mediaError(w, err, "status media upload failed")
				return
			}
			uploaded.FileLength = uint64(len(data))
			caption := req.Caption
			if req.Message != "" {
				caption = req.Message
			}
			msg = media.Message(kind, uploaded, mime, "", caption)
			if kind == media.Video {
				msg.VideoMessage.Width = proto.Uint32(info.Width)
				msg.VideoMessage.Height = proto.Uint32(info.Height)
				msg.VideoMessage.Seconds = proto.Uint32(info.Seconds)
			}
		}
		resp, err := client.SendMessage(ctx, types.StatusBroadcastJID, msg)
		if err != nil {
			sendError(w, err, []map[string]any{}, types.StatusBroadcastJID.String())
			return
		}
		s.observeSent(cleanSession(chi.URLParam(r, "session")), types.StatusBroadcastJID, resp.ID, resp.Timestamp, msg)
		writeJSON(w, 201, map[string]any{"status": "success", "response": []map[string]any{{"id": resp.ID, "to": types.StatusBroadcastJID.String(), "type": "status"}}, "mapper": "return"})
	}
}
