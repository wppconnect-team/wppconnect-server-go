package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/messages"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type channelRequest struct {
	ID          string    `json:"id"`
	Parent      string    `json:"parentId"`
	Child       string    `json:"groupId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Value       *bool     `json:"value"`
	Count       int       `json:"count"`
	Before      int       `json:"before"`
	Phone       string    `json:"phone"`
	IsGroup     bool      `json:"isGroup"`
	Message     string    `json:"message"`
	CallID      string    `json:"callId"`
	MessageID   string    `json:"messageId"`
	Choices     *[]string `json:"choices"`
}

func (s *Server) channelRoutes(r chi.Router) {
	r.Get("/api/{session}/newsletters", s.channelOperation("newsletters"))
	for _, name := range []string{"create-community", "community-info", "link-community-group", "unlink-community-group", "create-newsletter", "newsletter-info", "follow-newsletter", "unfollow-newsletter", "mute-newsletter", "subscribe-newsletter", "newsletter-messages", "send-newsletter-text", "reject-call", "vote-poll"} {
		r.Post("/api/{session}/"+name, s.channelOperation(name))
	}
}
func (s *Server) channelConnection(w http.ResponseWriter, r *http.Request) (channels.Client, bool) {
	if s.channelSession != nil {
		if client, ok := s.channelSession(cleanSession(chi.URLParam(r, "session"))); ok {
			return client, true
		}
	}
	writeJSON(w, 404, map[string]any{"status": "disconnected"})
	return nil, false
}
func newsletterJID(value string) (types.JID, error) {
	j, err := types.ParseJID(value)
	if err != nil || j.Server != types.NewsletterServer || j.User == "" || len(j.User) > 32 || j.Device != 0 {
		return types.EmptyJID, errors.New("id must be a newsletter JID")
	}
	for _, r := range j.User {
		if r < '0' || r > '9' {
			return types.EmptyJID, errors.New("invalid newsletter JID")
		}
	}
	return j, nil
}
func newsletterInfo(v *types.NewsletterMetadata) any {
	if v == nil {
		return nil
	}
	return map[string]any{"id": v.ID.String(), "state": v.State, "thread": v.ThreadMeta, "viewer": v.ViewerMeta}
}
func (s *Server) channelOperation(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.channelConnection(w, r)
		if !ok || !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		var req channelRequest
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
		case "create-community":
			if err = account.Text(req.Name, 25); err != nil || len([]rune(req.Description)) > 2048 {
				if err == nil {
					err = errors.New("description exceeds 2048 characters")
				}
				mediaError(w, err, "")
				return
			}
			var info *types.GroupInfo
			info, err = client.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: req.Name, GroupParent: types.GroupParent{IsParent: true}})
			if err == nil {
				result = groups.Info(info)
				if req.Description != "" && info != nil {
					if e := client.SetGroupDescription(ctx, info.JID, req.Description); e != nil {
						writeJSON(w, 502, map[string]any{"status": "error", "message": "community created; description update failed", "completed": map[string]any{"id": info.JID.String()}})
						return
					}
				}
			}
		case "community-info", "link-community-group", "unlink-community-group":
			parentValue := req.Parent
			if parentValue == "" {
				parentValue = req.ID
			}
			parent, e := groups.ParseGroup(parentValue)
			if e != nil {
				mediaError(w, e, "")
				return
			}
			info, e := client.GetGroupInfo(ctx, parent)
			if e != nil {
				directoryError(w, e)
				return
			}
			if info == nil || !info.IsParent {
				mediaError(w, errors.New("parentId must identify a community"), "")
				return
			}
			if kind == "community-info" {
				subs, e := client.GetSubGroups(ctx, parent)
				err = e
				result = map[string]any{"community": groups.Info(info), "groups": subs}
			} else {
				child, e := groups.ParseGroup(req.Child)
				if e != nil || child == parent {
					mediaError(w, errors.New("invalid child group"), "")
					return
				}
				if kind == "link-community-group" {
					err = client.LinkGroup(ctx, parent, child)
				} else {
					err = client.UnlinkGroup(ctx, parent, child)
				}
			}
		case "newsletters":
			var list []*types.NewsletterMetadata
			list, err = client.GetSubscribedNewsletters(ctx)
			out := []any{}
			for _, v := range list[:min(len(list), 1000)] {
				out = append(out, newsletterInfo(v))
			}
			result = out
		case "create-newsletter":
			if err = account.Text(req.Name, 100); err != nil || len([]rune(req.Description)) > 2048 {
				mediaError(w, errors.New("newsletter name 1..100 and description up to 2048 characters"), "")
				return
			}
			var info *types.NewsletterMetadata
			info, err = client.CreateNewsletter(ctx, whatsmeow.CreateNewsletterParams{Name: req.Name, Description: req.Description})
			result = newsletterInfo(info)
		case "newsletter-info", "follow-newsletter", "unfollow-newsletter", "mute-newsletter", "subscribe-newsletter", "newsletter-messages", "send-newsletter-text":
			jid, e := newsletterJID(req.ID)
			if e != nil {
				mediaError(w, e, "")
				return
			}
			switch kind {
			case "newsletter-info":
				var info *types.NewsletterMetadata
				info, err = client.GetNewsletterInfo(ctx, jid)
				result = newsletterInfo(info)
			case "follow-newsletter":
				err = client.FollowNewsletter(ctx, jid)
			case "unfollow-newsletter":
				err = client.UnfollowNewsletter(ctx, jid)
			case "mute-newsletter":
				if req.Value == nil {
					mediaError(w, errors.New("value is required"), "")
					return
				}
				err = client.NewsletterToggleMute(ctx, jid, *req.Value)
			case "subscribe-newsletter":
				var duration time.Duration
				duration, err = client.NewsletterSubscribeLiveUpdates(ctx, jid)
				result = map[string]any{"expiresInSeconds": int64(duration / time.Second)}
			case "newsletter-messages":
				count := req.Count
				if count == 0 {
					count = 50
				}
				if count < 1 || count > 100 || req.Before < 0 {
					mediaError(w, errors.New("count must be 1..100 and before nonnegative"), "")
					return
				}
				var list []*types.NewsletterMessage
				list, err = client.GetNewsletterMessages(ctx, jid, &whatsmeow.GetNewsletterMessagesParams{Count: count, Before: types.MessageServerID(req.Before)})
				out := []map[string]any{}
				for _, item := range list[:min(len(list), count)] {
					if item == nil {
						continue
					}
					event := media.Event(&events.Message{Info: types.MessageInfo{ID: item.MessageID, Timestamp: item.Timestamp, MessageSource: types.MessageSource{Chat: jid}}, Message: item.Message})
					if event == nil {
						event = map[string]any{}
					}
					event["serverId"], event["views"] = item.MessageServerID, item.ViewsCount
					out = append(out, event)
				}
				result = out
			case "send-newsletter-text":
				var message *waE2E.Message
				message, err = messages.Text(req.Message, nil, nil)
				if err != nil {
					mediaError(w, err, "")
					return
				}
				var resp whatsmeow.SendResponse
				resp, err = client.SendMessage(ctx, jid, message)
				if err == nil {
					s.observeSent(cleanSession(chi.URLParam(r, "session")), jid, resp.ID, resp.Timestamp, message)
					result = map[string]any{"id": resp.ID, "to": jid.String()}
				}
			}
		case "reject-call":
			jid, e := directory.Parse(req.Phone, false, false)
			if e != nil || jid.Server == types.GroupServer || req.CallID == "" || len(req.CallID) > 256 {
				mediaError(w, errors.New("individual phone and callId (1..256 bytes) are required"), "")
				return
			}
			err = client.RejectCall(ctx, jid, req.CallID)
		case "vote-poll":
			jid, e := directory.Parse(req.Phone, req.IsGroup, false)
			if e != nil {
				mediaError(w, e, "")
				return
			}
			if req.Choices == nil {
				mediaError(w, errors.New("choices is required; [] withdraws the vote"), "")
				return
			}
			store, close, ok := s.archive(w, r)
			if !ok {
				return
			}
			defer close()
			record, e := store.Get(ctx, jid.String(), req.MessageID)
			if e != nil {
				historyError(w, e)
				return
			}
			choices, maxChoices, e := history.PollOptions(record)
			if e != nil {
				historyError(w, e)
				return
			}
			selected := *req.Choices
			if len(selected) > len(choices) || (maxChoices > 0 && len(selected) > maxChoices) {
				mediaError(w, errors.New("too many selected poll choices"), "")
				return
			}
			seen := map[string]bool{}
			for _, option := range selected {
				found := false
				for _, known := range choices {
					if known == option {
						found = true
					}
				}
				if !found || seen[option] {
					mediaError(w, errors.New("unknown or duplicate poll choice"), "")
					return
				}
				seen[option] = true
			}
			info := record.Info()
			message, e := client.BuildPollVote(ctx, &info, selected)
			if e != nil {
				directoryError(w, e)
				return
			}
			var resp whatsmeow.SendResponse
			resp, err = client.SendMessage(ctx, jid, message)
			result = map[string]any{"id": resp.ID, "to": jid.String()}
			if err == nil && !client.Self().IsEmpty() {
				hashes := make([][]byte, len(selected))
				for i, choice := range selected {
					sum := sha256.Sum256([]byte(choice))
					hashes[i] = sum[:]
				}
				if e := store.Vote(ctx, jid.String(), req.MessageID, client.Self().ToNonAD().String(), time.Now().UnixMilli(), hashes); e != nil {
					writeJSON(w, 500, map[string]any{"status": "error", "message": "vote accepted; local result update failed", "completed": result})
					return
				}
			}
		}
		if err != nil {
			directoryError(w, err)
			return
		}
		status := 200
		if strings.HasPrefix(kind, "create-") || kind == "send-newsletter-text" || kind == "vote-poll" {
			status = 201
		}
		writeJSON(w, status, map[string]any{"status": "success", "response": result})
	}
}
