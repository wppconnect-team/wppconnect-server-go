package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type accountRequest struct {
	Phone  string `json:"phone"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Value  string `json:"value"`
	Remove bool   `json:"remove"`
}

func (s *Server) accountRoutes(r chi.Router) {
	r.Get("/api/{session}/privacy-settings", s.accountOperation("privacy-get"))
	r.Post("/api/{session}/privacy-settings", s.accountOperation("privacy-set"))
	r.Get("/api/{session}/blocklist", s.accountOperation("blocklist"))
	for _, name := range []string{"block-contact", "unblock-contact", "set-profile-name", "set-profile-status"} {
		r.Post("/api/{session}/"+name, s.accountOperation(name))
	}
	r.Post("/api/{session}/set-profile-picture", s.profilePhoto)
}
func (s *Server) accountConnection(w http.ResponseWriter, r *http.Request) (account.Client, bool) {
	if s.accountSession != nil {
		if client, ok := s.accountSession(cleanSession(chi.URLParam(r, "session"))); ok {
			return client, true
		}
	}
	writeJSON(w, 404, map[string]any{"status": "disconnected"})
	return nil, false
}
func (s *Server) accountOperation(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.accountConnection(w, r)
		if !ok {
			return
		}
		var req accountRequest
		if r.Method == http.MethodPost {
			if err := readStructured(w, r, &req); err != nil {
				mediaError(w, err, "")
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var result any = map[string]any{}
		var err error
		switch kind {
		case "privacy-get":
			result, err = client.TryFetchPrivacySettings(ctx, true)
		case "privacy-set":
			err = account.ValidatePrivacy(req.Name, req.Value)
			if err != nil {
				mediaError(w, err, "")
				return
			}
			result, err = client.SetPrivacySetting(ctx, types.PrivacySettingType(req.Name), types.PrivacySetting(req.Value))
		case "blocklist":
			result, err = client.GetBlocklist(ctx)
		case "block-contact", "unblock-contact":
			var jid types.JID
			jid, err = directory.Parse(req.Phone, false, false)
			if err != nil || jid.Server == types.GroupServer {
				mediaError(w, errors.New("block operations require an individual contact"), "")
				return
			}
			action := events.BlocklistChangeActionBlock
			if kind == "unblock-contact" {
				action = events.BlocklistChangeActionUnblock
			}
			result, err = client.UpdateBlocklist(ctx, jid, action)
		case "set-profile-name":
			if err = account.Text(req.Name, 25); err != nil {
				mediaError(w, err, "")
				return
			}
			err = client.SendAppState(ctx, appstate.BuildSettingPushName(req.Name))
		case "set-profile-status":
			if err = account.Text(req.Status, 139); err != nil {
				mediaError(w, err, "")
				return
			}
			err = client.SetStatusMessage(ctx, req.Status)
		}
		if err != nil {
			directoryError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"status": "success", "response": result})
	}
}
func (s *Server) profilePhoto(w http.ResponseWriter, r *http.Request) {
	client, ok := s.accountConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	_, data, err := readMedia(w, r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	if len(data) > 2<<20 {
		writeJSON(w, 413, map[string]any{"status": "error", "message": "profile picture limit is 2 MiB"})
		return
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 16000000 {
		mediaError(w, errors.New("invalid picture or pixel limit exceeded"), "")
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		mediaError(w, err, "")
		return
	}
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, groupAvatar(img), &jpeg.Options{Quality: 85}); err != nil {
		mediaError(w, err, "")
		return
	}
	self := client.Self()
	if self.IsEmpty() {
		writeJSON(w, 409, map[string]any{"status": "error", "message": "own account is not paired"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id, err := client.SetGroupPhoto(ctx, self, encoded.Bytes())
	if err != nil {
		directoryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"id": id}})
}
