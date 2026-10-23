// Package channels defines the SDK seam for communities, newsletters and calls.
package channels

import (
	"context"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"time"
)

type Client interface {
	CreateGroup(context.Context, whatsmeow.ReqCreateGroup) (*types.GroupInfo, error)
	GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error)
	GetSubGroups(context.Context, types.JID) ([]*types.GroupLinkTarget, error)
	LinkGroup(context.Context, types.JID, types.JID) error
	UnlinkGroup(context.Context, types.JID, types.JID) error
	SetGroupDescription(context.Context, types.JID, string) error
	GetSubscribedNewsletters(context.Context) ([]*types.NewsletterMetadata, error)
	GetNewsletterInfo(context.Context, types.JID) (*types.NewsletterMetadata, error)
	CreateNewsletter(context.Context, whatsmeow.CreateNewsletterParams) (*types.NewsletterMetadata, error)
	FollowNewsletter(context.Context, types.JID) error
	UnfollowNewsletter(context.Context, types.JID) error
	NewsletterToggleMute(context.Context, types.JID, bool) error
	NewsletterSubscribeLiveUpdates(context.Context, types.JID) (time.Duration, error)
	GetNewsletterMessages(context.Context, types.JID, *whatsmeow.GetNewsletterMessagesParams) ([]*types.NewsletterMessage, error)
	RejectCall(context.Context, types.JID, string) error
	BuildPollVote(context.Context, *types.MessageInfo, []string) (*waE2E.Message, error)
	SendMessage(context.Context, types.JID, *waE2E.Message, ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
	Self() types.JID
}
type SDK struct{ *whatsmeow.Client }

func (s SDK) Self() types.JID {
	if s.Store == nil || s.Store.ID == nil {
		return types.EmptyJID
	}
	return s.Store.ID.ToNonAD()
}
