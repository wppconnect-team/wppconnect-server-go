package session

import (
	"context"
	"go.mau.fi/whatsmeow/types"
	"time"

	"github.com/wppconnect-team/wppconnect-server-go/internal/media"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

// ProtocolEventSink handles receipts, labels, calls and channel events.
type ProtocolEventSink interface{ OnProtocolEvent(string, any) }

func (m *Manager) forwardProtocolEvent(h *Handle, evt any) {
	if sink, ok := m.sink.(ProtocolEventSink); ok {
		sink.OnProtocolEvent(h.Name, evt)
	}
}

func (m *Manager) applyMessageChange(ctx context.Context, h *Handle, evt *events.Message) {
	msg := media.Unwrap(evt.Message)
	if msg == nil {
		msg = media.Unwrap(evt.RawMessage)
	}
	change := msg.GetProtocolMessage()
	if change == nil {
		return
	}
	if change.GetType() != waE2E.ProtocolMessage_REVOKE && change.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT {
		return
	}
	r, err := h.History.Get(ctx, evt.Info.Chat.String(), change.GetKey().GetID())
	if err != nil {
		return
	}
	if change.GetKey().GetRemoteJID() != "" && change.GetKey().GetRemoteJID() != r.Chat {
		return
	}
	// Only the original author (including another own device) can change local
	// history here. Group-admin revocations remain visible as protocol events.
	authorized := (r.FromMe && evt.Info.IsFromMe) || (!r.FromMe && !evt.Info.IsFromMe && (r.Sender == evt.Info.Sender.ToNonAD().String() || r.Sender == evt.Info.SenderAlt.ToNonAD().String()))
	if !authorized {
		return
	}
	at := change.GetTimestampMS()
	if at == 0 {
		at = evt.Info.Timestamp.UnixMilli()
	}
	if change.GetType() == waE2E.ProtocolMessage_REVOKE {
		err = h.History.ChangeAt(ctx, r.Chat, r.ID, nil, at)
	} else {
		err = h.History.ChangeAt(ctx, r.Chat, r.ID, change.GetEditedMessage(), at)
	}
	if err != nil && m.log != nil {
		m.log.Warnf("message change persistence failed: %v", err)
	}
}

func (m *Manager) importHistory(h *Handle, evt *events.HistorySync) {
	if evt == nil || evt.Data == nil || h.History == nil || h.Client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancel()
	count := 0
	for _, conv := range evt.Data.GetConversations() {
		if ctx.Err() != nil || count >= 10000 {
			return
		}
		for _, item := range conv.GetMessages() {
			if count >= 10000 || ctx.Err() != nil {
				return
			}
			count++
			chat, err := types.ParseJID(conv.GetID())
			if err != nil {
				continue
			}
			parsed, err := h.Client.ParseWebMessage(chat, item.GetMessage())
			if err != nil {
				continue
			}
			if err = h.History.Put(ctx, parsed.Info, parsed.Message); err != nil && m.log != nil {
				m.log.Warnf("history sync message skipped: %v", err)
			}
		}
	}
}
