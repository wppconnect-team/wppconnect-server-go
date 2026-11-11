package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
)

const deleteNewsletter = "delete-newsletter"

type newsletterDeleteRequest struct {
	ID string `json:"id"`
}
type newsletterDeleter interface {
	SupportsNewsletterDeletion() bool
	DeleteNewsletter(context.Context, types.JID) error
}

var _ newsletterDeleter = channels.SDK{}

func (s *Server) removeNewsletter(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Idempotency-Key") == "" {
		commandError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "channel deletion requires Idempotency-Key")
		return
	}
	var q newsletterDeleteRequest
	if err := readStructured(w, r, &q); err != nil {
		mediaError(w, err, "")
		return
	}
	jid, err := newsletterJID(q.ID)
	if err != nil || jid.String() != q.ID || jid.RawAgent != 0 || jid.Integrator != 0 || r.URL.RawQuery != "" {
		mediaError(w, errors.New("provide a canonical numeric newsletter JID in the body"), "")
		return
	}
	client, ok := s.channelConnection(w, r)
	if !ok {
		return
	}
	api, ok := client.(newsletterDeleter)
	if !ok || !api.SupportsNewsletterDeletion() {
		writeJSON(w, 501, map[string]string{"status": "not_supported", "message": "this session cannot issue the Web channel deletion query"})
		return
	}
	if !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	info, err := client.GetNewsletterInfo(ctx, jid)
	if err != nil {
		directoryError(w, err)
		return
	}
	if info == nil || info.ID != jid {
		writeJSON(w, 502, map[string]string{"status": "error", "message": "channel identity could not be verified"})
		return
	}
	if info.ViewerMeta == nil || info.ViewerMeta.Role != types.NewsletterRoleOwner {
		writeJSON(w, 403, map[string]string{"status": "error", "message": "channel owner role required"})
		return
	}
	if err := api.DeleteNewsletter(ctx, jid); err != nil {
		writeJSON(w, 502, map[string]string{"status": "unconfirmed", "message": "deletion was not confirmed. Check channel state before issuing a new command."})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]string{"id": jid.String(), "state": "DELETED"}})
}
