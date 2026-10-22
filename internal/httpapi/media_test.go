package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wppconnect-team/wppconnect-server-go/internal/auth"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type fakeMediaClient struct {
	uploads      int
	uploadType   whatsmeow.MediaType
	uploaded     []byte
	sent         []*waE2E.Message
	to           []types.JID
	uploadErr    error
	sendErr      error
	downloadErr  error
	downloads    int
	downloadData []byte
	downloadFunc func(whatsmeow.File) error
}

func (f *fakeMediaClient) Upload(ctx context.Context, data []byte, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("upload lacks deadline")
	}
	f.uploads++
	f.uploadType, f.uploaded = kind, bytes.Clone(data)
	return whatsmeow.UploadResponse{DirectPath: "/media", URL: "https://mmg.whatsapp.net/media", MediaKey: []byte("key"), FileEncSHA256: []byte("encrypted-hash"), FileSHA256: []byte("hash")}, f.uploadErr
}

func (f *fakeMediaClient) SendMessage(ctx context.Context, to types.JID, msg *waE2E.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	if err := ctx.Err(); err != nil {
		return whatsmeow.SendResponse{}, err
	}
	f.sent, f.to = append(f.sent, msg), append(f.to, to)
	return whatsmeow.SendResponse{ID: fmt.Sprintf("sent-%d", len(f.sent))}, f.sendErr
}

func (f *fakeMediaClient) DownloadToFile(ctx context.Context, _ whatsmeow.DownloadableMessage, file whatsmeow.File) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("download lacks deadline")
	}
	f.downloads++
	if f.downloadErr != nil {
		return f.downloadErr
	}
	if f.downloadFunc != nil {
		return f.downloadFunc(file)
	}
	// Hide Reader.WriteTo to exercise the destination's ReaderFrom fast path.
	_, err := io.Copy(file, struct{ io.Reader }{bytes.NewReader(f.downloadData)})
	return err
}

type mediaHarness struct {
	router http.Handler
	client *fakeMediaClient
	stores map[string]*media.Store
	tokens map[string]string
}

func newMediaHarness(t *testing.T) *mediaHarness {
	t.Helper()
	h := &mediaHarness{client: &fakeMediaClient{}, stores: map[string]*media.Store{"alpha": media.NewStore(), "beta": media.NewStore()}, tokens: map[string]string{}}
	for name := range h.stores {
		token, err := auth.Generate(name, "test-secret")
		if err != nil {
			t.Fatal(err)
		}
		h.tokens[name] = token
	}
	s := &Server{cfg: config.Config{SecretKey: "test-secret"}, mediaSession: func(name string) (media.Client, *media.Store, bool) {
		store, ok := h.stores[name]
		return h.client, store, ok
	}}
	h.router = s.router()
	return h
}

func (h *mediaHarness) call(method, path, token, contentType string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	return w
}

func (h *mediaHarness) post(t *testing.T, route string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return h.call(http.MethodPost, "/api/alpha/"+route, h.tokens["alpha"], "application/json", bytes.NewReader(data))
}

func pngData(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSendMediaContracts(t *testing.T) {
	image := pngData(t)
	ogg := append([]byte("OggS\x00\x02"), append(make([]byte, 22), []byte("OpusHead\x01\x01")...)...)
	for _, tc := range []struct {
		name, route, field, mime string
		data                     []byte
		kind                     whatsmeow.MediaType
		voice                    bool
	}{
		{"image", "send-image", "base64", "image/png", image, whatsmeow.MediaImage, false},
		{"file", "send-file", "base64", "application/pdf", []byte("%PDF-1.7\nexample"), whatsmeow.MediaDocument, false},
		{"file alias", "send-file-base64", "base64", "text/plain", []byte("plain text"), whatsmeow.MediaDocument, false},
		{"voice", "send-voice-base64", "base64Ptt", "audio/ogg; codecs=opus", ogg, whatsmeow.MediaAudio, true},
		{"voice alias", "send-voice", "base64Ptt", "audio/ogg; codecs=opus", ogg, whatsmeow.MediaAudio, true},
		{"audio", "send-audio-base64", "base64", "audio/mpeg", []byte("ID3\x04\x00\x00\x00\x00\x00\x00audio"), whatsmeow.MediaAudio, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMediaHarness(t)
			w := h.post(t, tc.route, map[string]any{"phone": []string{"5511999999999", "5521999999999@c.us"}, tc.field: base64.StdEncoding.EncodeToString(tc.data), "filename": "..\\secret/report.pdf", "caption": "caption"})
			if w.Code != http.StatusCreated {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if h.client.uploads != 1 || h.client.uploadType != tc.kind || !bytes.Equal(h.client.uploaded, tc.data) {
				t.Fatal("wrong upload")
			}
			if len(h.client.sent) != 2 || h.client.to[1].String() != "5521999999999@s.whatsapp.net" {
				t.Fatal("wrong recipients")
			}
			msg := h.client.sent[0]
			switch tc.kind {
			case whatsmeow.MediaImage:
				m := msg.GetImageMessage()
				if m.GetMimetype() != tc.mime || m.GetCaption() != "caption" || m.GetFileLength() != uint64(len(tc.data)) {
					t.Fatalf("wrong image: %v", m)
				}
			case whatsmeow.MediaDocument:
				m := msg.GetDocumentMessage()
				if m.GetMimetype() != tc.mime || m.GetFileName() != "report.pdf" || m.GetCaption() != "caption" {
					t.Fatalf("wrong document: %v", m)
				}
			case whatsmeow.MediaAudio:
				m := msg.GetAudioMessage()
				if m.GetMimetype() != tc.mime || m.GetPTT() != tc.voice {
					t.Fatalf("wrong audio: %v", m)
				}
			}
			if _, ok := h.stores["alpha"].Get("sent-1"); !ok {
				t.Fatal("sent media was not indexed")
			}
			var result struct {
				Status, Mapper string
				Response       []struct{ ID string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Status != "success" || result.Mapper != "return" || len(result.Response) != 2 || result.Response[0].ID == "" {
				t.Fatalf("unexpected Node response: %s", w.Body.String())
			}
		})
	}
}

func TestMediaDataURLGroupAndMultipart(t *testing.T) {
	h := newMediaHarness(t)
	data := pngData(t)
	w := h.post(t, "send-image", map[string]any{"phone": "12345-67890@g.us", "isGroup": true, "base64": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)})
	if w.Code != 201 || h.client.to[0].Server != types.GroupServer {
		t.Fatalf("group data URL: %d %s", w.Code, w.Body.String())
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("phone", "5511999999999")
	_ = form.WriteField("message", "Node message alias")
	file, err := form.CreateFormFile("file", "document.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("hello"))
	_ = form.Close()
	w = h.call(http.MethodPost, "/api/alpha/send-file", h.tokens["alpha"], form.FormDataContentType(), &body)
	if w.Code != 201 {
		t.Fatalf("multipart: %d %s", w.Code, w.Body.String())
	}
	doc := h.client.sent[1].GetDocumentMessage()
	if doc.GetMimetype() != "text/plain" || doc.GetCaption() != "Node message alias" || doc.GetFileName() != "document.txt" {
		t.Fatalf("wrong multipart document: %v", doc)
	}
}

func TestMediaInputRejections(t *testing.T) {
	h := newMediaHarness(t)
	valid := base64.StdEncoding.EncodeToString(pngData(t))
	for _, tc := range []struct {
		name, route string
		body        any
		status      int
	}{
		{"base64", "send-image", map[string]any{"phone": "123", "base64": "###"}, 400},
		{"empty", "send-file", map[string]any{"phone": "123", "base64": ""}, 400},
		{"mime mismatch", "send-image", map[string]any{"phone": "123", "base64": "data:image/jpeg;base64," + valid}, 400},
		{"fake image", "send-image", map[string]any{"phone": "123", "base64": base64.StdEncoding.EncodeToString([]byte("not an image"))}, 400},
		{"invalid mime", "send-file", map[string]any{"phone": "123", "base64": valid, "mimetype": "bad type"}, 400},
		{"missing phone", "send-image", map[string]any{"base64": valid}, 400},
		{"mixed phone", "send-image", map[string]any{"phone": []any{"123", 42}, "base64": valid}, 400},
		{"empty phone", "send-image", map[string]any{"phone": []string{}, "base64": valid}, 400},
		{"bad jid", "send-image", map[string]any{"phone": "123@evil.test", "base64": valid}, 400},
		{"wrong group", "send-image", map[string]any{"phone": "123@g.us", "base64": valid}, 400},
		{"voice codec", "send-voice-base64", map[string]any{"phone": "123", "base64Ptt": valid}, 400},
		{"local path", "send-file", map[string]any{"phone": "123", "path": "/etc/passwd"}, 400},
		{"remote path", "send-voice", map[string]any{"phone": "123", "path": "http://127.0.0.1/secret"}, 400},
		{"decoded size", "send-file", map[string]any{"phone": "123", "base64": base64.StdEncoding.EncodeToString(make([]byte, media.MaxBytes+1))}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := h.post(t, tc.route, tc.body)
			if w.Code != tc.status {
				t.Fatalf("want %d, got %d: %s", tc.status, w.Code, w.Body.String())
			}
		})
	}
	w := h.call(http.MethodPost, "/api/alpha/send-file", h.tokens["alpha"], "application/json", strings.NewReader(`{"phone":"123","base64":"aGk="} {}`))
	if w.Code != 400 {
		t.Fatalf("trailing JSON accepted: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/alpha/send-file", strings.NewReader(""))
	r.Header.Set("Authorization", "Bearer "+h.tokens["alpha"])
	r.ContentLength = media.MaxBodyBytes + 1
	w = httptest.NewRecorder()
	h.router.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("Content-Length limit: %d", w.Code)
	}
	if h.client.uploads != 0 {
		t.Fatal("invalid input reached WhatsApp")
	}
}

func TestMediaTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		stage  string
		err    error
		status int
	}{
		{"upload", errors.New("network down"), 502},
		{"send", errors.New("network down"), 502},
		{"upload", context.DeadlineExceeded, 504},
		{"upload", context.Canceled, 408},
	} {
		t.Run(fmt.Sprint(tc.stage, tc.status), func(t *testing.T) {
			h := newMediaHarness(t)
			if tc.stage == "upload" {
				h.client.uploadErr = tc.err
			} else {
				h.client.sendErr = tc.err
			}
			w := h.post(t, "send-file", map[string]any{"phone": "123", "base64": "aGk="})
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if _, ok := h.stores["alpha"].Get("sent-1"); ok {
				t.Fatal("failed send indexed")
			}
		})
	}
}

func TestMediaStreamingBodyAndMultipartLimits(t *testing.T) {
	h := newMediaHarness(t)
	body := io.MultiReader(strings.NewReader(`{"phone":"123","base64":"aGk="}`), strings.NewReader(strings.Repeat(" ", media.MaxBodyBytes)))
	w := h.call(http.MethodPost, "/api/alpha/send-file", h.tokens["alpha"], "application/json", body)
	if w.Code != 413 {
		t.Fatalf("body without Content-Length: %d %s", w.Code, w.Body.String())
	}
	var multipartBody bytes.Buffer
	form := multipart.NewWriter(&multipartBody)
	_ = form.WriteField("phone", "123")
	file, err := form.CreateFormFile("file", "too-large.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(make([]byte, media.MaxBytes+1))
	_ = form.Close()
	w = h.call(http.MethodPost, "/api/alpha/send-file", h.tokens["alpha"], form.FormDataContentType(), &multipartBody)
	if w.Code != 413 {
		t.Fatalf("multipart file limit: %d %s", w.Code, w.Body.String())
	}
	if h.client.uploads != 0 {
		t.Fatal("oversized input reached upload")
	}
}

func TestDownloadContractsAndSessionIsolation(t *testing.T) {
	h := newMediaHarness(t)
	data := []byte("%PDF-1.7\nsecret attachment")
	h.client.downloadData = data
	h.stores["alpha"].Remember("received-id", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(uint64(len(data))), DirectPath: proto.String("/media")}})
	for _, body := range []any{map[string]any{"messageId": "received-id"}, map[string]any{"messageId": map[string]any{"id": "received-id", "url": "http://evil.test"}}} {
		w := h.post(t, "download-media", body)
		if w.Code != 200 {
			t.Fatalf("POST download: %d %s", w.Code, w.Body.String())
		}
		var result struct{ Base64, Mimetype string }
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		if result.Base64 != base64.StdEncoding.EncodeToString(data) || result.Mimetype != "application/pdf" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("wrong download: %s", w.Body.String())
		}
	}
	w := h.call(http.MethodGet, "/api/alpha/get-media-by-message/received-id", h.tokens["alpha"], "", nil)
	if w.Code != 200 {
		t.Fatalf("GET download: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		path, token string
		code        int
	}{
		{"/api/beta/get-media-by-message/received-id", h.tokens["beta"], 404},
		{"/api/alpha/get-media-by-message/received-id", h.tokens["beta"], 401},
		{"/api/alpha/get-media-by-message/received-id", "", 401},
		{"/api/alpha/get-media-by-message/unknown", h.tokens["alpha"], 404},
	} {
		w = h.call(http.MethodGet, tc.path, tc.token, "", nil)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if h.client.downloads != 3 {
		t.Fatal("unauthorized/missing media reached downloader")
	}
	for _, body := range []any{map[string]any{}, map[string]any{"messageId": map[string]any{"url": "http://127.0.0.1/secret", "mediaKey": "evil"}}} {
		if w = h.post(t, "download-media", body); w.Code != 400 {
			t.Fatalf("descriptor accepted: %d", w.Code)
		}
	}
	delete(h.stores, "alpha")
	if w = h.post(t, "download-media", map[string]any{"messageId": "received-id"}); w.Code != 404 {
		t.Fatalf("disconnected download: %d", w.Code)
	}
}

func TestDownloadFailuresAndActualSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   uint64
		err    error
		actual int
		status int
	}{
		{"network", 1, errors.New("network down"), 0, 502},
		{"timeout", 1, context.DeadlineExceeded, 0, 504},
		{"integrity", 1, whatsmeow.ErrInvalidMediaSHA256, 0, 502},
		{"declared too large", media.MaxBytes + 1, nil, 0, 413},
		{"hidden size via io.Copy", 1, nil, media.MaxBytes + 65, 413},
		{"decrypted too large", 1, nil, media.MaxBytes + 1, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMediaHarness(t)
			h.stores["alpha"].Remember("id", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{FileLength: proto.Uint64(tc.size), Mimetype: proto.String("image/png")}})
			h.client.downloadErr = tc.err
			if tc.actual != 0 {
				h.client.downloadData = make([]byte, tc.actual)
			}
			w := h.post(t, "download-media", map[string]any{"messageId": "id"})
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.size > media.MaxBytes && h.client.downloads != 0 {
				t.Fatal("oversized metadata reached downloader")
			}
		})
	}
}
