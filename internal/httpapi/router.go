// Package httpapi exposes the HTTP routes, kept compatible with the Node
// wppconnect-server contract (same paths, same payload field names) so existing
// clients can point at the Go server with minimal changes.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/session"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"golang.org/x/crypto/bcrypt"
)

// Server wires the routes to the session manager.
type Server struct {
	cfg config.Config
	mgr *session.Manager
}

// NewRouter builds the chi router with the compatible endpoints.
func NewRouter(cfg config.Config, mgr *session.Manager) http.Handler {
	s := &Server{cfg: cfg, mgr: mgr}
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/api/dashboard/stats", s.dashboardStats)

	r.Group(func(protected chi.Router) {
		protected.Use(s.auth)
		// Mirrors POST /api/:session/start-session
		protected.Post("/api/{session}/start-session", s.startSession)
		// Mirrors GET /api/:session/status-session
		protected.Get("/api/{session}/status-session", s.statusSession)
		protected.Get("/api/{session}/qrcode-session", s.qrcodeSession)
		// Mirrors POST /api/:session/send-message
		protected.Post("/api/{session}/send-message", s.sendMessage)
		protected.Post("/api/{session}/send-location", s.sendLocation)
		protected.Post("/api/{session}/send-file-base64", s.sendFileBase64)
		// Mirrors POST /api/:session/close-session and logout-session
		protected.Post("/api/{session}/close-session", s.closeSession)
		protected.Post("/api/{session}/logout-session", s.closeSession)
		// Mirrors POST /api/:session/send-image (base64)
		protected.Post("/api/{session}/send-image", s.sendImage)
		// Mirrors POST /api/:session/send-seen
		protected.Post("/api/{session}/send-seen", s.sendSeen)
		// Mirrors GET /api/:session/check-number-status/:phone
		protected.Get("/api/{session}/check-number-status/{phone}", s.checkNumber)
		// Mirrors GET /api/:session/all-groups
		protected.Get("/api/{session}/all-groups", s.allGroups)
		protected.Get("/api/{session}/group-members/{groupId}", s.groupMembers)
		protected.Post("/api/{session}/create-group", s.createGroup)

		s.registerNodeCompatibilityRoutes(r, protected)
	})

	return r
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionName := cleanSession(chi.URLParam(r, "session"))
		auth := r.Header.Get("Authorization")
		token := ""
		if parts := strings.SplitN(auth, " ", 2); len(parts) == 2 {
			token = parts[1]
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"message": "Token is not present. Check your header and try again",
			})
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(token), []byte(sessionName+s.cfg.SecretKey)); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "Check that the Session and Token are correct",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cleanSession(name string) string {
	if idx := strings.Index(name, ":"); idx >= 0 {
		return name[:idx]
	}
	return name
}

// connected returns the live handle for a connected session, or writes the
// standard "disconnected" response and returns false.
func (s *Server) connected(w http.ResponseWriter, name string) (*session.Handle, bool) {
	h, ok := s.mgr.Get(cleanSession(name))
	if !ok || h.Status != session.StatusConnected {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "disconnected", "session": cleanSession(name),
		})
		return nil, false
	}
	return h, true
}

func chatJID(id string, isGroup bool) types.JID {
	id = strings.TrimSpace(id)
	if strings.Contains(id, "@") {
		if jid, err := types.ParseJID(id); err == nil {
			return jid
		}
		id = strings.Split(id, "@")[0]
	}
	if isGroup {
		return types.NewJID(id, types.GroupServer)
	}
	return types.NewJID(id, types.DefaultUserServer)
}

func userJID(phone string) types.JID {
	return chatJID(phone, false)
}

func groupJID(id string) types.JID {
	return chatJID(id, true)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, err := s.mgr.Start(context.Background(), name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.Status), "session": name, "qrcode": h.QRCode,
	})
}

func (s *Server) statusSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "CLOSED", "session": name})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": string(h.Status), "session": name, "urlcode": h.QRCode,
	})
}

func (s *Server) qrcodeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok || h.QRCode == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "QRCODE_NOT_AVAILABLE",
			"message": "QRCode is not available...",
		})
		return
	}
	png, err := qrcode.Encode(h.QRCode, qrcode.Medium, 500)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

type sendMessageReq struct {
	Phone   any    `json:"phone"` // string or []string, like the Node server
	Message string `json:"message"`
	IsGroup bool   `json:"isGroup"`
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	h, ok := s.mgr.Get(name)
	if !ok || h.Status != session.StatusConnected {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "disconnected", "session": name,
		})
		return
	}

	var req sendMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error"})
		return
	}

	for _, phone := range toList(req.Phone) {
		jid := chatJID(phone, req.IsGroup)
		_, err := h.Client.SendMessage(context.Background(), jid, &waE2E.Message{
			Conversation: &req.Message,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"status": "error", "message": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	name := cleanSession(chi.URLParam(r, "session"))
	s.mgr.Close(name)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "session": name})
}

type sendLocationReq struct {
	Phone     any     `json:"phone"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Title     string  `json:"title"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	URL       string  `json:"url"`
	IsGroup   bool    `json:"isGroup"`
}

func (s *Server) sendLocation(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req sendLocationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return
	}
	lat, lng := req.Lat, req.Lng
	if lat == 0 {
		lat = req.Latitude
	}
	if lng == 0 {
		lng = req.Longitude
	}
	if lat == 0 && lng == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "lat/lng is required"})
		return
	}
	title := req.Title
	if title == "" {
		title = req.Name
	}
	msg := &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
		DegreesLatitude:  &lat,
		DegreesLongitude: &lng,
		Name:             strPtr(title),
		Address:          strPtr(req.Address),
		URL:              strPtr(req.URL),
	}}
	if err := s.sendToTargets(h, req.Phone, req.IsGroup, msg); err != nil {
		writeSendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

type sendMediaReq struct {
	Phone    any    `json:"phone"`
	Base64   string `json:"base64"`
	File     string `json:"file"`
	Path     string `json:"path"`
	FileName string `json:"filename"`
	Filename string `json:"fileName"`
	Caption  string `json:"caption"`
	Mimetype string `json:"mimetype"`
	MimeType string `json:"mimeType"`
	IsGroup  bool   `json:"isGroup"`
	PTT      bool   `json:"ptt"`
}

// sendImage uploads a base64 image and sends it. Mirrors POST send-image (the
// base64 variant of the Node server).
func (s *Server) sendImage(w http.ResponseWriter, r *http.Request) {
	s.sendMediaJSON(w, r, whatsmeow.MediaImage, "image/jpeg", false)
}

func (s *Server) sendFileBase64(w http.ResponseWriter, r *http.Request) {
	s.sendMediaJSON(w, r, "", "", false)
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		s.sendMediaMultipart(w, r)
		return
	}
	s.sendMediaJSON(w, r, "", "", false)
}

func (s *Server) sendVoice(w http.ResponseWriter, r *http.Request) {
	s.sendMediaJSON(w, r, whatsmeow.MediaAudio, "audio/ogg", true)
}

func (s *Server) sendMediaJSON(w http.ResponseWriter, r *http.Request, forcedType whatsmeow.MediaType, fallbackMime string, forcePTT bool) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req sendMediaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return
	}

	payload := firstNonEmpty(req.Base64, req.File, req.Path)
	data, detectedMime, err := decodeBase64Payload(payload)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
		return
	}

	mimetype := firstNonEmpty(req.Mimetype, req.MimeType, detectedMime, fallbackMime, "application/octet-stream")
	filename := firstNonEmpty(req.FileName, req.Filename, defaultFilename(mimetype))
	msg, err := s.buildMediaMessage(h, data, forcedType, mimetype, filename, req.Caption, req.PTT || forcePTT)
	if err != nil {
		writeSendError(w, err)
		return
	}
	if err := s.sendToTargets(h, req.Phone, req.IsGroup, msg); err != nil {
		writeSendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

func (s *Server) sendMediaMultipart(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid multipart"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "cannot read file"})
		return
	}

	mimetype := r.FormValue("mimetype")
	if mimetype == "" && header != nil {
		mimetype = header.Header.Get("Content-Type")
	}
	if mimetype == "" {
		mimetype = "application/octet-stream"
	}
	filename := r.FormValue("filename")
	if filename == "" && header != nil {
		filename = header.Filename
	}
	msg, err := s.buildMediaMessage(h, data, "", mimetype, filename, r.FormValue("caption"), false)
	if err != nil {
		writeSendError(w, err)
		return
	}
	isGroup := strings.EqualFold(r.FormValue("isGroup"), "true")
	if err := s.sendToTargets(h, r.FormValue("phone"), isGroup, msg); err != nil {
		writeSendError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success"})
}

type sendSeenReq struct {
	Phone string `json:"phone"`
}

// sendSeen marks a chat as read. Mirrors POST send-seen.
func (s *Server) sendSeen(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req sendSeenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error"})
		return
	}
	// MarkRead with no specific message IDs marks the chat as seen.
	err := h.Client.MarkRead(
		context.Background(),
		nil,
		time.Now(),
		userJID(req.Phone),
		types.EmptyJID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// checkNumber reports whether a phone is registered on WhatsApp.
// Mirrors GET check-number-status/:phone.
func (s *Server) checkNumber(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	phone := chi.URLParam(r, "phone")
	resp, err := h.Client.IsOnWhatsApp(
		context.Background(),
		[]string{"+" + strings.TrimPrefix(phone, "+")},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	numberExists := false
	var wid string
	if len(resp) > 0 {
		numberExists = resp[0].IsIn
		wid = resp[0].JID.String()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"response": map[string]any{"numberExists": numberExists, "id": wid},
	})
}

// allGroups lists the joined groups. Mirrors GET all-groups.
func (s *Server) allGroups(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	groups, err := h.Client.GetJoinedGroups(context.Background())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, map[string]any{
			"id":           g.JID.String(),
			"name":         g.Name,
			"participants": len(g.Participants),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": out})
}

func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request) {
	s.groupInfo(w, r, "members")
}

func (s *Server) groupInfo(w http.ResponseWriter, r *http.Request, mode string) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	groupID := firstNonEmpty(chi.URLParam(r, "groupId"), chi.URLParam(r, "id"))
	if groupID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "groupId is required"})
		return
	}
	info, err := h.Client.GetGroupInfo(context.Background(), groupJID(groupID))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	switch mode {
	case "members":
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": serializeParticipants(info.Participants, false)})
	case "memberIDs":
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": participantIDs(info.Participants, false)})
	case "admins":
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": serializeParticipants(info.Participants, true)})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": serializeGroup(info)})
	}
}

type createGroupReq struct {
	Name         string `json:"name"`
	Subject      string `json:"subject"`
	GroupName    string `json:"groupName"`
	Participants any    `json:"participants"`
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req createGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return
	}
	name := firstNonEmpty(req.Name, req.Subject, req.GroupName)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "name is required"})
		return
	}
	participants := make([]types.JID, 0)
	for _, phone := range toList(req.Participants) {
		participants = append(participants, userJID(phone))
	}
	info, err := h.Client.CreateGroup(context.Background(), whatsmeow.ReqCreateGroup{
		Name:         name,
		Participants: participants,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success", "response": serializeGroup(info)})
}

type groupActionReq struct {
	GroupID      string `json:"groupId"`
	ID           string `json:"id"`
	Participants any    `json:"participants"`
	Phones       any    `json:"phones"`
	Phone        any    `json:"phone"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	Subject      string `json:"subject"`
	Description  string `json:"description"`
}

func (s *Server) leaveGroup(w http.ResponseWriter, r *http.Request) {
	h, req, ok := s.groupActionRequest(w, r)
	if !ok {
		return
	}
	if err := h.Client.LeaveGroup(context.Background(), groupJID(firstNonEmpty(req.GroupID, req.ID))); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) updateGroupParticipants(action whatsmeow.ParticipantChange) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, req, ok := s.groupActionRequest(w, r)
		if !ok {
			return
		}
		participants := make([]types.JID, 0)
		for _, phone := range toList(firstAny(req.Participants, req.Phones, req.Phone)) {
			participants = append(participants, userJID(phone))
		}
		if len(participants) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "participants is required"})
			return
		}
		resp, err := h.Client.UpdateGroupParticipants(context.Background(), groupJID(firstNonEmpty(req.GroupID, req.ID)), participants, action)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": serializeParticipants(resp, false)})
	}
}

func (s *Server) setGroupSubject(w http.ResponseWriter, r *http.Request) {
	h, req, ok := s.groupActionRequest(w, r)
	if !ok {
		return
	}
	subject := firstNonEmpty(req.Subject, req.Title, req.Name)
	if subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "subject is required"})
		return
	}
	if err := h.Client.SetGroupName(context.Background(), groupJID(firstNonEmpty(req.GroupID, req.ID)), subject); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) setGroupDescription(w http.ResponseWriter, r *http.Request) {
	h, req, ok := s.groupActionRequest(w, r)
	if !ok {
		return
	}
	if req.Description == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "description is required"})
		return
	}
	if err := h.Client.SetGroupDescription(context.Background(), groupJID(firstNonEmpty(req.GroupID, req.ID)), req.Description); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) groupActionRequest(w http.ResponseWriter, r *http.Request) (*session.Handle, groupActionReq, bool) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return nil, groupActionReq{}, false
	}
	var req groupActionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return nil, groupActionReq{}, false
	}
	if firstNonEmpty(req.GroupID, req.ID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "groupId is required"})
		return nil, groupActionReq{}, false
	}
	return h, req, true
}

type phoneReq struct {
	Phone   any    `json:"phone"`
	Number  any    `json:"number"`
	ID      any    `json:"id"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Online  *bool  `json:"online"`
	IsGroup bool   `json:"isGroup"`
}

func (s *Server) subscribePresence(w http.ResponseWriter, r *http.Request) {
	h, req, ok := s.phoneRequest(w, r)
	if !ok {
		return
	}
	for _, phone := range toList(firstAny(req.Phone, req.Number, req.ID)) {
		if err := h.Client.SubscribePresence(context.Background(), chatJID(phone, req.IsGroup)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) setOnlinePresence(w http.ResponseWriter, r *http.Request) {
	h, req, ok := s.phoneRequest(w, r)
	if !ok {
		return
	}
	state := types.PresenceAvailable
	if req.Online != nil && !*req.Online {
		state = types.PresenceUnavailable
	}
	if strings.EqualFold(req.State, "unavailable") || strings.EqualFold(req.Status, "unavailable") {
		state = types.PresenceUnavailable
	}
	if err := h.Client.SendPresence(context.Background(), state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) sendChatPresence(state types.ChatPresence, media types.ChatPresenceMedia) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, req, ok := s.phoneRequest(w, r)
		if !ok {
			return
		}
		phones := toList(firstAny(req.Phone, req.Number, req.ID))
		if len(phones) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "phone is required"})
			return
		}
		for _, phone := range phones {
			if err := h.Client.SendChatPresence(context.Background(), chatJID(phone, req.IsGroup), state, media); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
	}
}

func (s *Server) contactInfo(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	phone := chi.URLParam(r, "phone")
	info, err := h.Client.GetUserInfo(context.Background(), []types.JID{userJID(phone)})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": serializeUserInfo(info)})
}

func (s *Server) profilePicture(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	pic, err := h.Client.GetProfilePictureInfo(context.Background(), userJID(chi.URLParam(r, "phone")), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": pic})
}

func (s *Server) profileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.setProfileStatus(w, r)
		return
	}
	s.contactInfo(w, r)
}

func (s *Server) setProfileStatus(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	var req struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return
	}
	status := firstNonEmpty(req.Status, req.Message)
	if status == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "status is required"})
		return
	}
	if err := h.Client.SetStatusMessage(context.Background(), status); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) blocklist(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	list, err := h.Client.GetBlocklist(context.Background())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	items := make([]string, 0, len(list.JIDs))
	for _, jid := range list.JIDs {
		items = append(items, jid.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": items, "dhash": list.DHash})
}

func (s *Server) updateBlocklist(action events.BlocklistChangeAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, req, ok := s.phoneRequest(w, r)
		if !ok {
			return
		}
		phones := toList(firstAny(req.Phone, req.Number, req.ID))
		if len(phones) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "phone is required"})
			return
		}
		var list *types.Blocklist
		var err error
		for _, phone := range phones {
			list, err = h.Client.UpdateBlocklist(context.Background(), userJID(phone), action)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": list})
	}
}

func (s *Server) ownPhoneNumber(w http.ResponseWriter, r *http.Request) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	if h.Client.Store.ID == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "response": h.Client.Store.ID.String()})
}

func (s *Server) phoneRequest(w http.ResponseWriter, r *http.Request) (*session.Handle, phoneReq, bool) {
	h, ok := s.connected(w, chi.URLParam(r, "session"))
	if !ok {
		return nil, phoneReq{}, false
	}
	var req phoneReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.Body != http.NoBody {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": "invalid json"})
		return nil, phoneReq{}, false
	}
	return h, req, true
}

func (s *Server) dashboardStats(w http.ResponseWriter, _ *http.Request) {
	handles := s.mgr.List()
	sessions := make([]map[string]any, 0, len(handles))
	connected := 0
	for _, h := range handles {
		if h.Status == session.StatusConnected {
			connected++
		}
		sessions = append(sessions, map[string]any{
			"session":   h.Name,
			"origin":    "unknown",
			"runtime":   "wppconnect-server-go",
			"provider":  "whatsmeow",
			"status":    h.Status,
			"connected": h.Status == session.StatusConnected,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
		"overview": map[string]any{
			"runtime":       "wppconnect-server-go",
			"sessions":      len(sessions),
			"totalSessions": len(sessions),
			"connected":     connected,
			"disconnected":  len(sessions) - connected,
			"byProvider":    map[string]int{"whatsmeow": len(sessions)},
		},
		"sessions": sessions,
	})
}

func (s *Server) notSupported(capability string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"status":     "not_supported",
			"runtime":    "wppconnect-server-go",
			"provider":   "whatsmeow",
			"capability": capability,
			"method":     r.Method,
			"route":      r.URL.Path,
			"message":    "Capability is not supported by wppconnect-server-go yet.",
		})
	}
}

func (s *Server) sendToTargets(h *session.Handle, phones any, isGroup bool, msg *waE2E.Message) error {
	targets := toList(phones)
	if len(targets) == 0 {
		return errors.New("phone is required")
	}
	for _, phone := range targets {
		if _, err := h.Client.SendMessage(context.Background(), chatJID(phone, isGroup), msg); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) buildMediaMessage(h *session.Handle, data []byte, forcedType whatsmeow.MediaType, mimetype, filename, caption string, ptt bool) (*waE2E.Message, error) {
	if len(data) == 0 {
		return nil, errors.New("base64/file is required")
	}
	mediaType := forcedType
	if mediaType == "" {
		mediaType = mediaTypeFromMime(mimetype)
	}
	uploaded, err := h.Client.Upload(context.Background(), data, mediaType)
	if err != nil {
		return nil, err
	}
	switch mediaType {
	case whatsmeow.MediaImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption:       strPtr(caption),
			Mimetype:      strPtr(mimetype),
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}}, nil
	case whatsmeow.MediaVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption:       strPtr(caption),
			Mimetype:      strPtr(mimetype),
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}}, nil
	case whatsmeow.MediaAudio:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype:      strPtr(mimetype),
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
			PTT:           &ptt,
		}}, nil
	default:
		if filename == "" {
			filename = defaultFilename(mimetype)
		}
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Caption:       strPtr(caption),
			Mimetype:      strPtr(mimetype),
			Title:         strPtr(filename),
			FileName:      strPtr(filename),
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}}, nil
	}
}

func writeSendError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	code := http.StatusInternalServerError
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") {
		code = http.StatusBadRequest
	}
	writeJSON(w, code, map[string]any{"status": "error", "message": err.Error()})
}

func decodeBase64Payload(payload string) ([]byte, string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, "", errors.New("base64/file is required")
	}
	mimetype := ""
	if i := strings.Index(payload, ","); strings.HasPrefix(payload, "data:") && i >= 0 {
		meta := strings.TrimPrefix(payload[:i], "data:")
		if semi := strings.Index(meta, ";"); semi >= 0 {
			mimetype = meta[:semi]
		}
		payload = payload[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errors.New("invalid base64")
	}
	return data, mimetype, nil
}

func mediaTypeFromMime(mimetype string) whatsmeow.MediaType {
	switch {
	case strings.HasPrefix(mimetype, "image/"):
		return whatsmeow.MediaImage
	case strings.HasPrefix(mimetype, "video/"):
		return whatsmeow.MediaVideo
	case strings.HasPrefix(mimetype, "audio/"):
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

func defaultFilename(mimetype string) string {
	exts, _ := mime.ExtensionsByType(mimetype)
	if len(exts) > 0 {
		return "file" + exts[0]
	}
	if ext := filepath.Ext(mimetype); ext != "" {
		return "file" + ext
	}
	return "file.bin"
}

func serializeGroup(g *types.GroupInfo) map[string]any {
	if g == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                g.JID.String(),
		"name":              g.Name,
		"owner":             g.OwnerJID.String(),
		"creation":          g.GroupCreated,
		"participants":      serializeParticipants(g.Participants, false),
		"participantsCount": len(g.Participants),
		"isAnnounce":        g.IsAnnounce,
		"isLocked":          g.IsLocked,
	}
}

func serializeUserInfo(info map[types.JID]types.UserInfo) map[string]any {
	out := make(map[string]any, len(info))
	for jid, user := range info {
		item := map[string]any{
			"id":        jid.String(),
			"status":    user.Status,
			"pictureId": user.PictureID,
			"lid":       user.LID.String(),
		}
		if user.VerifiedName != nil && user.VerifiedName.Details != nil {
			item["verifiedName"] = user.VerifiedName.Details.GetVerifiedName()
		}
		devices := make([]string, 0, len(user.Devices))
		for _, device := range user.Devices {
			devices = append(devices, device.String())
		}
		item["devices"] = devices
		out[jid.String()] = item
	}
	return out
}

func serializeParticipants(participants []types.GroupParticipant, adminsOnly bool) []map[string]any {
	out := make([]map[string]any, 0, len(participants))
	for _, p := range participants {
		if adminsOnly && !p.IsAdmin && !p.IsSuperAdmin {
			continue
		}
		out = append(out, map[string]any{
			"id":           p.JID.String(),
			"phoneNumber":  p.PhoneNumber.String(),
			"isAdmin":      p.IsAdmin,
			"isSuperAdmin": p.IsSuperAdmin,
			"displayName":  p.DisplayName,
			"error":        p.Error,
		})
	}
	return out
}

func participantIDs(participants []types.GroupParticipant, adminsOnly bool) []string {
	out := make([]string, 0, len(participants))
	for _, p := range participants {
		if adminsOnly && !p.IsAdmin && !p.IsSuperAdmin {
			continue
		}
		out = append(out, p.JID.String())
	}
	return out
}

func toList(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func firstAny(values ...any) any {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if v != "" {
				return value
			}
		case nil:
		default:
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
