package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	wm "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/wppconnect-team/wppconnect-server-go/internal/directory"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"strings"
	"time"
)

type carouselButton struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type carouselCard struct {
	Body    string           `json:"body"`
	Image   string           `json:"imageBase64"`
	Buttons []carouselButton `json:"buttons"`
}
type carouselRequest struct {
	Phone   string         `json:"phone"`
	IsGroup bool           `json:"isGroup"`
	Body    string         `json:"body"`
	Footer  string         `json:"footer"`
	Cards   []carouselCard `json:"cards"`
}
type carouselImage struct {
	data []byte
	mime string
}

// Validate all cards before uploading any media. Only the uploader supplies media keys/URLs.
func prepareCarousel(q carouselRequest) (*waE2E.Message, []carouselImage, error) {
	if len(q.Cards) < 2 || len(q.Cards) > 10 {
		return nil, nil, errors.New("carousel requires 2..10 cards")
	}
	params := wm.BusinessCarouselMessageParams{Body: q.Body, Footer: q.Footer}
	images := make([]carouselImage, 0, len(q.Cards))
	for _, c := range q.Cards {
		data, declared, err := media.Decode(c.Image)
		if err != nil || len(data) > 1<<20 {
			return nil, nil, errors.New("each card requires a base64 JPEG or PNG up to 1 MiB")
		}
		mt, err := media.MIME(data, declared, media.Image)
		if err != nil || (mt != "image/jpeg" && mt != "image/png") {
			return nil, nil, errors.New("card media must be a JPEG or PNG")
		}
		conf, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 4096 || conf.Height > 4096 {
			return nil, nil, errors.New("invalid card image or dimensions over 4096")
		}
		if len(c.Buttons) < 1 || len(c.Buttons) > 3 {
			return nil, nil, errors.New("each card needs 1..3 reply buttons")
		}
		card := wm.BusinessCarouselCard{Body: c.Body, Image: &waE2E.ImageMessage{}}
		for _, b := range c.Buttons {
			if strings.TrimSpace(b.ID) == "" || len(b.ID) > 256 || strings.TrimSpace(b.Label) == "" || len([]rune(b.Label)) > 80 {
				return nil, nil, errors.New("invalid reply ID or label")
			}
			raw, _ := json.Marshal(map[string]string{"display_text": b.Label, "id": b.ID})
			card.Buttons = append(card.Buttons, wm.BusinessNativeFlowButton{Name: "quick_reply", ParamsJSON: string(raw)})
		}
		images = append(images, carouselImage{data, mt})
		params.Cards = append(params.Cards, card)
	}
	msg, err := wm.BuildBusinessCarouselMessage(params)
	return msg, images, err
}
func (s *Server) sendCarousel(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.ExperimentalInteractives {
		writeJSON(w, 501, map[string]any{"status": "not_supported", "capability": "carousel_delivery", "message": "image carousels require ENABLE_EXPERIMENTAL_INTERACTIVES"})
		return
	}
	client, _, ok := s.mediaConnection(w, r)
	if !ok || !s.acquireMedia(w, r) {
		return
	}
	defer func() { <-s.mediaSlots }()
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		mediaError(w, errors.New("expected application/json"), "")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	var q carouselRequest
	if err = decoder.Decode(&q); err == nil {
		err = requireEOF(decoder)
	}
	if err != nil {
		mediaError(w, err, "")
		return
	}
	to, err := directory.Parse(q.Phone, q.IsGroup, false)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	msg, images, err := prepareCarousel(q)
	if err != nil {
		mediaError(w, err, "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	cards := msg.GetInteractiveMessage().GetCarouselMessage().GetCards()
	for i, img := range images {
		u, err := client.Upload(ctx, img.data, media.UploadType(media.Image))
		if err != nil {
			writeJSON(w, 502, map[string]any{"status": "error", "message": "card upload failed; no carousel send attempted"})
			return
		}
		m := media.Message(media.Image, u, img.mime, "", "")
		cards[i].Header.Media = &waE2E.InteractiveMessage_Header_ImageMessage{ImageMessage: m.ImageMessage}
	}
	sent, err := client.SendMessage(ctx, to, msg)
	if err != nil {
		sendError(w, err, nil, to.String())
		return
	}
	s.observeSent(cleanSession(chi.URLParam(r, "session")), to, sent.ID, sent.Timestamp, msg)
	writeJSON(w, 201, map[string]any{"status": "success", "response": []map[string]any{{"id": sent.ID, "to": to.String(), "type": "carousel"}}, "experimental": true})
}
