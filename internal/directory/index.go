package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	_ "modernc.org/sqlite"
)

const MaxChats = 10000

type Chat struct {
	JID         string
	Name        string
	T           int64
	Unread      uint32
	UnreadKnown bool
	Archived    bool
	Pinned      bool
	Muted       bool
	MuteUntil   int64
	MuteAt      int64
	ArchiveAt   int64
	PinAt       int64
	ReadAt      int64
	DeletedAt   int64
	Seen        []string
}

type Index struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS directory_chats (jid TEXT PRIMARY KEY,t INTEGER NOT NULL,data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS directory_chats_order ON directory_chats(t DESC,jid);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Index{db: db}, nil
}
func (s *Index) Close() error { return s.db.Close() }

func patch(tx *sql.Tx, jid types.JID, change func(*Chat)) error {
	jid = jid.ToNonAD()
	if !Supported(jid) {
		return nil
	}
	c := Chat{JID: jid.String(), Name: jid.User}
	var data []byte
	err := tx.QueryRow("SELECT data FROM directory_chats WHERE jid=?", jid.String()).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(data, &c); err != nil {
			return err
		}
	}
	change(&c)
	runes := []rune(c.Name)
	if len(runes) > 256 {
		c.Name = string(runes[:256])
	}
	data, err = json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO directory_chats(jid,t,data) VALUES(?,?,?) ON CONFLICT(jid) DO UPDATE SET t=excluded.t,data=excluded.data", jid.String(), c.T, data)
	return err
}

func (s *Index) Update(ctx context.Context, jid types.JID, change func(*Chat)) error {
	return s.transaction(ctx, func(tx *sql.Tx) error { return patch(tx, jid, change) })
}
func (s *Index) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM directory_chats WHERE jid IN (SELECT jid FROM directory_chats ORDER BY t DESC,jid LIMIT -1 OFFSET ?)", MaxChats)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Index) List(ctx context.Context) ([]Chat, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM directory_chats ORDER BY t DESC,jid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Chat, 0)
	for rows.Next() {
		var data []byte
		var c Chat
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		if c.DeletedAt == 0 {
			list = append(list, c)
		}
	}
	return list, rows.Err()
}

func (s *Index) Message(ctx context.Context, evt *events.Message) error {
	if evt == nil || !Supported(evt.Info.Chat) || evt.Info.ID == "" || len(evt.Info.ID) > 256 {
		return nil
	}
	return s.Update(ctx, evt.Info.Chat, func(c *Chat) {
		for _, id := range c.Seen {
			if id == evt.Info.ID {
				return
			}
		}
		c.Seen = append(c.Seen, evt.Info.ID)
		if len(c.Seen) > 64 {
			c.Seen = c.Seen[len(c.Seen)-64:]
		}
		t := evt.Info.Timestamp.Unix()
		if t < 0 {
			t = 0
		}
		if c.DeletedAt > 0 {
			if t < c.DeletedAt {
				return
			}
			c.DeletedAt = 0
		}
		if !evt.Info.IsFromMe && t > c.ReadAt && c.Unread < 1_000_000 {
			c.Unread++
		}
		if t > c.T {
			c.T = t
		}
	})
}

func (s *Index) Event(ctx context.Context, evt any) error {
	switch v := evt.(type) {
	case *events.Mute:
		if v == nil || v.Action == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			if v.Timestamp.Unix() >= c.MuteAt {
				c.Muted = v.Action.GetMuted()
				c.MuteUntil = v.Action.GetMuteEndTimestamp()
				c.MuteAt = v.Timestamp.Unix()
			}
		})
	case *events.HistorySync:
		if v == nil || v.Data == nil {
			return nil
		}
		return s.transaction(ctx, func(tx *sql.Tx) error {
			for _, conv := range v.Data.GetConversations() {
				jid, err := types.ParseJID(conv.GetID())
				if err != nil || !Supported(jid) {
					continue
				}
				err = patch(tx, jid, func(c *Chat) {
					t := int64(conv.GetLastMsgTimestamp())
					if t == 0 {
						t = int64(conv.GetConversationTimestamp())
					}
					if t < 0 {
						t = 0
					}
					if c.DeletedAt > 0 {
						if t <= c.DeletedAt {
							return
						}
						c.DeletedAt = 0
					}
					name := conv.GetName()
					if name == "" {
						name = conv.GetDisplayName()
					}
					if name != "" && (c.Name == jid.User || t >= c.T) {
						c.Name = name
					}
					if t >= c.T {
						c.T = t
						if conv.UnreadCount != nil && t >= c.ReadAt {
							c.Unread = conv.GetUnreadCount()
							c.UnreadKnown = true
						}
						messages := conv.GetMessages()
						for _, m := range messages[:min(len(messages), 64)] {
							if id := m.GetMessage().GetKey().GetID(); id != "" {
								seen := false
								for _, old := range c.Seen {
									if old == id {
										seen = true
										break
									}
								}
								if !seen && len(id) <= 256 {
									c.Seen = append(c.Seen, id)
								}
							}
						}
						if len(c.Seen) > 64 {
							c.Seen = c.Seen[len(c.Seen)-64:]
						}
					}
					if t >= c.ArchiveAt {
						if conv.Archived != nil {
							c.Archived = conv.GetArchived()
							c.ArchiveAt = t
						}
					}
					if t >= c.PinAt {
						if conv.Pinned != nil {
							c.Pinned = conv.GetPinned() != 0
							c.PinAt = t
						}
					}
				})
				if err != nil {
					return err
				}
			}
			return nil
		})
	case *events.Archive:
		if v == nil || v.Action == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			if t := v.Timestamp.Unix(); t >= c.ArchiveAt {
				c.Archived = v.Action.GetArchived()
				c.ArchiveAt = t
			}
		})
	case *events.Pin:
		if v == nil || v.Action == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			if t := v.Timestamp.Unix(); t >= c.PinAt {
				c.Pinned = v.Action.GetPinned()
				c.PinAt = t
			}
		})
	case *events.MarkChatAsRead:
		if v == nil || v.Action == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			if t := v.Timestamp.Unix(); t >= c.ReadAt {
				c.ReadAt = t
				if v.Action.GetRead() {
					c.Unread = 0
					c.UnreadKnown = true
				} else if c.Unread == 0 {
					c.Unread = 1
					c.UnreadKnown = false
				}
			}
		})
	case *events.DeleteChat:
		if v == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			t := v.Timestamp.Unix()
			if t > c.DeletedAt && t >= c.T {
				c.DeletedAt = t
				c.Unread = 0
				c.UnreadKnown = true
				c.ReadAt = t
				c.Archived = false
				c.Pinned = false
				c.ArchiveAt = t
				c.PinAt = t
			}
		})
	case *events.GroupInfo:
		if v == nil || v.Name == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) { c.Name = v.Name.Name })
	case *events.JoinedGroup:
		if v == nil {
			return nil
		}
		return s.Update(ctx, v.JID, func(c *Chat) {
			c.Name = v.Name
			if c.T == 0 && !v.GroupCreated.IsZero() {
				c.T = v.GroupCreated.Unix()
			}
		})
	}
	return nil
}

// State belongs to exactly one connected session. Presence is never persisted.
type State struct {
	Chats      *Index
	Presence   *PresenceCache
	commands   sync.Mutex
	generation uint64
	owners     map[types.JID]uint64
	closed     bool
}

func NewState(index *Index) *State {
	return &State{Chats: index, Presence: NewPresenceCache(), owners: make(map[types.JID]uint64)}
}

func (s *State) ResetPresence() {
	s.commands.Lock()
	defer s.commands.Unlock()
	s.Presence.Clear()
	clear(s.owners)
}
func (s *State) Close() error {
	s.commands.Lock()
	s.closed = true
	clear(s.owners)
	s.Presence.Clear()
	s.commands.Unlock()
	return s.Chats.Close()
}
func (s *State) Observe(evt any) error {
	if s == nil {
		return nil
	}
	if v, ok := evt.(*events.Presence); ok {
		s.Presence.Observe(v)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if m, ok := evt.(*events.Message); ok {
		return s.Chats.Message(ctx, m)
	}
	return s.Chats.Event(ctx, evt)
}
