package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"os"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
)

func advancedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../media/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAdvancedMediaHTTP(t *testing.T) {
	for _, tc := range []struct {
		route, file string
		kind        whatsmeow.MediaType
		animated    bool
	}{
		{"send-video", "video.mp4", whatsmeow.MediaVideo, false},
		{"send-video-base64", "video.mp4", whatsmeow.MediaVideo, false},
		{"send-sticker", "sticker.webp", whatsmeow.MediaImage, false},
		{"send-sticker", "animated.webp", whatsmeow.MediaImage, true},
		{"send-sticker-gif", "animated.webp", whatsmeow.MediaImage, true},
	} {
		t.Run(tc.route+tc.file, func(t *testing.T) {
			h := newMediaHarness(t)
			data := advancedFixture(t, tc.file)
			w := h.post(t, tc.route, map[string]any{"phone": []string{"123-456", "234-567"}, "isGroup": true, "base64": base64.StdEncoding.EncodeToString(data)})
			if w.Code != 201 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if h.client.uploads != 1 || h.client.uploadType != tc.kind || !bytes.Equal(data, h.client.uploaded) || len(h.client.sent) != 2 {
				t.Fatal("incorrect upload/send")
			}
			msg := h.client.sent[0]
			if strings.HasPrefix(tc.route, "send-video") {
				v := msg.GetVideoMessage()
				if v == nil || v.GetMimetype() != "video/mp4" || v.GetWidth() != 32 || v.GetSeconds() != 1 || v.GetFileLength() != uint64(len(data)) {
					t.Fatal("invalid video metadata")
				}
			} else {
				s := msg.GetStickerMessage()
				if s == nil || s.GetMimetype() != "image/webp" || s.GetWidth() != 512 || s.GetIsAnimated() != tc.animated || s.GetFileLength() != uint64(len(data)) {
					t.Fatal("invalid sticker metadata")
				}
			}
			if _, ok := h.stores["alpha"].Get("sent-1"); !ok {
				t.Fatal("missing sent descriptor")
			}
			h.client.downloadData = data
			for _, session := range []string{"alpha", "beta"} {
				got := h.call("GET", "/api/"+session+"/get-media-by-message/sent-1", h.tokens[session], "", nil)
				want := 200
				if session == "beta" {
					want = 404
				}
				if got.Code != want {
					t.Fatalf("%s download %d %s", session, got.Code, got.Body.String())
				}
			}
			if h.client.downloads != 1 {
				t.Fatal("cross-session download reached SDK")
			}
		})
	}
}

func TestAdvancedMediaMultipartAndDataURL(t *testing.T) {
	h := newMediaHarness(t)
	for _, tc := range []struct{ route, file, mime string }{{"send-video", "video-audio.mp4", "video/mp4"}, {"send-sticker-gif", "animated.webp", "image/webp"}} {
		data := advancedFixture(t, tc.file)
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if err := form.WriteField("phone", "123"); err != nil {
			t.Fatal(err)
		}
		file, err := form.CreateFormFile("file", tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		w := h.call("POST", "/api/alpha/"+tc.route, h.tokens["alpha"], form.FormDataContentType(), &body)
		if w.Code != 201 {
			t.Fatalf("multipart %d %s", w.Code, w.Body.String())
		}
		w = h.post(t, tc.route, map[string]any{"phone": "123", "path": "data:" + tc.mime + ";base64," + base64.StdEncoding.EncodeToString(data)})
		if w.Code != 201 {
			t.Fatalf("data URL %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAdvancedStructuredHTTP(t *testing.T) {
	for _, tc := range []struct {
		route, body, kind string
		status            int
	}{
		{"contact-vcard", `"contactsId":["5511999999999"],"name":"João"`, "vcard", 200},
		{"send-contact", `"contacts":[{"phone":"5511999999999","name":"Ana"},{"phone":"5521999999999","name":"Bia"}]`, "vcard", 201},
		{"send-location", `"lat":"-23.55","lng":-46.63,"title":"Office","address":"Main St"`, "location", 201},
		{"send-poll-message", `"name":"Lunch?","choices":["12:00","13:00"],"options":{"selectableCount":1}`, "poll_creation", 201},
	} {
		t.Run(tc.route, func(t *testing.T) {
			h := newMediaHarness(t)
			body := `{"phone":["123-456","234-567"],"isGroup":true,` + tc.body + `}`
			w := h.call("POST", "/api/alpha/"+tc.route, h.tokens["alpha"], "application/json", strings.NewReader(body))
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if h.client.uploads != 0 || len(h.client.sent) != 2 || h.client.to[0].String() != "123-456@g.us" {
				t.Fatal("unexpected SDK calls")
			}
			msg := h.client.sent[0]
			switch tc.route {
			case "contact-vcard":
				if msg.GetContactMessage().GetDisplayName() != "João" || !strings.Contains(msg.GetContactMessage().GetVcard(), "waid=5511999999999") {
					t.Fatal("contact")
				}
			case "send-contact":
				if len(msg.GetContactsArrayMessage().GetContacts()) != 2 {
					t.Fatal("multiple contacts")
				}
			case "send-location":
				if msg.GetLocationMessage().GetDegreesLatitude() != -23.55 || msg.GetLocationMessage().GetName() != "Office" {
					t.Fatal("location")
				}
			case "send-poll-message":
				if msg.GetPollCreationMessage().GetName() != "Lunch?" || msg.GetPollCreationMessage().GetSelectableOptionsCount() != 1 {
					t.Fatal("poll")
				}
				a, b := msg.GetMessageContextInfo().GetMessageSecret(), h.client.sent[1].GetMessageContextInfo().GetMessageSecret()
				if len(a) != 32 || bytes.Equal(a, b) {
					t.Fatal("poll secrets must differ per recipient")
				}
				if strings.Contains(w.Body.String(), base64.StdEncoding.EncodeToString(a)) {
					t.Fatal("secret exposed")
				}
			}
			if !strings.Contains(w.Body.String(), `"type":"`+tc.kind+`"`) {
				t.Fatal("result kind")
			}
		})
	}
}

func TestAdvancedRejectsBeforeSDK(t *testing.T) {
	h := newMediaHarness(t)
	static := base64.StdEncoding.EncodeToString(advancedFixture(t, "sticker.webp"))
	for _, tc := range []struct{ route, body string }{
		{"send-sticker-gif", `{"phone":"123","base64":"` + static + `"}`},
		{"send-sticker", `{"phone":"123","base64":"` + static + `","caption":"unsupported"}`},
		{"send-video", `{"phone":"123","base64":"` + static + `"}`},
		{"send-contact", `{"phone":"123","contactsId":["invalid"]}`},
		{"send-location", `{"phone":"123","lat":0}`},
		{"send-location", `{"phone":"123","lat":0,"lng":0,"live":true}`},
		{"send-location", `{"phone":"123","lat":0,"lng":0,"choices":["A","B"]}`},
		{"send-contact", `{"phone":"123","contactsId":["5511999999999"],"contacts":[]}`},
		{"send-location", `{"phone":"123","lat":0,"lng":0} {}`},
		{"send-poll-message", `{"phone":"123","name":"Q","choices":["A","B"],"options":{"selectableCount":3}}`},
		{"send-poll-message", `{"phone":"123","name":"Q","choices":["A","B"],"options":{"unknown":true}}`},
		{"send-poll-message", `{"phone":"123@lid","name":"Q","choices":["A","B"]}`},
		{"send-poll-message", `{"phone":"123","isLid":true,"name":"Q","choices":["A","B"]}`},
	} {
		w := h.call("POST", "/api/alpha/"+tc.route, h.tokens["alpha"], "application/json", strings.NewReader(tc.body))
		if w.Code != 400 {
			t.Fatalf("%s: %d %s", tc.route, w.Code, w.Body.String())
		}
	}
	if h.client.uploads != 0 || len(h.client.sent) != 0 {
		t.Fatal("invalid input reached SDK")
	}
}

func TestAdvancedAuthLimitsAndFailures(t *testing.T) {
	h := newMediaHarness(t)
	for _, route := range []string{"send-video", "send-video-base64", "send-sticker", "send-sticker-gif", "contact-vcard", "send-contact", "send-location", "send-poll-message"} {
		for _, token := range []string{"", h.tokens["beta"]} {
			if w := h.call("POST", "/api/alpha/"+route, token, "application/json", strings.NewReader(`{}`)); w.Code != 401 {
				t.Fatalf("%s auth: %d", route, w.Code)
			}
		}
	}
	if w := h.call("POST", "/api/alpha/send-location", h.tokens["alpha"], "application/json", strings.NewReader(`{"phone":"123","lat":0,"lng":0,"title":"`+strings.Repeat("x", 65<<10)+`"}`)); w.Code != 413 {
		t.Fatalf("body limit %d", w.Code)
	}
	for _, tc := range []struct {
		err  error
		want int
	}{{errors.New("private SDK details"), 502}, {context.DeadlineExceeded, 504}, {context.Canceled, 408}} {
		h.client.sendErr = tc.err
		w := h.post(t, "send-location", map[string]any{"phone": "123", "lat": 0, "lng": 0})
		if w.Code != tc.want || strings.Contains(w.Body.String(), "private SDK") {
			t.Fatalf("send failure %d %s", w.Code, w.Body.String())
		}
	}
	h = newMediaHarness(t)
	h.client.sendFailAt = 2
	w := h.post(t, "send-poll-message", map[string]any{"phone": []string{"123", "456", "789"}, "name": "Q", "choices": []string{"A", "B"}})
	var result struct {
		Completed []map[string]any `json:"completed"`
		Failed    string           `json:"failedRecipient"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 502 || len(result.Completed) != 1 || result.Failed != "456@s.whatsapp.net" || len(h.client.sent) != 2 {
		t.Fatalf("partial result: %d %s", w.Code, w.Body.String())
	}
}
