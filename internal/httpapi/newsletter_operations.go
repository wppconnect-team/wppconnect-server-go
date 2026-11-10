package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/jpeg"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"google.golang.org/protobuf/proto"
)

type newsletterInviteRequest struct {
	Invite string `json:"invite"`
}
type newsletterReactionRequest struct {
	ID       string  `json:"id"`
	ServerID int64   `json:"serverId"`
	Reaction *string `json:"reaction"`
}
type newsletterViewRequest struct {
	ID        string  `json:"id"`
	ServerIDs []int64 `json:"serverIds"`
}
type newsletterUpdatesRequest struct {
	ID    string `json:"id"`
	Count *int   `json:"count"`
	Since int64  `json:"since"`
	After int64  `json:"after"`
}
type newsletterTOSRequest struct {
	NoticeID string `json:"noticeId"`
	Stage    string `json:"stage"`
	Accepted bool   `json:"accepted"`
}
type newsletterAdminInviteRequest struct {
	ID               string `json:"id"`
	Phone            string `json:"phone"`
	Caption          string `json:"caption"`
	Thumbnail        string `json:"thumbnail"`
	ExpiresInSeconds int64  `json:"expiresInSeconds"`
}

var newsletterInvitePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
var newsletterNoticePattern = regexp.MustCompile(`^[0-9]{1,32}$`)

func newsletterInvite(value string) (string, error) {
	if strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host != "whatsapp.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/channel/") {
			return "", errors.New("invite must be a WhatsApp channel link or code")
		}
		value = strings.TrimPrefix(parsed.Path, "/channel/")
	}
	if !newsletterInvitePattern.MatchString(value) {
		return "", errors.New("invalid channel invite code")
	}
	return value, nil
}
func (s *Server) newsletterRoutes(r chi.Router) {
	for _, operation := range []string{"newsletter-info-invite", "follow-newsletter-invite", "unfollow-newsletter-invite", "newsletter-reaction", "newsletter-mark-viewed", "newsletter-message-updates", "newsletter-accept-tos", "newsletter-admin-invite"} {
		r.Post("/api/{session}/"+operation, s.newsletterOperation(operation))
	}
}
func (s *Server) newsletterOperation(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.channelConnection(w, r)
		if !ok {
			return
		}
		extended, ok := client.(channels.NewsletterOperations)
		if !ok {
			writeJSON(w, 501, map[string]any{"status": "not_supported", "message": "newsletter operations unavailable"})
			return
		}
		if !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var result any = map[string]any{}
		var err error
		code := http.StatusOK
		invalid := func(err error) { mediaError(w, err, "") }
		switch operation {
		case "newsletter-info-invite", "follow-newsletter-invite", "unfollow-newsletter-invite":
			var req newsletterInviteRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			invite, e := newsletterInvite(req.Invite)
			if e != nil {
				invalid(e)
				return
			}
			info, e := extended.GetNewsletterInfoWithInvite(ctx, invite)
			if e != nil {
				directoryError(w, e)
				return
			}
			if info == nil {
				writeJSON(w, 404, map[string]any{"status": "error", "message": "newsletter invite not found"})
				return
			}
			jid, e := newsletterJID(info.ID.String())
			if e != nil {
				writeJSON(w, 502, map[string]any{"status": "error", "message": "invalid newsletter identity from upstream"})
				return
			}
			if operation == "follow-newsletter-invite" {
				err = client.FollowNewsletter(ctx, jid)
			} else if operation == "unfollow-newsletter-invite" {
				err = client.UnfollowNewsletter(ctx, jid)
			}
			result = newsletterInfo(info)
		case "newsletter-reaction":
			var req newsletterReactionRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			jid, e := newsletterJID(req.ID)
			if e != nil || req.ServerID < 1 || req.ServerID > 2147483647 || req.Reaction == nil || !utf8.ValidString(*req.Reaction) || len(*req.Reaction) > 64 {
				invalid(errors.New("newsletter id, serverId 1..2147483647 and reaction up to 64 bytes are required; empty reaction removes it"))
				return
			}
			messageID := extended.GenerateMessageID()
			err = extended.NewsletterSendReaction(ctx, jid, types.MessageServerID(req.ServerID), *req.Reaction, messageID)
			code = http.StatusAccepted
			result = map[string]any{"id": messageID, "serverId": req.ServerID, "acknowledged": false}
		case "newsletter-mark-viewed":
			var req newsletterViewRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			jid, e := newsletterJID(req.ID)
			if e != nil || len(req.ServerIDs) < 1 || len(req.ServerIDs) > 100 {
				invalid(errors.New("newsletter id and 1..100 serverIds are required"))
				return
			}
			seen := map[int64]bool{}
			ids := make([]types.MessageServerID, 0, len(req.ServerIDs))
			for _, id := range req.ServerIDs {
				if id < 1 || id > 2147483647 || seen[id] {
					invalid(errors.New("serverIds must be unique integers in 1..2147483647"))
					return
				}
				seen[id] = true
				ids = append(ids, types.MessageServerID(id))
			}
			err = extended.NewsletterMarkViewed(ctx, jid, ids)
			result = map[string]any{"acknowledged": true, "count": len(ids)}
		case "newsletter-message-updates":
			var req newsletterUpdatesRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			jid, e := newsletterJID(req.ID)
			count := 50
			if req.Count != nil {
				count = *req.Count
			}
			if e != nil || count < 1 || count > 100 || req.Since < 0 || req.Since > 253402300799 || req.After < 0 || req.After > 2147483647 {
				invalid(errors.New("newsletter id, count 1..100, nonnegative Unix seconds since and after 0..2147483647 required"))
				return
			}
			params := &whatsmeow.GetNewsletterUpdatesParams{Count: count, After: types.MessageServerID(req.After)}
			if req.Since > 0 {
				params.Since = time.Unix(req.Since, 0)
			}
			list, e := extended.GetNewsletterMessageUpdates(ctx, jid, params)
			err = e
			output := make([]map[string]any, 0, min(len(list), count))
			for _, item := range list[:min(len(list), count)] {
				if item == nil {
					continue
				}
				reactions := map[string]int{}
				for _, reaction := range slices.Sorted(maps.Keys(item.ReactionCounts)) {
					count := item.ReactionCounts[reaction]
					if len(reactions) >= 100 {
						break
					}
					if len(reaction) <= 64 && count >= 0 {
						reactions[reaction] = count
					}
				}
				entry := map[string]any{"serverId": item.MessageServerID, "id": item.MessageID, "views": item.ViewsCount, "forwards": item.ForwardsCount, "reactions": reactions, "reactionsTruncated": len(reactions) != len(item.ReactionCounts)}
				if !item.Timestamp.IsZero() {
					entry["timestamp"] = item.Timestamp.Unix()
				}
				output = append(output, entry)
			}
			result = output
		case "newsletter-accept-tos":
			var req newsletterTOSRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			if !req.Accepted || !newsletterNoticePattern.MatchString(req.NoticeID) || !newsletterNoticePattern.MatchString(req.Stage) {
				invalid(errors.New("noticeId, stage and explicit accepted=true are required"))
				return
			}
			err = extended.AcceptTOSNotice(ctx, req.NoticeID, req.Stage)
		case "newsletter-admin-invite":
			var req newsletterAdminInviteRequest
			if err = readStructured(w, r, &req); err != nil {
				invalid(err)
				return
			}
			jid, e := newsletterJID(req.ID)
			to, toErr := directory.Parse(req.Phone, false, false)
			if req.ExpiresInSeconds == 0 {
				req.ExpiresInSeconds = 86400
			}
			if e != nil || toErr != nil || to.Server != types.DefaultUserServer || len([]rune(req.Caption)) > 2048 || req.ExpiresInSeconds < 60 || req.ExpiresInSeconds > 604800 || len(req.Thumbnail) > 88000 {
				invalid(errors.New("valid newsletter/phone, caption up to 2048 characters, thumbnail up to 64 KiB and expiry 60..604800 seconds required"))
				return
			}
			var thumbnail []byte
			if req.Thumbnail != "" {
				thumbnail, e = base64.StdEncoding.DecodeString(req.Thumbnail)
				if e != nil || len(thumbnail) > 65536 {
					invalid(errors.New("invalid base64 JPEG thumbnail"))
					return
				}
				image, e := jpeg.DecodeConfig(bytes.NewReader(thumbnail))
				if e != nil || image.Width > 512 || image.Height > 512 {
					invalid(errors.New("thumbnail must be JPEG up to 512x512"))
					return
				}
			}
			info, e := client.GetNewsletterInfo(ctx, jid)
			if e != nil {
				directoryError(w, e)
				return
			}
			if info == nil || info.ID != jid || info.ViewerMeta == nil || (info.ViewerMeta.Role != types.NewsletterRoleOwner && info.ViewerMeta.Role != types.NewsletterRoleAdmin) {
				writeJSON(w, 403, map[string]any{"status": "error", "message": "newsletter administrator role is required"})
				return
			}
			expires := time.Now().Add(time.Duration(req.ExpiresInSeconds) * time.Second).Unix()
			msg := &waE2E.Message{NewsletterAdminInviteMessage: &waE2E.NewsletterAdminInviteMessage{NewsletterJID: proto.String(jid.String()), NewsletterName: proto.String(info.ThreadMeta.Name.Text), JPEGThumbnail: thumbnail, Caption: proto.String(req.Caption), InviteExpiration: proto.Int64(expires)}}
			sent, e := client.SendMessage(ctx, to, msg)
			if e != nil {
				sendError(w, e, nil, to.String())
				return
			}
			s.observeSent(cleanSession(chi.URLParam(r, "session")), to, sent.ID, sent.Timestamp, msg)
			code = http.StatusCreated
			result = map[string]any{"id": sent.ID, "to": to.String(), "expiresAt": expires, "roleGranted": false}
		}
		if err != nil {
			directoryError(w, err)
			return
		}
		writeJSON(w, code, map[string]any{"status": "success", "response": result})
	}
}
