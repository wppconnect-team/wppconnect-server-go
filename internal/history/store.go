// Package history persists bounded, session-owned message history and labels.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("message not found in this session")
var ErrTooLarge = errors.New("message exceeds history storage limits")

type Options struct {
	Retention   time.Duration
	MaxMessages int
	MaxBytes    int64
}

func DefaultOptions() Options { return Options{30 * 24 * time.Hour, 100000, 256 << 20} }

type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	options Options
	now     func() time.Time
}
type Record struct {
	Sequence  int64          `json:"sequence"`
	ID        string         `json:"id"`
	Chat      string         `json:"chatId"`
	Sender    string         `json:"author"`
	FromMe    bool           `json:"fromMe"`
	Timestamp int64          `json:"t"`
	Read      bool           `json:"read"`
	Deleted   bool           `json:"deleted"`
	Edited    bool           `json:"edited"`
	Content   map[string]any `json:"content"`
	Message   *waE2E.Message `json:"-"`
}
type Query struct {
	Chat   string
	Before int64
	Limit  int
	Unread bool
}
type Page struct {
	Messages   []Record `json:"messages"`
	NextCursor int64    `json:"nextCursor,omitempty"`
}

func Open(path string, options Options) (*Store, error) {
	if options.Retention <= 0 || options.Retention > 365*24*time.Hour || options.MaxMessages < 1 || options.MaxMessages > 1000000 || options.MaxBytes < 1<<20 || options.MaxBytes > 10<<30 {
		return nil, errors.New("invalid history limits")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("history database cannot be a symbolic link")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS history_messages(seq INTEGER PRIMARY KEY AUTOINCREMENT,chat TEXT NOT NULL,id TEXT NOT NULL,sender TEXT NOT NULL,from_me INTEGER NOT NULL,t INTEGER NOT NULL,read INTEGER NOT NULL,deleted INTEGER NOT NULL DEFAULT 0,edited INTEGER NOT NULL DEFAULT 0,content BLOB NOT NULL,protocol BLOB NOT NULL,bytes INTEGER NOT NULL,UNIQUE(chat,id));
CREATE INDEX IF NOT EXISTS history_chat_order ON history_messages(chat,seq DESC);
CREATE INDEX IF NOT EXISTS history_age ON history_messages(t);
CREATE TABLE IF NOT EXISTS history_labels(id TEXT PRIMARY KEY,name TEXT NOT NULL,color INTEGER NOT NULL,deleted INTEGER NOT NULL,updated INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS history_label_links(label TEXT NOT NULL,chat TEXT NOT NULL,message TEXT NOT NULL DEFAULT '',active INTEGER NOT NULL,updated INTEGER NOT NULL,PRIMARY KEY(label,chat,message));
CREATE TABLE IF NOT EXISTS history_votes(chat TEXT NOT NULL,poll TEXT NOT NULL,voter TEXT NOT NULL,updated INTEGER NOT NULL,choices BLOB NOT NULL,PRIMARY KEY(chat,poll,voter));
CREATE TABLE IF NOT EXISTS history_receipts(chat TEXT NOT NULL,id TEXT NOT NULL,sender TEXT NOT NULL,kind TEXT NOT NULL,t INTEGER NOT NULL,PRIMARY KEY(chat,id,sender,kind));
CREATE TABLE IF NOT EXISTS history_stats(id INTEGER PRIMARY KEY CHECK(id=1),n INTEGER NOT NULL,b INTEGER NOT NULL);
INSERT OR IGNORE INTO history_stats SELECT 1,count(*),coalesce(sum(bytes),0) FROM history_messages;
CREATE TRIGGER IF NOT EXISTS history_insert AFTER INSERT ON history_messages BEGIN UPDATE history_stats SET n=n+1,b=b+new.bytes WHERE id=1; END;
CREATE TRIGGER IF NOT EXISTS history_delete AFTER DELETE ON history_messages BEGIN UPDATE history_stats SET n=n-1,b=b-old.bytes WHERE id=1; END;
CREATE TRIGGER IF NOT EXISTS history_update AFTER UPDATE OF bytes ON history_messages BEGIN UPDATE history_stats SET b=b-old.bytes+new.bytes WHERE id=1; END;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, options: options, now: time.Now}
	if err = s.initAttachments(); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.Prune(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func validID(v string) bool { return v != "" && len(v) <= 256 && !strings.ContainsAny(v, "\x00\r\n") }
func ValidChat(v string) bool {
	j, e := types.ParseJID(v)
	return e == nil && j.User != "" && len(j.User) <= 128 && (j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer || j.Server == types.GroupServer || j.Server == types.NewsletterServer || j == types.StatusBroadcastJID)
}

func (s *Store) Put(ctx context.Context, info types.MessageInfo, msg *waE2E.Message) error {
	if s == nil || msg == nil {
		return nil
	}
	if !validID(info.ID) || !ValidChat(info.Chat.String()) {
		return errors.New("invalid message identity")
	}
	msg = media.Unwrap(msg)
	if msg == nil {
		return nil
	}
	if proto.Size(msg) > 256<<10 {
		return ErrTooLarge
	}
	raw, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	content := media.Event(&events.Message{Info: info, Message: msg})
	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}
	if len(encoded) > 256<<10 {
		return ErrTooLarge
	}
	t := info.Timestamp.Unix()
	now := s.now()
	if info.Timestamp.IsZero() || t > now.Add(24*time.Hour).Unix() {
		t = now.Unix()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO history_messages(chat,id,sender,from_me,t,read,content,protocol,bytes) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(chat,id) DO NOTHING`, info.Chat.ToNonAD().String(), info.ID, info.Sender.ToNonAD().String(), info.IsFromMe, t, info.IsFromMe, encoded, raw, len(raw)+len(encoded))
	if err != nil {
		return err
	}
	if err = s.prune(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func scan(row interface{ Scan(...any) error }) (Record, error) {
	var r Record
	var content, raw []byte
	err := row.Scan(&r.Sequence, &r.Chat, &r.ID, &r.Sender, &r.FromMe, &r.Timestamp, &r.Read, &r.Deleted, &r.Edited, &content, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(content, &r.Content); err != nil {
		return r, err
	}
	r.Message = &waE2E.Message{}
	if len(raw) > 0 {
		err = proto.Unmarshal(raw, r.Message)
	}
	return r, err
}

const columns = "seq,chat,id,sender,from_me,t,read,deleted,edited,content,protocol"

func (s *Store) Get(ctx context.Context, chat, id string) (Record, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM history_messages WHERE chat=? AND id=? AND t>=?", chat, id, s.now().Add(-s.options.Retention).Unix()))
}
func (s *Store) Find(ctx context.Context, id string) (Record, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM history_messages WHERE id=? AND t>=? LIMIT 2", id, s.now().Add(-s.options.Retention).Unix())
	if err != nil {
		return Record{}, err
	}
	defer rows.Close()
	var found Record
	count := 0
	for rows.Next() {
		found, err = scan(rows)
		if err != nil {
			return found, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return found, err
	}
	if count == 0 {
		return found, ErrNotFound
	}
	if count > 1 {
		return Record{}, errors.New("ambiguous message ID; supply chatId")
	}
	return found, nil
}
func (s *Store) List(ctx context.Context, q Query) (Page, error) {
	out := Page{Messages: []Record{}}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 || q.Before < 0 {
		return out, errors.New("limit must be 1..200 and cursor nonnegative")
	}
	where, args := "t>=?", []any{s.now().Add(-s.options.Retention).Unix()}
	if q.Chat != "" {
		if !ValidChat(q.Chat) {
			return out, errors.New("invalid chatId")
		}
		where += " AND chat=?"
		args = append(args, q.Chat)
	}
	if q.Before > 0 {
		where += " AND seq<?"
		args = append(args, q.Before)
	}
	if q.Unread {
		where += " AND read=0 AND from_me=0"
	}
	args = append(args, q.Limit+1)
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM history_messages WHERE "+where+" ORDER BY seq DESC LIMIT ?", args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	responseBytes := 0
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return out, err
		}
		encoded, _ := json.Marshal(r)
		if len(out.Messages) > 0 && responseBytes+len(encoded) > 4<<20 {
			out.NextCursor = out.Messages[len(out.Messages)-1].Sequence
			break
		}
		responseBytes += len(encoded)
		out.Messages = append(out.Messages, r)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Messages) > q.Limit {
		out.Messages = out.Messages[:q.Limit]
		out.NextCursor = out.Messages[q.Limit-1].Sequence
	}
	return out, nil
}
func (s *Store) Prune(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.prune(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) prune(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM history_messages WHERE t<?", s.now().Add(-s.options.Retention).Unix())
	if err != nil {
		return err
	}
	var count, size int64
	if err = tx.QueryRowContext(ctx, "SELECT n,b FROM history_stats WHERE id=1").Scan(&count, &size); err != nil {
		return err
	}
	if count > int64(s.options.MaxMessages) || size > s.options.MaxBytes {
		keep := max(1, s.options.MaxMessages*95/100)
		budget := s.options.MaxBytes * 95 / 100
		_, err = tx.ExecContext(ctx, `DELETE FROM history_messages WHERE seq IN (SELECT seq FROM (SELECT seq,row_number() OVER(ORDER BY seq DESC) n,sum(bytes) OVER(ORDER BY seq DESC) total FROM history_messages) WHERE n>? OR total>?)`, keep, budget)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM history_votes WHERE NOT EXISTS(SELECT 1 FROM history_messages m WHERE m.chat=history_votes.chat AND m.id=history_votes.poll); DELETE FROM history_receipts WHERE NOT EXISTS(SELECT 1 FROM history_messages m WHERE m.chat=history_receipts.chat AND m.id=history_receipts.id); DELETE FROM history_label_links WHERE message<>'' AND NOT EXISTS(SELECT 1 FROM history_messages m WHERE m.chat=history_label_links.chat AND m.id=history_label_links.message);`)
	return err
}
func (r Record) Info() types.MessageInfo {
	chat, _ := types.ParseJID(r.Chat)
	sender, _ := types.ParseJID(r.Sender)
	return types.MessageInfo{ID: r.ID, Timestamp: time.Unix(r.Timestamp, 0), MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: r.FromMe, IsGroup: chat.Server == types.GroupServer}}
}
func (s *Store) MarkRead(ctx context.Context, chat string, ids []string) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, "UPDATE history_messages SET read=1 WHERE chat=? AND id=?", chat, id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Change(ctx context.Context, chat, id string, replacement *waE2E.Message) error {
	return s.ChangeAt(ctx, chat, id, replacement, s.now().UnixMilli())
}
func (s *Store) ChangeAt(ctx context.Context, chat, id string, replacement *waE2E.Message, at int64) error {
	if at <= 0 || at > s.now().Add(24*time.Hour).UnixMilli() {
		return errors.New("invalid message update timestamp")
	}
	if replacement == nil {
		content, _ := json.Marshal(map[string]any{"updatedAt": at})
		_, err := s.db.ExecContext(ctx, `UPDATE history_messages SET deleted=1,content=?,protocol=X'',bytes=? WHERE chat=? AND id=? AND coalesce(json_extract(content,'$.updatedAt'),t*1000)<=?`, content, len(content), chat, id, at)
		return err
	}
	r, err := s.Get(ctx, chat, id)
	if err != nil {
		return err
	}
	if proto.Size(replacement) > 256<<10 {
		return ErrTooLarge
	}
	raw, err := proto.Marshal(replacement)
	if err != nil {
		return err
	}
	data := media.Event(&events.Message{Info: r.Info(), Message: replacement})
	data["updatedAt"] = at
	content, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE history_messages SET edited=1,content=?,protocol=?,bytes=? WHERE chat=? AND id=? AND deleted=0 AND coalesce(json_extract(content,'$.updatedAt'),t*1000)<=?`, content, raw, len(content)+len(raw), chat, id, at)
	return err
}

// Purge removes retained local messages. S3 objects have separate retention.
func (s *Store) Purge(ctx context.Context, chat string) error {
	if chat != "" && !ValidChat(chat) {
		return errors.New("invalid chatId")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"history_messages", "history_votes", "history_receipts"} {
		query := "DELETE FROM " + table
		args := []any{}
		if chat != "" {
			query += " WHERE chat=?"
			args = append(args, chat)
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	query := "DELETE FROM history_label_links WHERE message<>''"
	args := []any{}
	if chat != "" {
		query += " AND chat=?"
		args = append(args, chat)
	}
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Receipt(ctx context.Context, evt *events.Receipt) error {
	if evt == nil || len(evt.MessageIDs) > 1000 {
		return errors.New("invalid receipt")
	}
	for _, id := range evt.MessageIDs {
		if !validID(id) {
			continue
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO history_receipts(chat,id,sender,kind,t) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM history_messages WHERE chat=? AND id=?) ON CONFLICT(chat,id,sender,kind) DO UPDATE SET t=MAX(t,excluded.t)`, evt.Chat.String(), id, evt.Sender.String(), string(evt.Type), evt.Timestamp.Unix(), evt.Chat.String(), id)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Receipts(ctx context.Context, chat, id string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT sender,kind,t FROM history_receipts WHERE chat=? AND id=? ORDER BY t,sender", chat, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var sender, kind string
		var t int64
		if err = rows.Scan(&sender, &kind, &t); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"sender": sender, "type": kind, "t": t})
	}
	return out, rows.Err()
}
func (s *Store) String() string {
	return fmt.Sprintf("session message store (%d messages, %s retention)", s.options.MaxMessages, s.options.Retention)
}
