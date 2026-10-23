package history

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type StoredMedia struct {
	Chat    string `json:"chatId"`
	ID      string `json:"id"`
	Key     string `json:"-"`
	MIME    string `json:"mimetype"`
	Size    int64  `json:"size"`
	Hash    string `json:"sha256"`
	Expires int64  `json:"expiresAt"`
	Ready   bool   `json:"ready"`
}

func (s *Store) initAttachments() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS history_media(chat TEXT NOT NULL,id TEXT NOT NULL,object TEXT NOT NULL UNIQUE,mime TEXT NOT NULL,size INTEGER NOT NULL,hash TEXT NOT NULL,expires INTEGER NOT NULL,ready INTEGER NOT NULL,PRIMARY KEY(chat,id));`)
	return err
}
func (s *Store) ReserveMedia(ctx context.Context, m StoredMedia) error {
	if !ValidChat(m.Chat) || !validID(m.ID) || m.Key == "" || m.Size < 1 || m.Size > 16<<20 || len(m.Hash) != 64 || m.Expires <= time.Now().UnixMilli() {
		return errors.New("invalid stored media")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count, total int64
	if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(size),0) FROM history_media").Scan(&count, &total); err != nil {
		return err
	}
	if count >= 1000 || total+m.Size > 512<<20 {
		return errors.New("stored media quota exceeded (1000 objects/512 MiB per session)")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO history_media(chat,id,object,mime,size,hash,expires,ready) VALUES(?,?,?,?,?,?,?,0)", m.Chat, m.ID, m.Key, m.MIME, m.Size, m.Hash, m.Expires)
	if err != nil {
		return errors.New("media is already stored or an upload is in progress")
	}
	return tx.Commit()
}
func (s *Store) FinishMedia(ctx context.Context, m StoredMedia) error {
	result, err := s.db.ExecContext(ctx, "UPDATE history_media SET ready=1,expires=? WHERE chat=? AND id=? AND object=?", m.Expires, m.Chat, m.ID, m.Key)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrNotFound
	}
	return err
}
func (s *Store) Media(ctx context.Context, chat, id string) (StoredMedia, error) {
	var m StoredMedia
	err := s.db.QueryRowContext(ctx, "SELECT chat,id,object,mime,size,hash,expires,ready FROM history_media WHERE chat=? AND id=? AND ready=1 AND expires>?", chat, id, time.Now().UnixMilli()).Scan(&m.Chat, &m.ID, &m.Key, &m.MIME, &m.Size, &m.Hash, &m.Expires, &m.Ready)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return m, err
}
func (s *Store) ExpiredMedia(ctx context.Context, now int64) ([]StoredMedia, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT chat,id,object,mime,size,hash,expires,ready FROM history_media WHERE expires<=? ORDER BY expires LIMIT 100", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoredMedia{}
	for rows.Next() {
		var m StoredMedia
		if err = rows.Scan(&m.Chat, &m.ID, &m.Key, &m.MIME, &m.Size, &m.Hash, &m.Expires, &m.Ready); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) ForgetMedia(ctx context.Context, chat, id, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM history_media WHERE chat=? AND id=? AND object=?", chat, id, key)
	return err
}
