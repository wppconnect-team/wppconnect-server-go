package history

import (
	"context"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
)

// ObserveStar applies only provider events for a retained message. Older sync
// data cannot overwrite a later patch, and pruning the message removes its row.
func (s *Store) ObserveStar(ctx context.Context, event *events.Star) error {
	if event == nil || event.Action == nil || event.Action.Starred == nil || event.Timestamp.IsZero() {
		return nil
	}
	return s.observeStar(ctx, event.ChatJID, event.MessageID, event.IsFromMe, event.SenderJID, event.Action.GetStarred(), event.Timestamp.UnixMilli())
}

// A history snapshot has no change timestamp. It can initialize an unknown
// value but cannot reverse a later patch.
func (s *Store) SnapshotStar(ctx context.Context, info types.MessageInfo, starred bool) error {
	return s.observeStar(ctx, info.Chat, info.ID, info.IsFromMe, info.Sender, starred, 0)
}

func (s *Store) observeStar(ctx context.Context, chat types.JID, id string, fromMe bool, sender types.JID, starred bool, at int64) error {
	if chat.Server == types.GroupServer && !fromMe && sender.IsEmpty() {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO history_stars(message_seq,starred,changed_at)
 SELECT seq,?,? FROM history_messages WHERE chat=? AND id=? AND from_me=? AND deleted=0
 AND (?='' OR sender=?) ON CONFLICT(message_seq) DO UPDATE SET starred=excluded.starred,changed_at=excluded.changed_at
 WHERE excluded.changed_at>=history_stars.changed_at AND excluded.changed_at>0`, starred, at, chat.ToNonAD().String(), id, fromMe, sender.ToNonAD().String(), sender.ToNonAD().String())
	return err
}
