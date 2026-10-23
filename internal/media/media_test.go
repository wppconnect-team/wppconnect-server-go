package media

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestBase64BoundaryAndInvalidDataURLs(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, MaxBytes))
	decoded, _, err := Decode(encoded)
	if err != nil || len(decoded) != MaxBytes {
		t.Fatalf("exact file limit rejected: %d %v", len(decoded), err)
	}
	if _, _, err := Decode(encoded + "AAAA"); !errors.Is(err, ErrTooLarge) {
		t.Fatal("encoded limit not enforced")
	}
	for _, value := range []string{"", "###", "data:image/png,AAAA", "data:image/png;base64,", "data:image/png;base64"} {
		if _, _, err := Decode(value); err == nil {
			t.Fatalf("invalid input accepted: %q", value)
		}
	}
}

func TestDocumentAndAudioMIME(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     []byte
		declared string
		kind     Kind
		want     string
	}{
		{"csv", []byte("name,amount\nitem,12\n"), "text/csv", Document, "text/csv"},
		{"generic PDF", []byte("%PDF-1.7\ncontent"), "application/octet-stream", Document, "application/pdf"},
		{"text pretending image", []byte("plain text"), "image/png", Document, ""},
		{"PDF pretending image", []byte("%PDF-1.7\ncontent"), "image/png", Image, ""},
		{"WAV", append([]byte("RIFF"), append(make([]byte, 4), []byte("WAVEfmt ")...)...), "audio/wav", Audio, "audio/wav"},
		{"FLAC", []byte("fLaCcontent"), "audio/flac", Audio, "audio/flac"},
		{"Ogg without Opus", append([]byte("OggS\x00"), bytes.Repeat([]byte{0}, 40)...), "", Voice, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MIME(tc.data, tc.declared, tc.kind)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("invalid MIME accepted: %s", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("want %s, got %s: %v", tc.want, got, err)
			}
		})
	}
}
