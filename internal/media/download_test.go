package media

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"golang.org/x/crypto/hkdf"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

// Use the real SDK decryption path against an in-memory HTTP transport.
func TestDownloadWithRealSDK(t *testing.T) {
	for _, kind := range []Kind{Document, Video, Sticker} {
		name := map[Kind]string{Document: "document", Video: "video", Sticker: "sticker"}[kind]
		t.Run(name, func(t *testing.T) { testDownloadWithRealSDK(t, kind) })
	}
}

func testDownloadWithRealSDK(t *testing.T, kind Kind) {
	plain := []byte("verified document content")
	key := bytes.Repeat([]byte{1}, 32)
	keys := make([]byte, 112)
	if _, err := io.ReadFull(hkdf.New(sha256.New, key, nil, []byte(UploadType(kind))), keys); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(keys[16:48])
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(bytes.Clone(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, keys[:16]).CryptBlocks(encrypted, padded)
	mac := hmac.New(sha256.New, keys[48:80])
	mac.Write(keys[:16])
	mac.Write(encrypted)
	encrypted = append(encrypted, mac.Sum(nil)[:10]...)
	encHash, plainHash := sha256.Sum256(encrypted), sha256.Sum256(plain)
	for _, tc := range []struct {
		name    string
		corrupt bool
		status  int
	}{{"valid", false, 200}, {"corrupt", true, 200}, {"not found", false, 404}} {
		t.Run(tc.name, func(t *testing.T) {
			cli := whatsmeow.NewClient(&store.Device{}, nil)
			payload := bytes.Clone(encrypted)
			if tc.corrupt {
				payload[0] ^= 1
			}
			cli.SetMediaHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Request: r}, nil
			})})
			a, ok := attachment(Message(kind, whatsmeow.UploadResponse{
				URL: "https://mmg.whatsapp.net/fixture", MediaKey: key, FileEncSHA256: encHash[:], FileSHA256: plainHash[:], FileLength: uint64(len(plain)),
			}, "application/octet-stream", "fixture", ""))
			if !ok {
				t.Fatal("descriptor rejected")
			}
			got, err := Download(context.Background(), cli, a)
			if tc.corrupt || tc.status != 200 {
				if err == nil || got != nil {
					t.Fatal("failed integrity/network response returned content")
				}
			} else if err != nil || !bytes.Equal(got, plain) {
				t.Fatalf("download: %q, %v", got, err)
			}
		})
	}
}

func TestDownloadFileLimitAllWritePaths(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "limited")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	bounded := &limitedFile{File: f, max: 4}
	if _, err := bounded.WriteAt([]byte("12345"), 0); !errors.Is(err, ErrTooLarge) {
		t.Fatal("WriteAt bypassed limit")
	}
	if err := bounded.Truncate(5); !errors.Is(err, ErrTooLarge) {
		t.Fatal("Truncate bypassed limit")
	}
	if _, err := io.Copy(bounded, struct{ io.Reader }{bytes.NewReader([]byte("12345"))}); !errors.Is(err, ErrTooLarge) {
		t.Fatal("ReaderFrom bypassed limit")
	}
	info, _ := f.Stat()
	if info.Size() > 4 {
		t.Fatal("file grew beyond bound")
	}
}
