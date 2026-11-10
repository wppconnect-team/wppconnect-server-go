package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
)

var sendKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func idempotentSend(name string) bool {
	switch name {
	case "follow-newsletter-invite", "unfollow-newsletter-invite", "newsletter-reaction", "newsletter-mark-viewed", "newsletter-accept-tos", "newsletter-admin-invite":
		return true
	case "send-message", "send-reply", "send-image", "send-file", "send-file-base64", "send-voice", "send-voice-base64", "send-audio-base64", "send-video", "send-video-base64", "send-sticker", "send-sticker-gif", "send-contact", "contact-vcard", "send-location", "send-poll-message", "send-status-text", "send-status-media", "send-buttons", "send-list", "send-carousel", "send-newsletter-text", "send-reaction":
		return true
	}
	return false
}

// Results share a namespace only with operations authorized by the same Cloud scope.
// Existing message keys retain their original hash for compatibility across upgrades.
func resultRoute(operation string) string {
	switch operation {
	case "newsletter-write-result", "follow-newsletter-invite", "unfollow-newsletter-invite", "newsletter-reaction", "newsletter-mark-viewed":
		return "newsletter-write-result"
	case "newsletter-admin-result", "newsletter-admin-invite", "newsletter-accept-tos":
		return "newsletter-admin-result"
	default:
		return "send-result"
	}
}
func operationHash(operation, key string) string {
	if ns := resultRoute(operation); ns != "send-result" {
		return keyHash(ns + "\x00" + key)
	}
	return keyHash(key)
}
func keyHash(key string) string { sum := sha256.Sum256([]byte(key)); return hex.EncodeToString(sum[:]) }
func commandError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"status": "error", "code": code, "message": message})
}

// Buffer only the small response. Spool bounded request bodies to a private
// temporary file so hashing a media request does not duplicate its RAM usage.
type sendResponse struct {
	header   http.Header
	body     bytes.Buffer
	code     int
	overflow bool
}

func (w *sendResponse) Header() http.Header { return w.header }
func (w *sendResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *sendResponse) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	if w.body.Len()+len(p) > sendledger.MaxResponse {
		w.overflow = true
		return 0, errors.New("send response too large")
	}
	return w.body.Write(p)
}

func (s *Server) idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if len(r.Header.Values("Idempotency-Key")) == 0 || r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if r.Method != http.MethodPost || !idempotentSend(name) {
			commandError(w, 400, "IDEMPOTENCY_UNSUPPORTED", "Idempotency-Key is only supported for documented send operations")
			return
		}
		if len(r.Header.Values("Idempotency-Key")) != 1 || !sendKeyPattern.MatchString(key) {
			commandError(w, 400, "INVALID_IDEMPOTENCY_KEY", "Use 1..128 ASCII letters, digits, dot, underscore, colon or hyphen")
			return
		}
		if s.sends == nil {
			commandError(w, 503, "SEND_LEDGER_UNAVAILABLE", "durable send ledger unavailable")
			return
		}
		select {
		case s.sendSlots <- struct{}{}:
			defer func() { <-s.sendSlots }()
		default:
			commandError(w, 429, "SEND_CAPACITY", "too many idempotent sends in progress")
			return
		}
		spool, err := os.CreateTemp("", "wpp-send-*")
		if err != nil {
			commandError(w, 503, "SEND_STORAGE_UNAVAILABLE", "request storage unavailable")
			return
		}
		defer func() { spool.Close(); os.Remove(spool.Name()) }()
		digest := sha256.New()
		// No authentication material or request body is stored in the ledger.
		io.WriteString(digest, name+"\x00"+r.URL.RawQuery+"\x00"+r.Header.Get("Content-Type")+"\x00")
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, err = io.Copy(io.MultiWriter(spool, digest), http.MaxBytesReader(w, r.Body, media.MaxBodyBytes))
		_ = controller.SetReadDeadline(time.Time{})
		r.Body.Close()
		if err != nil {
			mediaError(w, err, "could not read complete request body")
			return
		}
		if _, err = spool.Seek(0, io.SeekStart); err != nil {
			commandError(w, 503, "SEND_STORAGE_UNAVAILABLE", "request storage unavailable")
			return
		}
		r.Body = io.NopCloser(spool)
		session := cleanSession(chi.URLParam(r, "session"))
		hash := operationHash(name, key)
		record, fresh, err := s.sends.Claim(r.Context(), session, hash, hex.EncodeToString(digest.Sum(nil)))
		if err != nil {
			if errors.Is(err, sendledger.ErrConflict) {
				commandError(w, 409, "IDEMPOTENCY_CONFLICT", err.Error())
			} else {
				commandError(w, 503, "SEND_LEDGER_UNAVAILABLE", "cannot reserve send; no protocol operation started")
			}
			return
		}
		if !fresh {
			if record.State == "pending" {
				commandError(w, 409, "SEND_PENDING_OR_UNKNOWN", "send is in progress or its outcome is unknown; it will not be repeated automatically")
				return
			}
			w.Header().Set("Idempotency-Replayed", "true")
			w.Header().Set("Content-Type", record.ContentType)
			w.WriteHeader(record.Code)
			w.Write(record.Body)
			return
		}
		captured := &sendResponse{header: make(http.Header)}
		next.ServeHTTP(captured, r)
		if captured.code == 0 {
			captured.code = 200
		}
		if captured.overflow {
			commandError(w, 503, "SEND_RESULT_UNKNOWN", "send result exceeded storage capacity; do not resend with a new key")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = s.sends.Finish(ctx, session, hash, record.Owner, captured.code, captured.header.Get("Content-Type"), captured.body.Bytes())
		if err != nil {
			commandError(w, 503, "SEND_RESULT_UNKNOWN", "send result could not be persisted; do not resend with a new key")
			return
		}
		for k, values := range captured.header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(captured.code)
		w.Write(captured.body.Bytes())
	})
}

func (s *Server) sendResult(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) != 1 || !sendKeyPattern.MatchString(key) {
		commandError(w, 400, "INVALID_IDEMPOTENCY_KEY", "a valid Idempotency-Key header is required")
		return
	}
	if s.sends == nil {
		commandError(w, 503, "SEND_LEDGER_UNAVAILABLE", "durable send ledger unavailable")
		return
	}
	record, err := s.sends.Get(r.Context(), cleanSession(chi.URLParam(r, "session")), operationHash(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], key))
	if errors.Is(err, sendledger.ErrMissing) {
		commandError(w, 404, "SEND_RESULT_NOT_FOUND", "send result not found in this session")
		return
	}
	if err != nil {
		commandError(w, 503, "SEND_LEDGER_UNAVAILABLE", "send result unavailable")
		return
	}
	state := record.State
	if state == "pending" {
		state = "pending_or_unknown"
	}
	var response any
	if len(record.Body) > 0 {
		if json.Unmarshal(record.Body, &response) != nil {
			response = string(record.Body)
		}
	}
	writeJSON(w, 200, map[string]any{"status": "success", "state": state, "httpStatus": record.Code, "response": response, "createdAt": record.Created})
}
