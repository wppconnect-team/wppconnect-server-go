package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func (s *Server) groupRoutes(r chi.Router) {
	r.Get("/api/{session}/all-groups", s.allGroups)
	r.Get("/api/{session}/common-groups/{wid}", s.commonGroups)
	for route, mode := range map[string]string{
		"group-members": "members", "group-members-ids": "ids", "group-admins": "admins", "group-info": "info",
		"group-invite-link": "invite", "group-revoke-link": "revoke",
	} {
		r.Get("/api/{session}/"+route+"/{groupId}", s.readGroup(mode))
	}
	r.Post("/api/{session}/create-group", s.createGroup)
	for route, action := range map[string]whatsmeow.ParticipantChange{
		"add-participant-group": whatsmeow.ParticipantChangeAdd, "remove-participant-group": whatsmeow.ParticipantChangeRemove,
		"promote-participant-group": whatsmeow.ParticipantChangePromote, "demote-participant-group": whatsmeow.ParticipantChangeDemote,
	} {
		r.Post("/api/{session}/"+route, s.updateParticipants(action))
	}
	for _, route := range []string{"group-subject", "group-description", "group-property", "messages-admins-only", "change-privacy-group", "leave-group"} {
		r.Post("/api/{session}/"+route, s.changeGroup(route))
	}
	r.Post("/api/{session}/join-code", s.joinGroup)
	r.Post("/api/{session}/group-info-from-invite-link", s.inviteInfo)
	r.Post("/api/{session}/group-pic", s.groupPicture)
}

func (s *Server) groupConnection(w http.ResponseWriter, r *http.Request) (groups.Client, bool) {
	name := cleanSession(chi.URLParam(r, "session"))
	if s.groupSession != nil {
		if client, ok := s.groupSession(name); ok {
			return client, true
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"status": "disconnected", "session": name})
	return nil, false
}

func groupContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

func groupErrorStatus(err error) (int, string) {
	var iq *whatsmeow.IQError
	var maxBody *http.MaxBytesError
	switch {
	case errors.As(err, &maxBody):
		return 413, "group request exceeds the body limit"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, whatsmeow.ErrIQTimedOut):
		return 504, "group operation timed out"
	case errors.Is(err, context.Canceled):
		return 408, "group operation canceled"
	case errors.Is(err, whatsmeow.ErrNotConnected), errors.Is(err, whatsmeow.ErrNotLoggedIn):
		return 503, "WhatsApp session is disconnected"
	case errors.Is(err, whatsmeow.ErrNotInGroup), errors.Is(err, whatsmeow.ErrGroupInviteLinkUnauthorized):
		return 403, "group operation forbidden for this account"
	case errors.Is(err, whatsmeow.ErrGroupNotFound):
		return 404, "group not found"
	case errors.Is(err, whatsmeow.ErrInviteLinkRevoked):
		return 410, "group invitation was revoked"
	case errors.Is(err, whatsmeow.ErrInviteLinkInvalid), errors.Is(err, whatsmeow.ErrInvalidImageFormat):
		return 400, "invalid invitation or group image"
	case errors.As(err, &iq):
		switch iq.Code {
		case 401, 403, 405:
			return 403, "group operation forbidden for this account"
		case 404:
			return 404, "group not found"
		case 410:
			return 410, "group invitation was revoked"
		case 400, 406:
			return 400, "WhatsApp rejected the group request"
		case 429, 419:
			return 429, "WhatsApp group limit reached"
		case 423:
			return 409, "group is locked"
		}
	}
	return 502, "WhatsApp group operation failed"
}

func groupError(w http.ResponseWriter, err error, completed any) {
	code, message := groupErrorStatus(err)
	body := map[string]any{"status": "error", "message": message}
	if completed != nil {
		body["completed"] = completed
	}
	writeJSON(w, code, body)
}

func groupInputError(w http.ResponseWriter, err error) {
	var maxBody *http.MaxBytesError
	if errors.As(err, &maxBody) {
		groupError(w, err, nil)
		return
	}
	writeJSON(w, 400, map[string]any{"status": "error", "message": err.Error()})
}

func readGroupJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, groups.MaxBodyBytes)
	return decodeJSON(r.Body, value)
}

func (s *Server) allGroups(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok {
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	list, err := client.GetJoinedGroups(ctx)
	if err != nil {
		groupError(w, err, nil)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, g := range list {
		if g != nil {
			out = append(out, groups.Summary(g))
		}
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": out})
}

func (s *Server) readGroup(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.groupConnection(w, r)
		if !ok {
			return
		}
		param, err := url.PathUnescape(chi.URLParam(r, "groupId"))
		if err != nil {
			groupInputError(w, errors.New("invalid encoded groupId"))
			return
		}
		raw, _ := json.Marshal(param)
		ids, err := groups.ParseList(raw, true, 20)
		if err != nil {
			groupInputError(w, err)
			return
		}
		ctx, cancel := groupContext(r)
		defer cancel()
		var response any
		admins := make([]any, 0, len(ids))
		completed := make([]string, 0, len(ids))
		for _, id := range ids {
			if mode == "invite" || mode == "revoke" {
				response, err = client.GetGroupInviteLink(ctx, id, mode == "revoke")
			} else {
				var info *types.GroupInfo
				info, err = client.GetGroupInfo(ctx, id)
				if err == nil && info == nil {
					err = errors.New("missing group info")
				}
				if err == nil {
					members := make([]any, 0, len(info.Participants))
					for _, p := range info.Participants {
						switch mode {
						case "members":
							members = append(members, groups.Participant(p))
						case "ids":
							members = append(members, groups.WID(p.JID))
						case "admins":
							if p.IsAdmin || p.IsSuperAdmin {
								members = append(members, groups.WID(p.JID))
							}
						}
					}
					response = members
					if mode == "info" {
						response = groups.Info(info)
					}
					if mode == "admins" {
						admins = append(admins, members)
					}
				}
			}
			if err != nil {
				groupError(w, err, completed)
				return
			}
			completed = append(completed, id.String())
		}
		if mode == "admins" {
			response = admins
		}
		status := "success"
		if mode == "revoke" {
			status = "Success"
			w.Header().Set("Cache-Control", "no-store")
		}
		if mode == "invite" {
			w.Header().Set("Cache-Control", "no-store")
		}
		writeJSON(w, 200, map[string]any{"status": status, "response": response})
	}
}

func validSubject(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 100
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok {
		return
	}
	var req struct {
		Name         json.RawMessage `json:"name"`
		Participants json.RawMessage `json:"participants"`
	}
	if err := readGroupJSON(w, r, &req); err != nil {
		groupInputError(w, err)
		return
	}
	names, err := groups.Strings(req.Name, 10)
	if err != nil {
		groupInputError(w, err)
		return
	}
	for _, name := range names {
		if !validSubject(name) {
			groupInputError(w, errors.New("name must contain 1 to 100 characters"))
			return
		}
	}
	participants, err := groups.ParseList(req.Participants, false, 100)
	if err != nil {
		groupInputError(w, err)
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	results := make([]map[string]any, 0, len(names))
	partial := false
	for _, name := range names {
		info, err := client.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: participants})
		if err == nil && (info == nil || info.JID.IsEmpty()) {
			err = errors.New("missing created group ID")
		}
		if err != nil {
			groupError(w, err, results)
			return
		}
		members := make([]map[string]any, 0, len(info.Participants))
		for _, p := range info.Participants {
			members = append(members, groups.Participant(p))
			if (p.Error != 0 && p.Error != 200) || p.AddRequest != nil {
				partial = true
			}
		}
		results = append(results, map[string]any{"name": name, "id": info.JID.User, "participants": members})
	}
	status := "success"
	if partial {
		status = "partial"
	}
	writeJSON(w, 201, map[string]any{"status": status, "response": map[string]any{"message": "Group(s) created successfully", "group": req.Name, "groupInfo": results}})
}

func (s *Server) updateParticipants(action whatsmeow.ParticipantChange) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.groupConnection(w, r)
		if !ok {
			return
		}
		var req struct {
			GroupID json.RawMessage `json:"groupId"`
			Phone   json.RawMessage `json:"phone"`
		}
		if err := readGroupJSON(w, r, &req); err != nil {
			groupInputError(w, err)
			return
		}
		ids, err := groups.ParseList(req.GroupID, true, 20)
		if err != nil {
			groupInputError(w, err)
			return
		}
		people, err := groups.ParseList(req.Phone, false, 100)
		if err != nil {
			groupInputError(w, err)
			return
		}
		ctx, cancel := groupContext(r)
		defer cancel()
		results := make([]any, 0, len(ids))
		completed := make([]string, 0, len(ids))
		groupIDs := make([]string, 0, len(ids))
		failed, succeeded := 0, 0
		failCode := 0
		for _, id := range ids {
			participants, err := client.UpdateGroupParticipants(ctx, id, people, action)
			if err != nil {
				groupError(w, err, map[string]any{"groups": completed, "result": results})
				return
			}
			batch := make([]map[string]any, 0, len(people))
			for _, person := range people {
				var found *types.GroupParticipant
				for i := range participants {
					p := &participants[i]
					if p.JID == person || p.PhoneNumber == person || p.LID == person {
						found = p
						break
					}
				}
				if found == nil {
					found = &types.GroupParticipant{JID: person, Error: 502}
				}
				if found.AddRequest != nil && (found.Error == 0 || found.Error == 200) {
					copy := *found
					copy.Error = 403
					found = &copy
				}
				batch = append(batch, groups.Participant(*found))
				if found.Error != 0 && found.Error != 200 {
					failed++
					if failCode == 0 {
						failCode = found.Error
					}
				} else {
					succeeded++
				}
			}
			results = append(results, batch)
			completed = append(completed, id.String())
			groupIDs = append(groupIDs, id.String())
		}
		code, status, message := 201, "success", "Successful promoted participant(s)"
		switch action {
		case whatsmeow.ParticipantChangeAdd:
			message = "Addition to group attempted."
		case whatsmeow.ParticipantChangeRemove:
			code, message = 200, "Participant(s) removed successfully"
		case whatsmeow.ParticipantChangeDemote:
			message = "Admin of participant(s) revoked successfully"
		}
		if failed > 0 {
			status, message = "partial", "Some participant operations were rejected; inspect result before retrying"
			code = 207
			if succeeded == 0 {
				status = "error"
				message = "Participant operations were rejected; inspect result before retrying"
				code, _ = groupErrorStatus(&whatsmeow.IQError{Code: failCode})
			}
		}
		response := map[string]any{"message": message, "participants": req.Phone, "groups": groupIDs, "result": results}
		if action == whatsmeow.ParticipantChangeRemove {
			response["groups"] = results
		}
		writeJSON(w, code, map[string]any{"status": status, "response": response})
	}
}

type groupChange struct {
	GroupID     json.RawMessage `json:"groupId"`
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
	Property    string          `json:"property"`
	Value       json.RawMessage `json:"value"`
	Status      json.RawMessage `json:"status"`
}

func propertyValue(raw json.RawMessage, ephemeral bool) (bool, time.Duration, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return false, 0, errors.New("value must not be null")
	}
	if len(raw) == 0 {
		raw = json.RawMessage("true")
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil && string(raw) != "null" {
		if ephemeral && value {
			return true, 7 * 24 * time.Hour, nil
		}
		return value, 0, nil
	}
	if ephemeral {
		var seconds int64
		if json.Unmarshal(raw, &seconds) == nil {
			if seconds == 1 {
				seconds = 604800
			}
			if seconds == 0 || seconds == 86400 || seconds == 604800 || seconds == 7776000 {
				return seconds != 0, time.Duration(seconds) * time.Second, nil
			}
		}
	}
	return false, 0, errors.New("value must be boolean, or 0/86400/604800/7776000 for ephemeral")
}

func (s *Server) changeGroup(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, ok := s.groupConnection(w, r)
		if !ok {
			return
		}
		var req groupChange
		if err := readGroupJSON(w, r, &req); err != nil {
			groupInputError(w, err)
			return
		}
		ids, err := groups.ParseList(req.GroupID, true, 20)
		if err != nil {
			groupInputError(w, err)
			return
		}
		property := req.Property
		if mode == "messages-admins-only" {
			property = "announcement"
		}
		if mode == "change-privacy-group" {
			property = "restrict"
			req.Value = req.Status
			if string(req.Status) == `"true"` {
				req.Value = json.RawMessage("true")
			}
			if string(req.Status) == `"false"` {
				req.Value = json.RawMessage("false")
			}
			if len(req.Status) == 0 {
				groupInputError(w, errors.New("status is required"))
				return
			}
		}
		value, duration := false, time.Duration(0)
		switch mode {
		case "group-subject":
			if req.Title == nil || !validSubject(*req.Title) {
				groupInputError(w, errors.New("title must contain 1 to 100 characters"))
				return
			}
		case "group-description":
			if req.Description == nil || utf8.RuneCountInString(*req.Description) > 2048 {
				groupInputError(w, errors.New("description is required and must fit in 2048 characters"))
				return
			}
		case "group-property", "messages-admins-only", "change-privacy-group":
			if property != "restrict" && property != "announcement" && property != "ephemeral" {
				groupInputError(w, errors.New("unsupported group property"))
				return
			}
			value, duration, err = propertyValue(req.Value, property == "ephemeral")
			if err != nil {
				groupInputError(w, err)
				return
			}
		}
		ctx, cancel := groupContext(r)
		defer cancel()
		completed := make([]string, 0, len(ids))
		for _, id := range ids {
			switch mode {
			case "group-subject":
				err = client.SetGroupName(ctx, id, *req.Title)
			case "group-description":
				err = client.SetGroupDescription(ctx, id, *req.Description)
			case "leave-group":
				err = client.LeaveGroup(ctx, id)
			default:
				switch property {
				case "restrict":
					err = client.SetGroupLocked(ctx, id, value)
				case "announcement":
					err = client.SetGroupAnnounce(ctx, id, value)
				case "ephemeral":
					err = client.SetDisappearingTimer(ctx, id, duration, time.Now())
				}
			}
			if err != nil {
				groupError(w, err, completed)
				return
			}
			completed = append(completed, id.String())
		}
		var response any = true
		if mode == "leave-group" {
			response = map[string]any{"messages": "Você saiu do grupo com sucesso", "group": req.GroupID}
		}
		if mode == "change-privacy-group" {
			response = map[string]any{"message": "Group privacy changed successfully"}
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": response})
	}
}

func (s *Server) joinGroup(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok {
		return
	}
	var req struct {
		InviteCode string `json:"inviteCode"`
	}
	if err := readGroupJSON(w, r, &req); err != nil {
		groupInputError(w, err)
		return
	}
	code, err := groups.Invite(req.InviteCode)
	if err != nil {
		groupInputError(w, err)
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	jid, err := client.JoinGroupWithLink(ctx, code)
	if err == nil && jid.IsEmpty() {
		err = errors.New("missing joined group ID")
	}
	if err != nil {
		groupError(w, err, nil)
		return
	}
	writeJSON(w, 201, map[string]any{"status": "success", "response": map[string]any{"message": "The group join request was accepted; membership may require approval", "contact": req.InviteCode, "groupId": jid.String()}})
}

func (s *Server) inviteInfo(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok {
		return
	}
	var req struct {
		InviteCode string `json:"invitecode"`
	}
	if err := readGroupJSON(w, r, &req); err != nil {
		groupInputError(w, err)
		return
	}
	code, err := groups.Invite(req.InviteCode)
	if err != nil {
		groupInputError(w, err)
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	info, err := client.GetGroupInfoFromLink(ctx, code)
	if err == nil && info == nil {
		err = errors.New("missing group info")
	}
	if err != nil {
		groupError(w, err, nil)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": groups.Info(info)})
}

func (s *Server) commonGroups(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok {
		return
	}
	param, err := url.PathUnescape(chi.URLParam(r, "wid"))
	if err != nil {
		groupInputError(w, errors.New("invalid encoded participant"))
		return
	}
	person, err := groups.ParseParticipant(param)
	if err != nil {
		groupInputError(w, err)
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	list, err := client.GetJoinedGroups(ctx)
	if err != nil {
		groupError(w, err, nil)
		return
	}
	result := make([]map[string]any, 0)
	for _, info := range list {
		if info == nil {
			continue
		}
		if info.ParticipantCount > len(info.Participants) {
			info, err = client.GetGroupInfo(ctx, info.JID)
			if err == nil && info == nil {
				err = errors.New("missing group info")
			}
			if err != nil {
				groupError(w, err, nil)
				return
			}
		}
		for _, p := range info.Participants {
			if p.JID == person || p.PhoneNumber == person || p.LID == person {
				result = append(result, groups.Summary(info))
				break
			}
		}
	}
	// The Node controller returns the array directly for this route.
	writeJSON(w, 200, result)
}

func (s *Server) groupPicture(w http.ResponseWriter, r *http.Request) {
	client, ok := s.groupConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	req, data, err := readMedia(w, r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	ids, err := groups.ParseList(req.GroupID, true, 20)
	if err != nil {
		groupInputError(w, err)
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		groupInputError(w, errors.New("group picture must be JPEG/PNG with at most 16 million pixels"))
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		groupInputError(w, errors.New("invalid group picture"))
		return
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, groupAvatar(img), &jpeg.Options{Quality: 85}); err != nil {
		groupError(w, err, nil)
		return
	}
	if jpegData.Len() > media.MaxBytes {
		mediaError(w, media.ErrTooLarge, "")
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	completed := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := client.SetGroupPhoto(ctx, id, jpegData.Bytes()); err != nil {
			groupError(w, err, completed)
			return
		}
		completed = append(completed, id.String())
	}
	writeJSON(w, 201, map[string]any{"status": "success", "response": map[string]any{"message": "Group profile photo successfully changed"}})
}

// Normalize avatars to a centered square and composite transparency onto white.
func groupAvatar(src image.Image) image.Image {
	bounds := src.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x0, y0 := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, 640, 640))
	for y := 0; y < 640; y++ {
		for x := 0; x < 640; x++ {
			r, g, b, a := src.At(x0+x*side/640, y0+y*side/640).RGBA()
			dst.SetRGBA(x, y, color.RGBA{R: uint8((r + 0xffff - a) >> 8), G: uint8((g + 0xffff - a) >> 8), B: uint8((b + 0xffff - a) >> 8), A: 255})
		}
	}
	return dst
}
