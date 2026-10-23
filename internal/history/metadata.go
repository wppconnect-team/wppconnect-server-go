package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/types/events"
)

type Label struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   int32  `json:"color"`
	Deleted bool   `json:"deleted"`
	Updated int64  `json:"updatedAt"`
}

func (s *Store) SetLabel(ctx context.Context, v Label) error {
	if !validID(v.ID) || !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) > 100 || (!v.Deleted && v.Name == "") || v.Color < 0 || v.Color > 19 {
		return errors.New("invalid label ID, name (1..100) or color (0..19)")
	}
	if v.Updated == 0 {
		v.Updated = s.now().UnixMilli()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO history_labels(id,name,color,deleted,updated) SELECT ?,?,?,?,? WHERE (SELECT count(*) FROM history_labels)<1000 OR EXISTS(SELECT 1 FROM history_labels WHERE id=?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,color=excluded.color,deleted=excluded.deleted,updated=excluded.updated WHERE excluded.updated>=updated`, v.ID, v.Name, v.Color, v.Deleted, v.Updated, v.ID)
	return s.quotaResult(ctx, result, err, "SELECT 1 FROM history_labels WHERE id=?", v.ID)
}
func (s *Store) Labels(ctx context.Context) ([]Label, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,color,deleted,updated FROM history_labels WHERE deleted=0 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var v Label
		if err = rows.Scan(&v.ID, &v.Name, &v.Color, &v.Deleted, &v.Updated); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Associate(ctx context.Context, label, chat, message string, active bool, at int64) error {
	if !validID(label) || !ValidChat(chat) || (message != "" && !validID(message)) {
		return errors.New("invalid label association")
	}
	if at == 0 {
		at = s.now().UnixMilli()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO history_label_links(label,chat,message,active,updated) SELECT ?,?,?,?,? WHERE (SELECT count(*) FROM history_label_links)<100000 OR EXISTS(SELECT 1 FROM history_label_links WHERE label=? AND chat=? AND message=?) ON CONFLICT(label,chat,message) DO UPDATE SET active=excluded.active,updated=excluded.updated WHERE excluded.updated>=updated`, label, chat, message, active, at, label, chat, message)
	return s.quotaResult(ctx, result, err, "SELECT 1 FROM history_label_links WHERE label=? AND chat=? AND message=?", label, chat, message)
}
func (s *Store) ChatLabels(ctx context.Context, chat, message string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.label FROM history_label_links l JOIN history_labels d ON d.id=l.label WHERE l.chat=? AND l.message=? AND l.active=1 AND d.deleted=0 ORDER BY l.label`, chat, message)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ChatLabelSets returns active chat labels only, without message associations.
func (s *Store) ChatLabelSets(ctx context.Context) (map[string]map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.chat,l.label FROM history_label_links l JOIN history_labels d ON d.id=l.label WHERE l.message='' AND l.active=1 AND d.deleted=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var chat, label string
		if err := rows.Scan(&chat, &label); err != nil {
			return nil, err
		}
		if out[chat] == nil {
			out[chat] = map[string]bool{}
		}
		out[chat][label] = true
	}
	return out, rows.Err()
}
func (s *Store) ObserveLabels(ctx context.Context, evt any) error {
	switch v := evt.(type) {
	case *events.LabelEdit:
		if v != nil && v.Action != nil {
			return s.SetLabel(ctx, Label{v.LabelID, v.Action.GetName(), v.Action.GetColor(), v.Action.GetDeleted(), v.Timestamp.UnixMilli()})
		}
	case *events.LabelAssociationChat:
		if v != nil && v.Action != nil {
			return s.Associate(ctx, v.LabelID, v.JID.String(), "", v.Action.GetLabeled(), v.Timestamp.UnixMilli())
		}
	case *events.LabelAssociationMessage:
		if v != nil && v.Action != nil {
			return s.Associate(ctx, v.LabelID, v.JID.String(), v.MessageID, v.Action.GetLabeled(), v.Timestamp.UnixMilli())
		}
	}
	return nil
}

func pollChoices(r Record) ([]string, int, error) {
	if r.Deleted {
		return nil, 0, ErrNotFound
	}
	p := r.Message.GetPollCreationMessage()
	if p == nil {
		p = r.Message.GetPollCreationMessageV2()
	}
	if p == nil {
		p = r.Message.GetPollCreationMessageV3()
	}
	if p == nil {
		p = r.Message.GetPollCreationMessageV5()
	}
	if p == nil {
		p = r.Message.GetPollCreationMessageV6()
	}
	if p == nil || len(p.GetOptions()) < 2 || len(p.GetOptions()) > 12 {
		return nil, 0, errors.New("message is not a supported poll")
	}
	out := make([]string, len(p.GetOptions()))
	for i, v := range p.GetOptions() {
		out[i] = v.GetOptionName()
	}
	return out, int(p.GetSelectableOptionsCount()), nil
}

func PollOptions(r Record) ([]string, int, error) { return pollChoices(r) }
func (s *Store) Vote(ctx context.Context, chat, poll, voter string, at int64, hashes [][]byte) error {
	r, err := s.Get(ctx, chat, poll)
	if err != nil {
		return err
	}
	choices, selectable, err := pollChoices(r)
	if err != nil {
		return err
	}
	if !ValidChat(voter) || len(hashes) > len(choices) || (selectable > 0 && len(hashes) > selectable) || at <= 0 || at > s.now().Add(24*time.Hour).UnixMilli() {
		return errors.New("invalid poll vote")
	}
	known := map[string]bool{}
	for _, choice := range choices {
		h := sha256.Sum256([]byte(choice))
		known[hex.EncodeToString(h[:])] = true
	}
	selected := make([]string, 0, len(hashes))
	seen := map[string]bool{}
	for _, h := range hashes {
		k := hex.EncodeToString(h)
		if len(h) != 32 || !known[k] || seen[k] {
			return errors.New("unknown or duplicate poll option hash")
		}
		seen[k] = true
		selected = append(selected, k)
	}
	b, _ := json.Marshal(selected)
	result, err := s.db.ExecContext(ctx, `INSERT INTO history_votes(chat,poll,voter,updated,choices) SELECT ?,?,?,?,? WHERE (SELECT count(*) FROM history_votes)<100000 OR EXISTS(SELECT 1 FROM history_votes WHERE chat=? AND poll=? AND voter=?) ON CONFLICT(chat,poll,voter) DO UPDATE SET updated=excluded.updated,choices=excluded.choices WHERE excluded.updated>updated`, chat, poll, voter, at, b, chat, poll, voter)
	return s.quotaResult(ctx, result, err, "SELECT 1 FROM history_votes WHERE chat=? AND poll=? AND voter=?", chat, poll, voter)
}
func (s *Store) quotaResult(ctx context.Context, result sql.Result, err error, query string, args ...any) error {
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n > 0 {
		return err
	}
	var found int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return errors.New("history metadata quota reached")
	} else {
		return err
	}
}
func (s *Store) PollResults(ctx context.Context, chat, id string) (map[string]any, error) {
	r, err := s.Get(ctx, chat, id)
	if err != nil {
		return nil, err
	}
	choices, selectable, err := pollChoices(r)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	keys := map[string]string{}
	for _, choice := range choices {
		h := sha256.Sum256([]byte(choice))
		k := hex.EncodeToString(h[:])
		keys[k] = choice
		counts[choice] = 0
	}
	rows, err := s.db.QueryContext(ctx, "SELECT choices FROM history_votes WHERE chat=? AND poll=?", chat, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	voters := 0
	for rows.Next() {
		var b []byte
		var selected []string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &selected); err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			voters++
		}
		for _, key := range selected {
			if name, ok := keys[key]; ok {
				counts[name]++
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "chatId": chat, "choices": choices, "selectableCount": selectable, "counts": counts, "voters": voters, "isPartial": true}, nil
}
