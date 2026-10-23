package httpapi

import (
	"reflect"
	"strings"
)

// requestSchema derives field names/types from the strict JSON decoder's input.
// Semantic limits and effects remain explicit in each operation's description.
func requestSchema(value any, required ...string) schema {
	var convert func(reflect.Type) schema
	convert = func(t reflect.Type) schema {
		if t.Kind() == reflect.Pointer {
			s := convert(t.Elem())
			s["nullable"] = true
			return s
		}
		switch t.Kind() {
		case reflect.String:
			return str()
		case reflect.Bool:
			return boolean()
		case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint32, reflect.Uint64:
			return integer()
		case reflect.Float64:
			return schema{"type": "number"}
		case reflect.Slice:
			if t.Elem().Kind() == reflect.Uint8 {
				return schema{}
			}
			return array(convert(t.Elem()))
		case reflect.Struct:
			props := schema{}
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				name := strings.Split(f.Tag.Get("json"), ",")[0]
				if name == "" {
					name = f.Name
				}
				if name != "-" {
					props[name] = convert(f.Type)
				}
			}
			s := object(props)
			s["additionalProperties"] = false
			return s
		default:
			return schema{}
		}
	}
	s := convert(reflect.TypeOf(value))
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func textSchema() schema {
	s := requestSchema(textRequest{}, "phone", "message")
	s["properties"].(schema)["phone"] = stringList(100)
	return s
}
func historyContract() routeContract {
	return routeContract{tag: "history", success: 200, response: schema{"type": "object"}, description: "Session-owned, observed SQLite messages only, including available history sync. Not a full WhatsApp inbox. Default retention 30 days/100000 messages/256 MiB. limit 1..200 (default 50), cursor is a nonnegative ingestion sequence, newest first. isPartial=true. Keys and raw protocol are never exposed. History remains readable while disconnected."}
}
func extendedContract(name string) (routeContract, bool) {
	c := routeContract{tag: "messages", success: 201, response: ref("AdvancedSent")}
	switch name {
	case "send-status-text":
		c.body = object(schema{"message": str()}, "message")
		c.description = "Publish text status (1..1000 characters) to the account's WhatsApp status privacy audience."
	case "send-status-media":
		c.body = ref("MediaRequest")
		c.multipart = true
		c.description = "Publish image or MP4 status using the account's status privacy audience. Same bounded uploads as messaging; phone is unused."
	case "send-buttons", "send-list":
		c.body = requestSchema(interactiveRequest{}, "phone", "message", "choices")
		c.description = "Experimental legacy reply buttons/lists, disabled unless ENABLE_EXPERIMENTAL_INTERACTIVES=true. Delivery varies by WhatsApp client/account. Maximum 3 buttons or 10 list rows; no universal delivery guarantee."
	case "send-carousel":
		c.unsupported = true
		c.description = "Carousel delivery is unavailable in this SDK and returns 501."
	case "messages", "message-receipts", "poll-results":
		c = historyContract()
	case "send-reply":
		c.body = textSchema()
		c.body["required"] = []string{"phone", "message", "quotedMessageId"}
		c.description = "Text with persisted quote from each destination chat; quoted keys/secrets cannot be supplied. Up to 100 mentions, 65536 text characters."
	case "send-reaction", "edit-message", "delete-message", "mark-read", "mark-played":
		c.body = requestSchema(actionRequest{}, "phone")
		c.description = "Target must exist in this session and chat. Only own messages may be edited/revoked; editing is limited to text and the SDK edit window. Empty reaction removes it. mark-read/mark-played require 1..100 received messageIds and preserve partial results."
		if name == "mark-read" || name == "mark-played" {
			c.success = 200
			c.response = envelope(array(str()))
		} else {
			c.body["required"] = []string{"phone", "messageId"}
		}
	case "privacy-settings", "blocklist", "block-contact", "unblock-contact", "set-profile-name", "set-profile-status", "set-profile-picture":
		c.tag = "account"
		c.success = 200
		c.response = envelope(schema{})
		if name != "blocklist" {
			c.body = requestSchema(accountRequest{})
		}
		if name == "set-profile-picture" {
			c.body = ref("MediaRequest")
			c.multipart = true
		}
		c.description = "Own-account operations only. Profile name 1..25 characters, status 1..139; photo up to 2 MiB/16 million pixels normalized to square JPEG. Privacy values validated per setting; exception lists are not managed. All mutations require a connected session and 30 second timeout."
	case "archive-chat", "pin-chat", "mute-chat", "mark-chat-unread", "delete-chat", "edit-label", "label-chat", "label-message", "labels", "chat-labels":
		c.tag = "chats"
		c.success = 200
		c.response = envelope(schema{})
		if name != "labels" && name != "chat-labels" {
			c.body = requestSchema(chatMutationRequest{})
		}
		c.description = "SDK app-state mutation, local state updated only after acknowledgement. value is required except delete-chat/edit-label. Mute duration 0..31536000 seconds; 0 means forever. Label IDs 1..65535, name 1..100 characters, color 0..19; session limits 1000 labels/100000 associations. Message references must be in the same session/chat."
	case "events", "events/replay", "events/ws":
		c.tag = "events"
		c.success = 200
		c.response = schema{"type": "object"}
		c.description = "Durable session event log; cursor resumes ascending ingestion sequence, limit 1..200. Retention defaults to 7 days/100000 events/128 MiB. Target attempts are independent. Retried/replayed delivery preserves event ID; consumers deduplicate. Saturated logs reject new events with a rejection metric."
		if name == "events/replay" {
			c.success = 202
			c.body = requestSchema(replayRequest{}, "eventId")
		}
		if name == "events/ws" {
			c.success = 101
			c.response = nil
			c.description += " Authenticated same-origin WebSocket, replay then live polling. Bearer headers or administrator cookie; no credentials in query strings."
		}
	case "archive-media", "stored-media":
		c.tag = "storage"
		c.success = 200
		c.response = schema{"type": "object"}
		c.description = "Optional authenticated S3-compatible storage, quota 1000 objects/512 MiB per session, 16 MiB per object. Private object keys, integrity-checked downloads, configurable retention and automatic cleanup. Stored downloads require chatId query and work while disconnected. Upload failures reserve quota for cleanup within one hour."
		if name == "archive-media" {
			c.success = 201
			c.body = requestSchema(archiveRequest{}, "chatId", "messageId")
		}
	case "newsletters", "create-community", "community-info", "link-community-group", "unlink-community-group", "create-newsletter", "newsletter-info", "follow-newsletter", "unfollow-newsletter", "mute-newsletter", "subscribe-newsletter", "newsletter-messages", "send-newsletter-text", "reject-call", "vote-poll":
		c.tag = "channels"
		c.success = 200
		c.response = envelope(schema{})
		c.body = requestSchema(channelRequest{})
		c.description = "Connected-session SDK operations; parent membership/roles enforced by WhatsApp. Communities use group JIDs, newsletters numeric @newsletter JIDs. Newsletter count 1..100, subscribe returns expiration without automatic renewal. Poll choice names must match a retained poll in the same chat; [] withdraws. Limits and partial creation effects are documented in the roadmap guide."
		if strings.HasPrefix(name, "create-") || name == "send-newsletter-text" || name == "vote-poll" {
			c.success = 201
		}
	case "pair-phone":
		c.tag = "sessions"
		c.success = 200
		c.response = envelope(schema{})
		c.body = object(schema{"phone": schema{"type": "string", "pattern": "^[0-9]{7,15}$"}}, "phone")
		c.description = "Start an unpaired session and request a temporary phone linking code once QR transport is ready. Never uses a real account during automated tests."
	case "admin/login", "admin/auth", "admin/logout", "admin/sessions", "admin/session-settings", "admin/capabilities", "metrics":
		c.tag = "administration"
		c.success = 200
		c.response = schema{"type": "object"}
		c.description = "ADMIN_TOKEN authentication (at least 32 bytes), separate from session bcrypt tokens. Browser login issues an 8-hour HttpOnly SameSite=Strict cookie; cookie-authenticated writes require X-CSRF-Token. Credentials/proxy URLs are never returned. Missing ADMIN_TOKEN disables administration (403)."
		if name == "admin/login" {
			c.body = requestSchema(adminLoginRequest{}, "token")
		}
		if name == "admin/sessions" {
			c.body = requestSchema(adminCreateRequest{}, "name")
		}
		if name == "admin/session-settings" {
			c.body = requestSchema(settingsRequest{})
		}
	default:
		return routeContract{}, false
	}
	return c, true
}
