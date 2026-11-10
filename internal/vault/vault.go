// Package vault stores explicitly archived media in private S3-compatible storage.
package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
)

type Objects interface {
	Put(context.Context, string, []byte, string) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
type Vault struct {
	Objects   Objects
	Retention time.Duration
}
type S3 struct {
	Client *minio.Client
	Bucket string
}

func NewS3(endpoint, access, secret, bucket, region string, secure bool) (*S3, error) {
	if endpoint == "" || access == "" || secret == "" || bucket == "" {
		return nil, errors.New("S3 endpoint, credentials and bucket are required")
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: secure, Region: region})
	if err != nil {
		return nil, errors.New("invalid S3 configuration")
	}
	return &S3{client, bucket}, nil
}
func (s *S3) Put(ctx context.Context, key string, data []byte, mime string) error {
	_, err := s.Client.PutObject(ctx, s.Bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: mime, DisableMultipart: true, SendContentMd5: true})
	return err
}
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	object, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, (16<<20)+1))
	if err == nil && len(data) > 16<<20 {
		err = errors.New("stored object exceeds media limit")
	}
	return data, err
}
func (s *S3) Delete(ctx context.Context, key string) error {
	return s.Client.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{})
}

func objectKey(session, chat, id string) string {
	a := sha256.Sum256([]byte(session))
	b := sha256.Sum256([]byte(chat + "\x00" + id))
	return "wppconnect/" + hex.EncodeToString(a[:]) + "/" + hex.EncodeToString(b[:])
}
func (v *Vault) Archive(ctx context.Context, store *history.Store, session, chat, id, mime string, data []byte) (history.StoredMedia, error) {
	if v.Objects == nil || v.Retention <= 0 || v.Retention > 30*24*time.Hour {
		return history.StoredMedia{}, errors.New("invalid media vault configuration")
	}
	now := time.Now()
	hash := sha256.Sum256(data)
	m := history.StoredMedia{Chat: chat, ID: id, Key: objectKey(session, chat, id) + "/" + uuid.NewString(), MIME: mime, Size: int64(len(data)), Hash: hex.EncodeToString(hash[:]), Expires: now.Add(v.Retention).UnixMilli()}
	reservation := m
	reservation.Expires = now.Add(time.Hour).UnixMilli()
	if err := store.ReserveMedia(ctx, reservation); err != nil {
		prior, lookupErr := store.ActiveMediaReservation(ctx, chat, id)
		if lookupErr != nil || !owns(session, prior) || prior.MIME != mime || prior.Size != m.Size || prior.Hash != m.Hash {
			return history.StoredMedia{}, err
		}
		saved, getErr := v.Objects.Get(ctx, prior.Key)
		if getErr != nil || !bytes.Equal(saved, data) {
			return history.StoredMedia{}, errors.New("existing media upload could not be verified")
		}
		if prior.Ready {
			return store.ConfirmMedia(ctx, prior, prior.Expires)
		}
		// A pending reservation expires one hour after the original attempt.
		// Recover its original retention window, even after a process restart.
		expires := time.UnixMilli(prior.Expires).Add(v.Retention - time.Hour).UnixMilli()
		return store.ConfirmMedia(ctx, prior, expires)
	}
	if err := v.Objects.Put(ctx, m.Key, data, mime); err != nil {
		// Keep the durable reservation: a failed/ambiguous upload is cleaned up
		// by retention, even if the object server accepted it before timing out.
		return m, errors.New("object upload unconfirmed; repeat the archive to verify the reserved object")
	}
	confirmed, err := store.ConfirmMedia(ctx, m, m.Expires)
	if err != nil {
		return m, errors.New("object uploaded; metadata acknowledgement failed")
	}
	return confirmed, nil
}
func (v *Vault) Download(ctx context.Context, store *history.Store, session, chat, id string) (history.StoredMedia, []byte, error) {
	m, err := store.Media(ctx, chat, id)
	if err != nil {
		return m, nil, err
	}
	if !owns(session, m) {
		return m, nil, errors.New("stored object ownership mismatch")
	}
	data, err := v.Objects.Get(ctx, m.Key)
	if err != nil {
		return m, nil, errors.New("stored object download failed")
	}
	hash := sha256.Sum256(data)
	if int64(len(data)) != m.Size || hex.EncodeToString(hash[:]) != m.Hash {
		return m, nil, errors.New("stored object failed integrity verification")
	}
	return m, data, nil
}
func (v *Vault) Delete(ctx context.Context, store *history.Store, session string, m history.StoredMedia) error {
	if !owns(session, m) {
		return errors.New("stored object ownership mismatch")
	}
	if err := v.Objects.Delete(ctx, m.Key); err != nil {
		return errors.New("stored object deletion failed")
	}
	return store.ForgetMedia(ctx, m.Chat, m.ID, m.Key)
}
func owns(session string, m history.StoredMedia) bool {
	prefix := objectKey(session, m.Chat, m.ID) + "/"
	if !strings.HasPrefix(m.Key, prefix) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(m.Key, prefix))
	return err == nil
}
func (v *Vault) Cleanup(ctx context.Context, store *history.Store, session string) error {
	list, err := store.ExpiredMedia(ctx, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	for _, m := range list {
		if err = v.Delete(ctx, store, session, m); err != nil {
			return err
		}
	}
	return nil
}
