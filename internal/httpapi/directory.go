package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/groups"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func (s *Server) directoryRoutes(r chi.Router) {
	r.Get("/api/{session}/all-contacts", s.allContacts)
	r.Get("/api/{session}/contact/{phone}", s.contact)
	r.Get("/api/{session}/contact/pn-lid/{pnLid}", s.pnLid)
	r.Get("/api/{session}/profile-pic/{phone}", s.profilePicture)
	r.Get("/api/{session}/profile-status/{phone}", s.contactStatus)
	r.Get("/api/{session}/all-chats", s.chats("all"))
	r.Get("/api/{session}/all-chats-archived", s.chats("archived"))
	r.Post("/api/{session}/list-chats", s.chats("list"))
	r.Get("/api/{session}/chat-by-id/{phone}", s.chats("one"))
	r.Get("/api/{session}/chat-is-online/{phone}", s.observedPresence(false))
	r.Get("/api/{session}/last-seen/{phone}", s.observedPresence(true))
	for _, mode := range []string{"subscribe-presence", "set-online-presence", "typing", "recording", "chat-state"} {
		r.Post("/api/{session}/"+mode, s.setPresence(mode))
	}
	for _, route := range []string{"all-chats-with-messages", "all-messages-in-chat/{phone}", "all-new-messages", "unread-messages", "all-unread-messages", "message-by-id/{messageId}", "load-messages-in-chat/{phone}", "get-messages/{phone}"} {
		r.Get("/api/{session}/"+route, s.messageHistory)
	}
}

func (s *Server) directoryConnection(w http.ResponseWriter, r *http.Request) (directory.Client, *directory.State, bool) {
	w.Header().Set("Cache-Control", "no-store")
	name := cleanSession(chi.URLParam(r, "session"))
	if s.directorySession != nil {
		if client, state, ok := s.directorySession(name); ok {
			return client, state, true
		}
	}
	writeJSON(w, 404, map[string]any{"status": "disconnected", "session": name})
	return nil, nil, false
}

func (s *Server) observeSent(name string, jid types.JID, id string, t time.Time, messages ...*waE2E.Message) {
	if t.IsZero() {
		t = time.Now()
	}
	if s.mgr != nil && len(messages) > 0 {
		if h, ok := s.mgr.Get(name); ok && h.History != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			var self types.JID
			if h.Client.Store != nil && h.Client.Store.ID != nil {
				self = h.Client.Store.ID.ToNonAD()
			}
			err := h.History.Put(ctx, types.MessageInfo{ID: id, Timestamp: t, MessageSource: types.MessageSource{Chat: jid, Sender: self, IsFromMe: true}}, messages[0])
			cancel()
			if err != nil {
				log.Printf("outgoing message persistence failed: %v", err)
			}
		}
	}
	if s.directorySession == nil {
		return
	}
	_, state, ok := s.directorySession(name)
	if !ok || state == nil {
		return
	}
	if t.IsZero() {
		t = time.Now()
	}
	err := state.Observe(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, IsFromMe: true}, ID: id, Timestamp: t}})
	if err != nil {
		log.Printf("outgoing chat index update failed: %v", err)
	}
}

func directoryStatus(err error) (int, string) {
	code, _ := groupErrorStatus(err)
	message := "contact/chat/presence operation failed"
	if errors.Is(err, whatsmeow.ErrNoPushName) {
		code, message = 409, "WhatsApp requires a push name before publishing presence"
	}
	if errors.Is(err, whatsmeow.ErrNoPrivacyToken) {
		code, message = 403, "presence subscription token is unavailable"
	}
	return code, message
}
func directoryError(w http.ResponseWriter, err error) {
	code, message := directoryStatus(err)
	writeJSON(w, code, map[string]any{"status": "error", "message": message})
}

func (s *Server) unsupportedDirectory(capability, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.directoryConnection(w, r); !ok {
			return
		}
		writeJSON(w, 501, map[string]any{"status": "not_supported", "provider": "whatsmeow", "capability": capability, "message": message})
	}
}

func queryBool(r *http.Request, key string) (bool, error) {
	value, ok := r.URL.Query()[key]
	if !ok {
		return false, nil
	}
	if len(value) != 1 {
		return false, errors.New("invalid boolean query parameter")
	}
	return strconv.ParseBool(value[0])
}
func directoryJID(r *http.Request, key string) (types.JID, error) {
	group, err := queryBool(r, "isGroup")
	if err != nil {
		return types.EmptyJID, err
	}
	lid, err := queryBool(r, "isLid")
	if err != nil {
		return types.EmptyJID, err
	}
	newsletter, err := queryBool(r, "isNewsletter")
	if err != nil {
		return types.EmptyJID, err
	}
	if newsletter || (group && lid) {
		return types.EmptyJID, errors.New("unsupported or conflicting chat type")
	}
	value, err := url.PathUnescape(chi.URLParam(r, key))
	if err != nil {
		return types.EmptyJID, err
	}
	return directory.Parse(value, group, lid)
}

func (s *Server) allContacts(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.directoryConnection(w, r)
	if !ok {
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	contacts, err := client.AllContacts(ctx)
	if err != nil {
		directoryError(w, err)
		return
	}
	ids := make([]types.JID, 0, len(contacts))
	for id := range contacts {
		if directory.Supported(id) && id.Server != types.GroupServer {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return groups.ID(ids[i]) < groups.ID(ids[j]) })
	result := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		result = append(result, directory.ContactData(id, contacts[id]))
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": result})
}

func lookupContact(ctx context.Context, client directory.Client, jid types.JID) (types.JID, types.ContactInfo, error) {
	c, err := client.Contact(ctx, jid)
	if err != nil || c.Found {
		return jid, c, err
	}
	other, err := client.Mapping(ctx, jid)
	if err != nil {
		return jid, c, err
	}
	if !other.IsEmpty() {
		c, err = client.Contact(ctx, other)
		return other, c, err
	}
	return jid, c, nil
}
func (s *Server) contact(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.directoryConnection(w, r)
	if !ok {
		return
	}
	jid, err := directoryJID(r, "phone")
	if err != nil || jid.Server == types.GroupServer {
		groupInputError(w, errors.New("invalid contact identifier"))
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	id, c, err := lookupContact(ctx, client, jid)
	if err != nil {
		directoryError(w, err)
		return
	}
	if !c.Found {
		writeJSON(w, 404, map[string]any{"status": "error", "message": "contact has not been synchronized"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": directory.ContactData(id, c)})
}
func (s *Server) pnLid(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.directoryConnection(w, r)
	if !ok {
		return
	}
	jid, err := directoryJID(r, "pnLid")
	if err != nil || jid.Server == types.GroupServer {
		groupInputError(w, errors.New("invalid PN/LID identifier"))
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	other, err := client.Mapping(ctx, jid)
	if err != nil {
		directoryError(w, err)
		return
	}
	result := make(map[string]any)
	for _, id := range []types.JID{jid, other} {
		if id.IsEmpty() {
			continue
		}
		wid := groups.WID(id)
		wid["id"] = id.User
		key := "phoneNumber"
		if id.Server == types.HiddenUserServer {
			key = "lid"
		}
		result[key] = wid
	}
	_, c, err := lookupContact(ctx, client, jid)
	if err != nil {
		directoryError(w, err)
		return
	}
	if c.Found {
		result["contact"] = directory.ContactData(jid, c)
	}
	writeJSON(w, 200, result)
}
func (s *Server) contactStatus(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.directoryConnection(w, r)
	if !ok {
		return
	}
	jid, err := directoryJID(r, "phone")
	if err != nil || jid.Server == types.GroupServer {
		groupInputError(w, errors.New("invalid contact identifier"))
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	info, err := client.GetUserInfo(ctx, []types.JID{jid})
	if err != nil {
		directoryError(w, err)
		return
	}
	user, ok := info[jid]
	if !ok {
		writeJSON(w, 404, map[string]any{"status": "error", "message": "contact profile unavailable"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"id": groups.ID(jid), "status": user.Status}})
}
func (s *Server) profilePicture(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.directoryConnection(w, r)
	if !ok {
		return
	}
	jid, err := directoryJID(r, "phone")
	if err != nil {
		groupInputError(w, err)
		return
	}
	ctx, cancel := groupContext(r)
	defer cancel()
	info, err := client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil {
		directoryError(w, err)
		return
	}
	if info == nil {
		writeJSON(w, 404, map[string]any{"status": "error", "message": "profile picture unavailable"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"id": info.ID, "imgFull": info.URL, "img": info.URL, "url": info.URL}})
}

type chatFilter struct {
	ID              string   `json:"id"`
	Count           *int     `json:"count"`
	Direction       string   `json:"direction"`
	OnlyGroups      bool     `json:"onlyGroups"`
	OnlyUsers       bool     `json:"onlyUsers"`
	OnlyWithUnread  bool     `json:"onlyWithUnreadMessage"`
	OnlyArchived    bool     `json:"onlyArchived"`
	WithLabels      []string `json:"withLabels"`
	OnlyCommunities bool     `json:"onlyCommunities"`
}

func (s *Server) chats(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, state, ok := s.directoryConnection(w, r)
		if !ok {
			return
		}
		var filter chatFilter
		if mode == "list" {
			r.Body = http.MaxBytesReader(w, r.Body, groups.MaxBodyBytes)
			if err := decodeJSON(r.Body, &filter); err != nil && !errors.Is(err, io.EOF) {
				groupInputError(w, err)
				return
			}
			if len(filter.WithLabels) > 20 {
				groupInputError(w, errors.New("at most 20 labels may be filtered"))
				return
			}
			for _, label := range filter.WithLabels {
				if n, err := strconv.ParseUint(label, 10, 16); err != nil || n == 0 {
					groupInputError(w, errors.New("label IDs must be 1..65535"))
					return
				}
			}
			if filter.OnlyGroups && filter.OnlyUsers {
				groupInputError(w, errors.New("onlyGroups and onlyUsers conflict"))
				return
			}
			if filter.Direction != "" && filter.Direction != "after" && filter.Direction != "before" {
				groupInputError(w, errors.New("direction must be before or after"))
				return
			}
			if filter.Count != nil && (*filter.Count < 1 || *filter.Count > 1000) {
				groupInputError(w, errors.New("count must be between 1 and 1000"))
				return
			}
		}
		var target types.JID
		var err error
		if mode == "one" {
			target, err = directoryJID(r, "phone")
			if err != nil {
				groupInputError(w, err)
				return
			}
		}
		if filter.ID != "" {
			target, err = directory.Parse(filter.ID, false, false)
			if err != nil {
				groupInputError(w, err)
				return
			}
		}
		ctx, cancel := groupContext(r)
		defer cancel()
		var labels map[string]map[string]bool
		if len(filter.WithLabels) > 0 {
			store, close, ok := s.archive(w, r)
			if !ok {
				return
			}
			defer close()
			labels, err = store.ChatLabelSets(ctx)
			if err != nil {
				historyError(w, err)
				return
			}
		}
		communities := map[string]bool{}
		if filter.OnlyCommunities {
			gc, ok := s.groupConnection(w, r)
			if !ok {
				return
			}
			joined, e := gc.GetJoinedGroups(ctx)
			if e != nil {
				directoryError(w, e)
				return
			}
			for _, group := range joined {
				if group.IsParent {
					communities[group.JID.String()] = true
				}
			}
		}
		list, err := state.Chats.List(ctx)
		if err != nil {
			directoryError(w, err)
			return
		}
		contacts, err := client.AllContacts(ctx)
		if err != nil {
			directoryError(w, err)
			return
		}
		result := make([]directory.Chat, 0, len(list))
		for _, c := range list {
			if filter.OnlyCommunities && !communities[c.JID] {
				continue
			}
			matches := true
			for _, label := range filter.WithLabels {
				if !labels[c.JID][label] {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			jid, _ := types.ParseJID(c.JID)
			if info, ok := contacts[jid]; ok && jid.Server != types.GroupServer {
				c.Name = directory.ContactData(jid, info)["name"].(string)
			}
			if (mode == "archived" || filter.OnlyArchived) && !c.Archived {
				continue
			}
			if filter.OnlyGroups && jid.Server != types.GroupServer {
				continue
			}
			if filter.OnlyUsers && jid.Server == types.GroupServer {
				continue
			}
			if filter.OnlyWithUnread && c.Unread == 0 {
				continue
			}
			result = append(result, c)
		}
		anchor := -1
		if !target.IsEmpty() {
			for i, c := range result {
				if c.JID == target.String() {
					anchor = i
					break
				}
			}
			if anchor < 0 && target.Server != types.GroupServer {
				other, e := client.Mapping(ctx, target)
				if e != nil {
					directoryError(w, e)
					return
				}
				for i, c := range result {
					if !other.IsEmpty() && c.JID == other.String() {
						anchor = i
						break
					}
				}
			}
			if anchor < 0 {
				writeJSON(w, 404, map[string]any{"status": "error", "message": "chat or pagination anchor not present in synchronized index"})
				return
			}
		}
		if mode == "one" {
			writeJSON(w, 200, directory.ChatData(result[anchor]))
			return
		}
		if mode == "list" {
			count := 20
			if filter.Count != nil {
				count = *filter.Count
			}
			start, end := 0, len(result)
			if anchor >= 0 {
				if filter.Direction == "before" {
					end = anchor
					start = max(0, end-count)
				} else {
					start = anchor
				}
			}
			if filter.Direction != "before" {
				end = min(end, start+count)
			} else if anchor < 0 {
				end = 0
			}
			result = result[start:end]
		}
		data := make([]map[string]any, 0, len(result))
		for _, c := range result {
			data = append(data, directory.ChatData(c))
		}
		w.Header().Set("X-WPP-Chat-Source", "synchronized-index")
		if mode == "all" {
			writeJSON(w, 200, map[string]any{"status": "success", "response": data, "mapper": "chat"})
		} else if mode == "archived" {
			writeJSON(w, 201, data)
		} else {
			writeJSON(w, 200, data)
		}
	}
}

func (s *Server) observedPresence(lastSeen bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, state, ok := s.directoryConnection(w, r)
		if !ok {
			return
		}
		jid, err := directoryJID(r, "phone")
		if err != nil || jid.Server == types.GroupServer {
			groupInputError(w, errors.New("presence requires an individual contact"))
			return
		}
		p, known := state.Presence.Get(jid)
		if !known {
			ctx, cancel := groupContext(r)
			defer cancel()
			other, e := client.Mapping(ctx, jid)
			if e != nil {
				directoryError(w, e)
				return
			}
			if !other.IsEmpty() {
				p, known = state.Presence.Get(other)
			}
		}
		var response any
		if known {
			if lastSeen {
				if !p.LastSeen.IsZero() {
					response = p.LastSeen.Unix()
				} else {
					known = false
				}
			} else {
				response = p.Online
			}
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": response, "known": known, "observedAt": groups.Timestamp(p.Observed)})
	}
}

func (s *Server) setPresence(mode string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, state, ok := s.directoryConnection(w, r)
		if !ok {
			return
		}
		var req struct {
			Phone     json.RawMessage `json:"phone"`
			Value     json.RawMessage `json:"value"`
			IsOnline  json.RawMessage `json:"isOnline"`
			Chatstate json.RawMessage `json:"chatstate"`
			All       bool            `json:"all"`
			IsGroup   bool            `json:"isGroup"`
			IsLid     bool            `json:"isLid"`
			Duration  *int            `json:"duration"`
		}
		if err := readGroupJSON(w, r, &req); err != nil {
			groupInputError(w, err)
			return
		}
		ctx, cancel := groupContext(r)
		defer cancel()
		if mode == "set-online-presence" {
			value, _, err := propertyValue(req.IsOnline, false)
			if err != nil {
				groupInputError(w, err)
				return
			}
			presence := types.PresenceUnavailable
			if value {
				presence = types.PresenceAvailable
			}
			if err = client.SendPresence(ctx, presence); err != nil {
				directoryError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"message": "Set Online Presence Successfully"}})
			return
		}
		value, _, err := propertyValue(req.Value, false)
		if err != nil {
			groupInputError(w, err)
			return
		}
		var ids []types.JID
		if req.All && mode == "subscribe-presence" {
			if req.IsGroup {
				writeJSON(w, 501, map[string]any{"status": "not_supported", "capability": "group_presence", "message": "Global online status is defined only for individual contacts."})
				return
			}
			contacts, e := client.AllContacts(ctx)
			if e != nil {
				directoryError(w, e)
				return
			}
			for id := range contacts {
				if directory.Supported(id) && id.Server != types.GroupServer {
					ids = append(ids, id)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
			if len(ids) > 100 {
				groupInputError(w, errors.New("subscribe all exceeds 100 contacts; send explicit batches"))
				return
			}
		} else {
			values, e := groups.Strings(req.Phone, 100)
			if e != nil {
				groupInputError(w, e)
				return
			}
			seen := make(map[types.JID]bool)
			for _, v := range values {
				id, e := directory.Parse(v, req.IsGroup, req.IsLid)
				if e != nil {
					groupInputError(w, e)
					return
				}
				if !seen[id] {
					ids = append(ids, id)
					seen[id] = true
				}
			}
		}
		if req.Duration != nil && (*req.Duration < 1 || *req.Duration > 10000) {
			groupInputError(w, errors.New("duration must be between 1 and 10000 milliseconds"))
			return
		}
		presence, medium := types.ChatPresenceComposing, types.ChatPresenceMediaText
		if mode == "recording" {
			medium = types.ChatPresenceMediaAudio
		}
		if !value {
			presence = types.ChatPresencePaused
		}
		if mode == "chat-state" {
			var code string
			_ = json.Unmarshal(req.Chatstate, &code)
			if code == "" {
				code = strings.TrimSpace(string(req.Chatstate))
			}
			switch code {
			case "0":
			case "1":
				medium = types.ChatPresenceMediaAudio
			case "2":
				presence = types.ChatPresencePaused
			default:
				groupInputError(w, errors.New("chatstate must be 0 (typing), 1 (recording) or 2 (paused)"))
				return
			}
		}
		if mode == "subscribe-presence" {
			for _, id := range ids {
				if id.Server == types.GroupServer {
					writeJSON(w, 501, map[string]any{"status": "not_supported", "capability": "group_presence", "message": "Subscribe to individual group members instead."})
					return
				}
			}
		}
		completed := make([]string, 0, len(ids))
		for _, id := range ids {
			if mode == "subscribe-presence" {
				err = client.SubscribePresence(ctx, id)
			} else {
				duration := time.Duration(0)
				if req.Duration != nil && presence == types.ChatPresenceComposing {
					duration = time.Duration(*req.Duration) * time.Millisecond
				}
				err = state.Signal(ctx, client, id, presence, medium, duration)
			}
			if err != nil {
				code, message := directoryStatus(err)
				writeJSON(w, code, map[string]any{"status": "error", "message": message, "completed": completed})
				return
			}
			completed = append(completed, groups.ID(id))
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"message": "Presence operation executed", "contacts": completed}})
	}
}
