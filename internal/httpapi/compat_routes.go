// Code generated from wppconnect-server/src/routes/index.ts compatibility surface; DO NOT EDIT MANUALLY.
package httpapi

import (
	"strings"

	"github.com/go-chi/chi/v5"
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
	{method: "GET", path: "/api/{session}/get-media-by-message/{messageId}", capability: "session"},
	{method: "GET", path: "/api/{session}/get-platform-from-message/{messageId}", capability: "session"},
	{method: "POST", path: "/api/{session}/{secretkey}/clear-session-data", capability: "session"},
	{method: "POST", path: "/api/{session}/subscribe-presence", capability: "session"},
	{method: "POST", path: "/api/{session}/set-online-presence", capability: "session"},
	{method: "POST", path: "/api/{session}/download-media", capability: "session"},
	{method: "POST", path: "/api/{session}/edit-message", capability: "session"},
	{method: "POST", path: "/api/{session}/send-sticker", capability: "session"},
	{method: "POST", path: "/api/{session}/send-sticker-gif", capability: "session"},
	{method: "POST", path: "/api/{session}/send-reply", capability: "session"},
	{method: "POST", path: "/api/{session}/send-file", capability: "session"},
	{method: "POST", path: "/api/{session}/send-voice", capability: "session"},
	{method: "POST", path: "/api/{session}/send-voice-base64", capability: "session"},
	{method: "POST", path: "/api/{session}/send-status", capability: "session"},
	{method: "POST", path: "/api/{session}/send-link-preview", capability: "session"},
	{method: "POST", path: "/api/{session}/send-mentioned", capability: "session"},
	{method: "POST", path: "/api/{session}/send-buttons", capability: "session"},
	{method: "POST", path: "/api/{session}/send-list-message", capability: "session"},
	{method: "POST", path: "/api/{session}/send-order-message", capability: "session"},
	{method: "POST", path: "/api/{session}/send-poll-message", capability: "session"},
	{method: "POST", path: "/api/{session}/send-pix-key", capability: "session"},
	{method: "GET", path: "/api/{session}/all-broadcast-list", capability: "session"},
	{method: "GET", path: "/api/{session}/common-groups/{wid}", capability: "session"},
	{method: "GET", path: "/api/{session}/group-admins/{groupId}", capability: "session"},
	{method: "GET", path: "/api/{session}/group-info/{groupId}", capability: "session"},
	{method: "GET", path: "/api/{session}/group-invite-link/{groupId}", capability: "session"},
	{method: "GET", path: "/api/{session}/group-revoke-link/{groupId}", capability: "session"},
	{method: "GET", path: "/api/{session}/group-members-ids/{groupId}", capability: "session"},
	{method: "POST", path: "/api/{session}/leave-group", capability: "session"},
	{method: "POST", path: "/api/{session}/join-code", capability: "session"},
	{method: "POST", path: "/api/{session}/add-participant-group", capability: "session"},
	{method: "POST", path: "/api/{session}/remove-participant-group", capability: "session"},
	{method: "POST", path: "/api/{session}/promote-participant-group", capability: "session"},
	{method: "POST", path: "/api/{session}/demote-participant-group", capability: "session"},
	{method: "POST", path: "/api/{session}/group-info-from-invite-link", capability: "session"},
	{method: "POST", path: "/api/{session}/group-description", capability: "session"},
	{method: "POST", path: "/api/{session}/group-property", capability: "session"},
	{method: "POST", path: "/api/{session}/group-subject", capability: "session"},
	{method: "POST", path: "/api/{session}/messages-admins-only", capability: "session"},
	{method: "POST", path: "/api/{session}/group-pic", capability: "session"},
	{method: "POST", path: "/api/{session}/change-privacy-group", capability: "session"},
	{method: "GET", path: "/api/{session}/all-chats", capability: "session"},
	{method: "POST", path: "/api/{session}/list-chats", capability: "session"},
	{method: "GET", path: "/api/{session}/all-chats-archived", capability: "session"},
	{method: "GET", path: "/api/{session}/all-chats-with-messages", capability: "session"},
	{method: "GET", path: "/api/{session}/all-messages-in-chat/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/all-new-messages", capability: "session"},
	{method: "GET", path: "/api/{session}/unread-messages", capability: "session"},
	{method: "GET", path: "/api/{session}/all-unread-messages", capability: "session"},
	{method: "GET", path: "/api/{session}/chat-by-id/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/message-by-id/{messageId}", capability: "session"},
	{method: "GET", path: "/api/{session}/chat-is-online/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/last-seen/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/list-mutes/{type}", capability: "session"},
	{method: "GET", path: "/api/{session}/load-messages-in-chat/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/get-messages/{phone}", capability: "session"},
	{method: "POST", path: "/api/{session}/archive-chat", capability: "session"},
	{method: "POST", path: "/api/{session}/archive-all-chats", capability: "session"},
	{method: "POST", path: "/api/{session}/clear-chat", capability: "session"},
	{method: "POST", path: "/api/{session}/clear-all-chats", capability: "session"},
	{method: "POST", path: "/api/{session}/delete-chat", capability: "session"},
	{method: "POST", path: "/api/{session}/delete-all-chats", capability: "session"},
	{method: "POST", path: "/api/{session}/delete-message", capability: "session"},
	{method: "POST", path: "/api/{session}/react-message", capability: "session"},
	{method: "POST", path: "/api/{session}/forward-messages", capability: "session"},
	{method: "POST", path: "/api/{session}/mark-unseen", capability: "session"},
	{method: "POST", path: "/api/{session}/pin-chat", capability: "session"},
	{method: "POST", path: "/api/{session}/contact-vcard", capability: "session"},
	{method: "POST", path: "/api/{session}/send-mute", capability: "session"},
	{method: "POST", path: "/api/{session}/chat-state", capability: "session"},
	{method: "POST", path: "/api/{session}/temporary-messages", capability: "session"},
	{method: "POST", path: "/api/{session}/typing", capability: "session"},
	{method: "POST", path: "/api/{session}/recording", capability: "session"},
	{method: "POST", path: "/api/{session}/star-message", capability: "session"},
	{method: "GET", path: "/api/{session}/reactions/{id}", capability: "session"},
	{method: "GET", path: "/api/{session}/votes/{id}", capability: "session"},
	{method: "POST", path: "/api/{session}/reject-call", capability: "session"},
	{method: "GET", path: "/api/{session}/get-products", capability: "session"},
	{method: "GET", path: "/api/{session}/get-product-by-id", capability: "session"},
	{method: "POST", path: "/api/{session}/add-product", capability: "session"},
	{method: "POST", path: "/api/{session}/edit-product", capability: "session"},
	{method: "POST", path: "/api/{session}/del-products", capability: "session"},
	{method: "POST", path: "/api/{session}/change-product-image", capability: "session"},
	{method: "POST", path: "/api/{session}/add-product-image", capability: "session"},
	{method: "POST", path: "/api/{session}/remove-product-image", capability: "session"},
	{method: "GET", path: "/api/{session}/get-collections", capability: "session"},
	{method: "POST", path: "/api/{session}/create-collection", capability: "session"},
	{method: "POST", path: "/api/{session}/edit-collection", capability: "session"},
	{method: "POST", path: "/api/{session}/del-collection", capability: "session"},
	{method: "POST", path: "/api/{session}/send-link-catalog", capability: "session"},
	{method: "POST", path: "/api/{session}/set-product-visibility", capability: "session"},
	{method: "POST", path: "/api/{session}/set-cart-enabled", capability: "session"},
	{method: "POST", path: "/api/{session}/send-text-storie", capability: "session"},
	{method: "POST", path: "/api/{session}/send-image-storie", capability: "session"},
	{method: "POST", path: "/api/{session}/send-video-storie", capability: "session"},
	{method: "POST", path: "/api/{session}/add-new-label", capability: "session"},
	{method: "POST", path: "/api/{session}/add-or-remove-label", capability: "session"},
	{method: "GET", path: "/api/{session}/get-all-labels", capability: "session"},
	{method: "PUT", path: "/api/{session}/delete-all-labels", capability: "session"},
	{method: "PUT", path: "/api/{session}/delete-label/{id}", capability: "session"},
	{method: "GET", path: "/api/{session}/all-contacts", capability: "session"},
	{method: "GET", path: "/api/{session}/contact/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/contact/pn-lid/{pnLid}", capability: "session"},
	{method: "GET", path: "/api/{session}/profile/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/profile-pic/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/profile-status/{phone}", capability: "session"},
	{method: "GET", path: "/api/{session}/blocklist", capability: "session"},
	{method: "POST", path: "/api/{session}/block-contact", capability: "session"},
	{method: "POST", path: "/api/{session}/unblock-contact", capability: "session"},
	{method: "GET", path: "/api/{session}/get-battery-level", capability: "session"},
	{method: "GET", path: "/api/{session}/host-device", capability: "session"},
	{method: "GET", path: "/api/{session}/get-phone-number", capability: "session"},
	{method: "POST", path: "/api/{session}/set-profile-pic", capability: "session"},
	{method: "POST", path: "/api/{session}/profile-status", capability: "session"},
	{method: "POST", path: "/api/{session}/change-username", capability: "session"},
	{method: "POST", path: "/api/{session}/edit-business-profile", capability: "session"},
	{method: "GET", path: "/api/{session}/get-business-profiles-products", capability: "session"},
	{method: "GET", path: "/api/{session}/get-order-by-messageId/{messageId}", capability: "session"},
	{method: "GET", path: "/api/{secretkey}/backup-sessions", capability: "session"},
	{method: "POST", path: "/api/{secretkey}/restore-sessions", capability: "session"},
	{method: "GET", path: "/api/{session}/take-screenshot", capability: "session"},
	{method: "POST", path: "/api/{session}/set-limit", capability: "session"},
	{method: "POST", path: "/api/{session}/create-community", capability: "session"},
	{method: "POST", path: "/api/{session}/deactivate-community", capability: "session"},
	{method: "POST", path: "/api/{session}/add-community-subgroup", capability: "session"},
	{method: "POST", path: "/api/{session}/remove-community-subgroup", capability: "session"},
	{method: "POST", path: "/api/{session}/promote-community-participant", capability: "session"},
	{method: "POST", path: "/api/{session}/demote-community-participant", capability: "session"},
	{method: "GET", path: "/api/{session}/community-participants/{id}", capability: "session"},
	{method: "POST", path: "/api/{session}/newsletter", capability: "session"},
	{method: "PUT", path: "/api/{session}/newsletter/{id}", capability: "session"},
	{method: "DELETE", path: "/api/{session}/newsletter/{id}", capability: "session"},
	{method: "POST", path: "/api/{session}/mute-newsletter/{id}", capability: "session"},
	{method: "POST", path: "/api/{session}/chatwoot", capability: "session"},
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
		target.Method(route.method, route.path, s.notSupported(route.capability))
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
