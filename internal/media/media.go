// Package media handles bounded attachments and the whatsmeow media contract.
package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

const MaxBytes = 16 << 20
const MaxBodyBytes = (MaxBytes+2)/3*4 + 64<<10

var ErrTooLarge = errors.New("media exceeds the 16 MiB limit")

// Client is the protocol boundary used by media handlers and their tests.
type Client interface {
	Upload(context.Context, []byte, whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	SendMessage(context.Context, types.JID, *waE2E.Message, ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	DownloadToFile(context.Context, whatsmeow.DownloadableMessage, whatsmeow.File) error
}

type Kind int

const (
	Image Kind = iota
	Document
	Audio
	Voice
	Video
	Sticker
)

// Decode accepts raw base64 and data URLs, without allocating oversized output.
func Decode(value string) ([]byte, string, error) {
	declared := ""
	if strings.HasPrefix(value, "data:") {
		header, payload, ok := strings.Cut(value[5:], ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, "", errors.New("expected a base64 data URL")
		}
		declared = strings.TrimSuffix(header, ";base64")
		value = payload
	}
	if len(value) > (MaxBytes+2)/3*4 {
		return nil, "", ErrTooLarge
	}
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("invalid or empty base64")
	}
	if len(data) > MaxBytes {
		return nil, "", ErrTooLarge
	}
	return data, declared, nil
}

// MIME uses content signatures for images/audio and validates declarations.
func MIME(data []byte, declared string, kind Kind) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty media")
	}
	detected, _, _ := mime.ParseMediaType(http.DetectContentType(data))
	if bytes.HasPrefix(data, []byte("fLaC")) {
		detected = "audio/flac"
	}
	detected = canonicalMIME(detected)
	if declared != "" {
		value, _, err := mime.ParseMediaType(declared)
		if err != nil {
			return "", errors.New("invalid mimetype")
		}
		value = canonicalMIME(value)
		compatibleDocument := kind == Document && ((detected == "text/plain" && (strings.HasPrefix(value, "text/") || value == "application/json" || value == "application/xml")) ||
			(detected == "application/zip" && (strings.HasPrefix(value, "application/vnd.openxmlformats-officedocument.") || strings.HasPrefix(value, "application/vnd.oasis.opendocument.") || value == "application/epub+zip")))
		if value != detected && detected != "application/octet-stream" && value != "application/octet-stream" && !compatibleDocument {
			return "", errors.New("mimetype does not match media content")
		}
		if kind == Document && value != "application/octet-stream" {
			detected = value
		}
	}
	switch kind {
	case Video:
		if detected != "video/mp4" {
			return "", errors.New("video requires MP4; no transcoding is performed")
		}
	case Sticker:
		if detected != "image/webp" {
			return "", errors.New("stickers require prepared WebP; no transcoding is performed")
		}
	case Image:
		switch detected {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return "", errors.New("expected JPEG, PNG, GIF or WebP image")
		}
	case Audio, Voice:
		if !strings.HasPrefix(detected, "audio/") {
			return "", errors.New("expected supported audio content")
		}
		if kind == Voice {
			// WhatsApp voice messages require Opus in an Ogg container.
			if detected != "audio/ogg" || !bytes.Contains(data[:min(len(data), 128)], []byte("OpusHead")) {
				return "", errors.New("voice messages require Ogg/Opus; no transcoding is performed")
			}
			detected = "audio/ogg; codecs=opus"
		}
	}
	return detected, nil
}

func canonicalMIME(value string) string {
	value = strings.ToLower(value)
	switch value {
	case "application/ogg":
		return "audio/ogg"
	case "audio/wave", "audio/x-wav":
		return "audio/wav"
	case "image/jpg":
		return "image/jpeg"
	default:
		return value
	}
}

func Filename(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(value, "/")
	value = parts[len(parts)-1]
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if value == "" || value == "." || value == ".." {
		return "file"
	}
	runes := []rune(value)
	return string(runes[:min(len(runes), 255)])
}

func UploadType(kind Kind) whatsmeow.MediaType {
	switch kind {
	case Image, Sticker:
		return whatsmeow.MediaImage
	case Video:
		return whatsmeow.MediaVideo
	case Audio, Voice:
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

func Message(kind Kind, u whatsmeow.UploadResponse, mimetype, filename, caption string) *waE2E.Message {
	switch kind {
	case Video:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			URL: &u.URL, DirectPath: &u.DirectPath, MediaKey: u.MediaKey,
			FileEncSHA256: u.FileEncSHA256, FileSHA256: u.FileSHA256, FileLength: &u.FileLength,
			Mimetype: &mimetype, Caption: &caption,
		}}
	case Sticker:
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			URL: &u.URL, DirectPath: &u.DirectPath, MediaKey: u.MediaKey,
			FileEncSHA256: u.FileEncSHA256, FileSHA256: u.FileSHA256, FileLength: &u.FileLength,
			Mimetype: &mimetype,
		}}
	case Image:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			URL: &u.URL, DirectPath: &u.DirectPath, MediaKey: u.MediaKey,
			FileEncSHA256: u.FileEncSHA256, FileSHA256: u.FileSHA256, FileLength: &u.FileLength,
			Mimetype: &mimetype, Caption: &caption,
		}}
	case Audio, Voice:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			URL: &u.URL, DirectPath: &u.DirectPath, MediaKey: u.MediaKey,
			FileEncSHA256: u.FileEncSHA256, FileSHA256: u.FileSHA256, FileLength: &u.FileLength,
			Mimetype: &mimetype, PTT: proto.Bool(kind == Voice),
		}}
	default:
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			URL: &u.URL, DirectPath: &u.DirectPath, MediaKey: u.MediaKey,
			FileEncSHA256: u.FileEncSHA256, FileSHA256: u.FileSHA256, FileLength: &u.FileLength,
			Mimetype: &mimetype, FileName: &filename, Caption: &caption,
		}}
	}
}

// Download bounds disk writes, including when metadata understates file size.
// DownloadToFile also verifies the encrypted file's MAC and SHA256 in whatsmeow.
func Download(ctx context.Context, client Client, a Attachment) ([]byte, error) {
	if a.Length > MaxBytes {
		return nil, ErrTooLarge
	}
	f, err := os.CreateTemp("", "wppgo-media-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	// Encryption adds one padding block and a 10-byte MAC.
	bounded := &limitedFile{File: f, max: MaxBytes + 64}
	if err := client.DownloadToFile(ctx, a.Message, bounded); err != nil {
		return nil, fmt.Errorf("download media: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxBytes {
		return nil, ErrTooLarge
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, MaxBytes+1))
}

type limitedFile struct {
	*os.File
	max int64
}

// os.File's promoted ReaderFrom would bypass our Write limit in io.Copy.
func (f *limitedFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{f}, r)
}

func (f *limitedFile) Write(p []byte) (int, error) {
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if pos < 0 || pos > f.max || int64(len(p)) > f.max-pos {
		return 0, ErrTooLarge
	}
	return f.File.Write(p)
}

func (f *limitedFile) WriteAt(p []byte, pos int64) (int, error) {
	if pos < 0 || pos > f.max || int64(len(p)) > f.max-pos {
		return 0, ErrTooLarge
	}
	return f.File.WriteAt(p, pos)
}

func (f *limitedFile) Truncate(size int64) error {
	if size < 0 || size > f.max {
		return ErrTooLarge
	}
	return f.File.Truncate(size)
}
