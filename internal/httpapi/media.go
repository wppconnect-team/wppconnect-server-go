package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow/types"
)

type mediaRequest struct {
	GroupID   json.RawMessage `json:"groupId"`
	Phone     json.RawMessage `json:"phone"`
	Base64    string          `json:"base64"`
	Base64Ptt string          `json:"base64Ptt"`
	Mimetype  string          `json:"mimetype"`
	Filename  string          `json:"filename"`
	Caption   string          `json:"caption"`
	Message   string          `json:"message"`
	Path      string          `json:"path"`
	IsGroup   bool            `json:"isGroup"`
}

func (s *Server) sendImage(w http.ResponseWriter, r *http.Request) { s.sendMedia(w, r, media.Image) }
func (s *Server) sendFile(w http.ResponseWriter, r *http.Request)  { s.sendMedia(w, r, media.Document) }
func (s *Server) sendAudio(w http.ResponseWriter, r *http.Request) { s.sendMedia(w, r, media.Audio) }
func (s *Server) sendVoice(w http.ResponseWriter, r *http.Request) { s.sendMedia(w, r, media.Voice) }

func mediaError(w http.ResponseWriter, err error, fallback string) {
	code, message := http.StatusBadRequest, err.Error()
	var maxBody *http.MaxBytesError
	switch {
	case errors.As(err, &maxBody):
		code, message = http.StatusRequestEntityTooLarge, "request body exceeds "+strconv.FormatInt(maxBody.Limit, 10)+" bytes"
	case errors.Is(err, media.ErrTooLarge):
		code, message = http.StatusRequestEntityTooLarge, media.ErrTooLarge.Error()
	case errors.Is(err, context.DeadlineExceeded):
		code, message = http.StatusGatewayTimeout, "media operation timed out"
	case errors.Is(err, context.Canceled):
		code, message = http.StatusRequestTimeout, "media operation canceled"
	case fallback != "":
		code, message = http.StatusBadGateway, fallback
	}
	writeJSON(w, code, map[string]any{"status": "error", "message": message})
}

func (s *Server) acquireMedia(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.mediaSlots <- struct{}{}:
		return true
	case <-r.Context().Done():
		mediaError(w, r.Context().Err(), "")
		return false
	}
}

func (s *Server) mediaConnection(w http.ResponseWriter, r *http.Request) (media.Client, *media.Store, bool) {
	name := cleanSession(chi.URLParam(r, "session"))
	client, store, ok := s.mediaSession(name)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"status": "disconnected", "session": name})
	}
	return client, store, ok
}

func decodeJSON(r io.Reader, value any) error {
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("expected one JSON object")
	}
	return nil
}

func readMedia(w http.ResponseWriter, r *http.Request) (mediaRequest, []byte, error) {
	var req mediaRequest
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxBodyBytes)
	if r.ContentLength > media.MaxBodyBytes {
		return req, nil, media.ErrTooLarge
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil && r.Header.Get("Content-Type") != "" {
		return req, nil, errors.New("invalid Content-Type")
	}
	var data []byte
	if contentType == "multipart/form-data" {
		if err := r.ParseMultipartForm(media.MaxBytes); err != nil {
			return req, nil, err
		}
		defer r.MultipartForm.RemoveAll()
		phone := r.FormValue("phone")
		req.GroupID, _ = json.Marshal(r.FormValue("groupId"))
		if strings.HasPrefix(phone, "[") {
			req.Phone = json.RawMessage(phone)
		} else {
			req.Phone, _ = json.Marshal(phone)
		}
		if value := r.FormValue("isGroup"); value != "" {
			req.IsGroup, err = strconv.ParseBool(value)
			if err != nil {
				return req, nil, errors.New("invalid isGroup")
			}
		}
		req.Filename, req.Caption, req.Message = r.FormValue("filename"), r.FormValue("caption"), r.FormValue("message")
		req.Mimetype = r.FormValue("mimetype")
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			return req, nil, errors.New("expected exactly one file field")
		}
		if files[0].Size > media.MaxBytes {
			return req, nil, media.ErrTooLarge
		}
		file, err := files[0].Open()
		if err != nil {
			return req, nil, err
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, media.MaxBytes+1))
		if err != nil {
			return req, nil, err
		}
		if req.Filename == "" {
			req.Filename = files[0].Filename
		}
		// Browsers often send application/octet-stream, so sniff the bytes.
	} else {
		if contentType != "" && contentType != "application/json" {
			return req, nil, errors.New("expected application/json or multipart/form-data")
		}
		if err := decodeJSON(r.Body, &req); err != nil {
			return req, nil, err
		}
		value := req.Base64
		if req.Base64Ptt != "" {
			value = req.Base64Ptt
		}
		if value == "" && strings.HasPrefix(req.Path, "data:") {
			value = req.Path
		}
		if value == "" {
			return req, nil, errors.New("base64/base64Ptt or a multipart file is required; filesystem paths and remote URLs are not supported")
		}
		var declared string
		data, declared, err = media.Decode(value)
		if err != nil {
			return req, nil, err
		}
		if declared != "" {
			if req.Mimetype != "" && req.Mimetype != declared {
				return req, nil, errors.New("conflicting data URL and mimetype")
			}
			req.Mimetype = declared
		}
	}
	if len(data) > media.MaxBytes {
		return req, nil, media.ErrTooLarge
	}
	return req, data, nil
}

func destinations(phone json.RawMessage, group bool) ([]types.JID, error) {
	var values []string
	var single string
	if err := json.Unmarshal(phone, &single); err == nil {
		values = []string{single}
	} else if err := json.Unmarshal(phone, &values); err != nil {
		return nil, errors.New("phone must be a string or an array of strings")
	}
	if len(values) == 0 || len(values) > 100 {
		return nil, errors.New("phone must contain between 1 and 100 recipients")
	}
	out := make([]types.JID, 0, len(values))
	for _, value := range values {
		user, server, hasServer := strings.Cut(value, "@")
		target := types.DefaultUserServer
		if group {
			target = types.GroupServer
		}
		if user == "" || (hasServer && server != target && !(server == "c.us" && !group)) {
			return nil, errors.New("invalid recipient JID")
		}
		for _, c := range user {
			if (c < '0' || c > '9') && !(group && c == '-') {
				return nil, errors.New("phone must contain digits (and '-' for legacy group IDs)")
			}
		}
		out = append(out, types.NewJID(user, target))
	}
	return out, nil
}

func (s *Server) sendMedia(w http.ResponseWriter, r *http.Request, kind media.Kind) {
	client, store, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	req, data, err := readMedia(w, r)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	recipients, err := destinations(req.Phone, req.IsGroup)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	mimetype, err := media.MIME(data, req.Mimetype, kind)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	uploaded, err := client.Upload(ctx, data, media.UploadType(kind))
	if err != nil {
		mediaError(w, err, "media upload failed")
		return
	}
	uploaded.FileLength = uint64(len(data))
	caption := req.Caption
	if req.Message != "" {
		caption = req.Message
	}
	filename := media.Filename(req.Filename)
	results := make([]map[string]any, 0, len(recipients))
	for _, jid := range recipients {
		msg := media.Message(kind, uploaded, mimetype, filename, caption)
		resp, err := client.SendMessage(ctx, jid, msg)
		if err != nil {
			mediaError(w, err, "media send failed; earlier recipients may have received the message")
			return
		}
		store.Remember(resp.ID, msg)
		s.observeSent(cleanSession(chi.URLParam(r, "session")), jid, resp.ID, resp.Timestamp)
		results = append(results, map[string]any{"id": resp.ID, "to": jid.String(), "mimetype": mimetype, "filename": filename})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status": "success", "response": results, "mapper": "return"})
}

// messageID accepts the Node string or a webhook object, but never trusts
// client-supplied URLs, encryption keys or download descriptors.
func messageID(value json.RawMessage) string {
	var id string
	if json.Unmarshal(value, &id) == nil {
		return id
	}
	var object struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(value, &object) == nil {
		if json.Unmarshal(object.ID, &id) == nil {
			return id
		}
	}
	return ""
}

func (s *Server) downloadMedia(w http.ResponseWriter, r *http.Request) {
	client, store, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	id := chi.URLParam(r, "messageId")
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var req struct {
			MessageID json.RawMessage `json:"messageId"`
		}
		if err := decodeJSON(r.Body, &req); err != nil {
			mediaError(w, err, "")
			return
		}
		id = messageID(req.MessageID)
	}
	if id == "" || len(id) > 256 {
		mediaError(w, errors.New("messageId is required and must fit in 256 bytes"), "")
		return
	}
	a, ok := store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"status": "error", "message": "media message not found in this session or expired"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	data, err := media.Download(ctx, client, a)
	if err != nil {
		mediaError(w, err, "media download failed")
		return
	}
	mimetype := a.Mimetype
	if _, _, err := mime.ParseMediaType(mimetype); err != nil {
		mimetype = http.DetectContentType(data)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"base64": base64.StdEncoding.EncodeToString(data), "mimetype": mimetype})
}
