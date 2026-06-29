// Code generated from wppconnect-server/src/routes/index.ts compatibility surface; DO NOT EDIT MANUALLY.
package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.mau.fi/whatsmeow"
)

type compatibilityRoute struct {
	method     string
	path       string
	capability string
}

var nodeCompatibilityRoutes = []compatibilityRoute{
	{method: "POST", path: "/api/{session}/{secretkey}/generate-token", capability: "session"},
	{method: "GET", path: "/api/{secretkey}/show-all-sessions", capability: "session"},
	{method: "POST", path: "/api/{secretkey}/start-all", capability: "compatibility"},
	{method: "GET", path: "/api/{session}/check-connection-session", capability: "session"},
	{method: "GET", path: "/api/{session}/get-media-by-message/{messageId}", capability: "chats"},
	{method: "GET", path: "/api/{session}/get-platform-from-message/{messageId}", capability: "chats"},
	{method: "POST", path: "/api/{session}/{secretkey}/clear-session-data", capability: "session"},
	{method: "POST", path: "/api/{session}/subscribe-presence", capability: "session"},
	{method: "POST", path: "/api/{session}/set-online-presence", capability: "session"},
	{method: "POST", path: "/api/{session}/download-media", capability: "messaging"},
	{method: "POST", path: "/api/{session}/edit-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/send-sticker", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-sticker-gif", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-reply", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-file", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-voice", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-voice-base64", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-status", capability: "stories"},
	{method: "POST", path: "/api/{session}/send-link-preview", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-mentioned", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-buttons", capability: "messaging"},
	{method: "POST", path: "/api/{session}/send-list-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/send-order-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/send-poll-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/send-pix-key", capability: "messaging"},
	{method: "GET", path: "/api/{session}/all-broadcast-list", capability: "groups"},
	{method: "GET", path: "/api/{session}/common-groups/{wid}", capability: "groups"},
	{method: "GET", path: "/api/{session}/group-admins/{groupId}", capability: "groups"},
	{method: "GET", path: "/api/{session}/group-info/{groupId}", capability: "groups"},
	{method: "GET", path: "/api/{session}/group-invite-link/{groupId}", capability: "groups"},
	{method: "GET", path: "/api/{session}/group-revoke-link/{groupId}", capability: "groups"},
	{method: "GET", path: "/api/{session}/group-members-ids/{groupId}", capability: "groups"},
	{method: "POST", path: "/api/{session}/leave-group", capability: "groups"},
	{method: "POST", path: "/api/{session}/join-code", capability: "compatibility"},
	{method: "POST", path: "/api/{session}/add-participant-group", capability: "groups"},
	{method: "POST", path: "/api/{session}/remove-participant-group", capability: "groups"},
	{method: "POST", path: "/api/{session}/promote-participant-group", capability: "groups"},
	{method: "POST", path: "/api/{session}/demote-participant-group", capability: "groups"},
	{method: "POST", path: "/api/{session}/group-info-from-invite-link", capability: "groups"},
	{method: "POST", path: "/api/{session}/group-description", capability: "groups"},
	{method: "POST", path: "/api/{session}/group-property", capability: "groups"},
	{method: "POST", path: "/api/{session}/group-subject", capability: "groups"},
	{method: "POST", path: "/api/{session}/messages-admins-only", capability: "chats"},
	{method: "POST", path: "/api/{session}/group-pic", capability: "groups"},
	{method: "POST", path: "/api/{session}/change-privacy-group", capability: "groups"},
	{method: "GET", path: "/api/{session}/all-chats", capability: "chats"},
	{method: "POST", path: "/api/{session}/list-chats", capability: "compatibility"},
	{method: "GET", path: "/api/{session}/all-chats-archived", capability: "chats"},
	{method: "GET", path: "/api/{session}/all-chats-with-messages", capability: "chats"},
	{method: "GET", path: "/api/{session}/all-messages-in-chat/{phone}", capability: "chats"},
	{method: "GET", path: "/api/{session}/all-new-messages", capability: "chats"},
	{method: "GET", path: "/api/{session}/unread-messages", capability: "chats"},
	{method: "GET", path: "/api/{session}/all-unread-messages", capability: "chats"},
	{method: "GET", path: "/api/{session}/chat-by-id/{phone}", capability: "chats"},
	{method: "GET", path: "/api/{session}/message-by-id/{messageId}", capability: "chats"},
	{method: "GET", path: "/api/{session}/chat-is-online/{phone}", capability: "chats"},
	{method: "GET", path: "/api/{session}/last-seen/{phone}", capability: "chats"},
	{method: "GET", path: "/api/{session}/list-mutes/{type}", capability: "chats"},
	{method: "GET", path: "/api/{session}/load-messages-in-chat/{phone}", capability: "chats"},
	{method: "GET", path: "/api/{session}/get-messages/{phone}", capability: "chats"},
	{method: "POST", path: "/api/{session}/archive-chat", capability: "chats"},
	{method: "POST", path: "/api/{session}/archive-all-chats", capability: "chats"},
	{method: "POST", path: "/api/{session}/clear-chat", capability: "compatibility"},
	{method: "POST", path: "/api/{session}/clear-all-chats", capability: "chats"},
	{method: "POST", path: "/api/{session}/delete-chat", capability: "compatibility"},
	{method: "POST", path: "/api/{session}/delete-all-chats", capability: "chats"},
	{method: "POST", path: "/api/{session}/delete-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/react-message", capability: "chats"},
	{method: "POST", path: "/api/{session}/forward-messages", capability: "chats"},
	{method: "POST", path: "/api/{session}/mark-unseen", capability: "chats"},
	{method: "POST", path: "/api/{session}/pin-chat", capability: "chats"},
	{method: "POST", path: "/api/{session}/contact-vcard", capability: "contacts"},
	{method: "POST", path: "/api/{session}/send-mute", capability: "chats"},
	{method: "POST", path: "/api/{session}/chat-state", capability: "chats"},
	{method: "POST", path: "/api/{session}/temporary-messages", capability: "chats"},
	{method: "POST", path: "/api/{session}/typing", capability: "chats"},
	{method: "POST", path: "/api/{session}/recording", capability: "chats"},
	{method: "POST", path: "/api/{session}/star-message", capability: "chats"},
	{method: "GET", path: "/api/{session}/reactions/{id}", capability: "chats"},
	{method: "GET", path: "/api/{session}/votes/{id}", capability: "chats"},
	{method: "POST", path: "/api/{session}/reject-call", capability: "chats"},
	{method: "GET", path: "/api/{session}/get-products", capability: "catalog"},
	{method: "GET", path: "/api/{session}/get-product-by-id", capability: "catalog"},
	{method: "POST", path: "/api/{session}/add-product", capability: "catalog"},
	{method: "POST", path: "/api/{session}/edit-product", capability: "catalog"},
	{method: "POST", path: "/api/{session}/del-products", capability: "catalog"},
	{method: "POST", path: "/api/{session}/change-product-image", capability: "catalog"},
	{method: "POST", path: "/api/{session}/add-product-image", capability: "catalog"},
	{method: "POST", path: "/api/{session}/remove-product-image", capability: "catalog"},
	{method: "GET", path: "/api/{session}/get-collections", capability: "catalog"},
	{method: "POST", path: "/api/{session}/create-collection", capability: "catalog"},
	{method: "POST", path: "/api/{session}/edit-collection", capability: "catalog"},
	{method: "POST", path: "/api/{session}/del-collection", capability: "catalog"},
	{method: "POST", path: "/api/{session}/send-link-catalog", capability: "catalog"},
	{method: "POST", path: "/api/{session}/set-product-visibility", capability: "catalog"},
	{method: "POST", path: "/api/{session}/set-cart-enabled", capability: "catalog"},
	{method: "POST", path: "/api/{session}/send-text-storie", capability: "stories"},
	{method: "POST", path: "/api/{session}/send-image-storie", capability: "stories"},
	{method: "POST", path: "/api/{session}/send-video-storie", capability: "stories"},
	{method: "POST", path: "/api/{session}/add-new-label", capability: "labels"},
	{method: "POST", path: "/api/{session}/add-or-remove-label", capability: "labels"},
	{method: "GET", path: "/api/{session}/get-all-labels", capability: "labels"},
	{method: "PUT", path: "/api/{session}/delete-all-labels", capability: "labels"},
	{method: "PUT", path: "/api/{session}/delete-label/{id}", capability: "labels"},
	{method: "GET", path: "/api/{session}/all-contacts", capability: "contacts"},
	{method: "GET", path: "/api/{session}/contact/{phone}", capability: "contacts"},
	{method: "GET", path: "/api/{session}/contact/pn-lid/{pnLid}", capability: "contacts"},
	{method: "GET", path: "/api/{session}/profile/{phone}", capability: "contacts"},
	{method: "GET", path: "/api/{session}/profile-pic/{phone}", capability: "contacts"},
	{method: "GET", path: "/api/{session}/profile-status/{phone}", capability: "contacts"},
	{method: "GET", path: "/api/{session}/blocklist", capability: "contacts"},
	{method: "POST", path: "/api/{session}/block-contact", capability: "contacts"},
	{method: "POST", path: "/api/{session}/unblock-contact", capability: "contacts"},
	{method: "GET", path: "/api/{session}/get-battery-level", capability: "device"},
	{method: "GET", path: "/api/{session}/host-device", capability: "device"},
	{method: "GET", path: "/api/{session}/get-phone-number", capability: "contacts"},
	{method: "POST", path: "/api/{session}/set-profile-pic", capability: "contacts"},
	{method: "POST", path: "/api/{session}/profile-status", capability: "contacts"},
	{method: "POST", path: "/api/{session}/change-username", capability: "compatibility"},
	{method: "POST", path: "/api/{session}/edit-business-profile", capability: "contacts"},
	{method: "GET", path: "/api/{session}/get-business-profiles-products", capability: "catalog"},
	{method: "GET", path: "/api/{session}/get-order-by-messageId/{messageId}", capability: "chats"},
	{method: "GET", path: "/api/{secretkey}/backup-sessions", capability: "session"},
	{method: "POST", path: "/api/{secretkey}/restore-sessions", capability: "session"},
	{method: "GET", path: "/api/{session}/take-screenshot", capability: "screenshot"},
	{method: "POST", path: "/api/{session}/set-limit", capability: "session"},
	{method: "POST", path: "/api/{session}/create-community", capability: "community"},
	{method: "POST", path: "/api/{session}/deactivate-community", capability: "community"},
	{method: "POST", path: "/api/{session}/add-community-subgroup", capability: "groups"},
	{method: "POST", path: "/api/{session}/remove-community-subgroup", capability: "groups"},
	{method: "POST", path: "/api/{session}/promote-community-participant", capability: "community"},
	{method: "POST", path: "/api/{session}/demote-community-participant", capability: "community"},
	{method: "GET", path: "/api/{session}/community-participants/{id}", capability: "community"},
	{method: "POST", path: "/api/{session}/newsletter", capability: "newsletter"},
	{method: "PUT", path: "/api/{session}/newsletter/{id}", capability: "newsletter"},
	{method: "DELETE", path: "/api/{session}/newsletter/{id}", capability: "newsletter"},
	{method: "POST", path: "/api/{session}/mute-newsletter/{id}", capability: "newsletter"},
	{method: "POST", path: "/api/{session}/chatwoot", capability: "compatibility"},
	{method: "GET", path: "/api-docs", capability: "docs"},
	{method: "GET", path: "/unhealthy", capability: "health"},
	{method: "GET", path: "/metrics", capability: "metrics"},
}

func (s *Server) registerNodeCompatibilityRoutes(public chi.Router, protected chi.Router) {
	for _, route := range nodeCompatibilityRoutes {
		target := public
		if requiresSessionToken(route.path) {
			target = protected
		}
		target.Method(route.method, route.path, s.compatibilityHandler(route))
	}
}

func (s *Server) compatibilityHandler(route compatibilityRoute) http.HandlerFunc {
	switch route.path {
	case "/api/{session}/check-connection-session":
		return s.statusSession
	case "/api/{session}/send-file":
		return s.sendFile
	case "/api/{session}/send-voice", "/api/{session}/send-voice-base64":
		return s.sendVoice
	case "/api/{session}/group-info/{groupId}":
		return func(w http.ResponseWriter, r *http.Request) { s.groupInfo(w, r, "info") }
	case "/api/{session}/group-members-ids/{groupId}":
		return func(w http.ResponseWriter, r *http.Request) { s.groupInfo(w, r, "memberIDs") }
	case "/api/{session}/group-admins/{groupId}":
		return func(w http.ResponseWriter, r *http.Request) { s.groupInfo(w, r, "admins") }
	case "/api/{session}/leave-group":
		return s.leaveGroup
	case "/api/{session}/add-participant-group":
		return s.updateGroupParticipants(whatsmeow.ParticipantChangeAdd)
	case "/api/{session}/remove-participant-group":
		return s.updateGroupParticipants(whatsmeow.ParticipantChangeRemove)
	case "/api/{session}/promote-participant-group":
		return s.updateGroupParticipants(whatsmeow.ParticipantChangePromote)
	case "/api/{session}/demote-participant-group":
		return s.updateGroupParticipants(whatsmeow.ParticipantChangeDemote)
	case "/api/{session}/group-subject":
		return s.setGroupSubject
	case "/api/{session}/group-description":
		return s.setGroupDescription
	default:
		return s.notSupported(route.capability)
	}
}

func requiresSessionToken(path string) bool {
	if !strings.Contains(path, "{session}") {
		return false
	}
	if strings.Contains(path, "{secretkey}") || strings.HasSuffix(path, "/chatwoot") {
		return false
	}
	return true
}
