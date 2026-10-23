// Package account validates own-profile and privacy operations.
package account

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Client interface {
	TryFetchPrivacySettings(context.Context, bool) (*types.PrivacySettings, error)
	SetPrivacySetting(context.Context, types.PrivacySettingType, types.PrivacySetting) (types.PrivacySettings, error)
	GetBlocklist(context.Context) (*types.Blocklist, error)
	UpdateBlocklist(context.Context, types.JID, events.BlocklistChangeAction) (*types.Blocklist, error)
	SetStatusMessage(context.Context, string) error
	SetGroupPhoto(context.Context, types.JID, []byte) (string, error)
	SendAppState(context.Context, appstate.PatchInfo) error
	Self() types.JID
}
type SDK struct{ *whatsmeow.Client }

func (s SDK) Self() types.JID {
	if s.Store == nil || s.Store.ID == nil {
		return types.EmptyJID
	}
	return s.Store.ID.ToNonAD()
}

// ValidatePrivacy deliberately accepts values whose effects the SDK models.
// Managing privacy exception lists is separate from selecting a policy.
func ValidatePrivacy(name, value string) error {
	allowed := map[string][]string{
		"groupadd": {"all", "contacts", "none"}, "last": {"all", "contacts", "none"}, "status": {"all", "contacts", "none"}, "profile": {"all", "contacts", "none"},
		"readreceipts": {"all", "none"}, "online": {"all", "match_last_seen"}, "calladd": {"all", "known"}, "messages": {"all", "contacts"}, "defense": {"on_standard", "off"}, "stickers": {"contacts", "none"},
	}
	for _, v := range allowed[name] {
		if v == value {
			return nil
		}
	}
	return errors.New("unsupported privacy setting/value; exception lists are not managed by this interface")
}
func Text(value string, limit int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > limit || strings.ContainsRune(value, 0) {
		return errors.New("invalid profile text or length")
	}
	return nil
}
