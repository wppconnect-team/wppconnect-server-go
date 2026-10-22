package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

type schema = map[string]any

var specOnce sync.Once
var specBytes []byte

// OpenAPISpec describes the registered HTTP surface, including explicit 501s.
// The export command and HTTP endpoint use the same contract.
func OpenAPISpec() []byte {
	specOnce.Do(func() { specBytes = buildOpenAPI() })
	return specBytes
}

func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(OpenAPISpec())
}

func ref(name string) schema   { return schema{"$ref": "#/components/schemas/" + name} }
func str() schema              { return schema{"type": "string"} }
func boolean() schema          { return schema{"type": "boolean"} }
func integer() schema          { return schema{"type": "integer", "format": "int64"} }
func array(item schema) schema { return schema{"type": "array", "items": item} }
func object(props schema, required ...string) schema {
	s := schema{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func envelope(response schema) schema {
	return object(schema{"status": str(), "response": response, "mapper": str()}, "status", "response")
}
func stringList(limit int) schema {
	return schema{"oneOf": []schema{schema{"type": "string", "minLength": 1}, schema{"type": "array", "minItems": 1, "maxItems": limit, "items": str()}}}
}
func contentResponse(description string, value schema) schema {
	return schema{"description": description, "content": schema{"application/json": schema{"schema": value}}}
}

func contractSchemas() schema {
	id := object(schema{"user": str(), "server": str(), "_serialized": str()}, "user", "server", "_serialized")
	contact := object(schema{"id": ref("WID"), "name": str(), "shortName": str(), "pushname": str(), "verifiedName": str(), "isBusiness": boolean(), "isMyContact": boolean(), "isGroup": boolean(), "isUser": boolean()}, "id", "name")
	chat := object(schema{"id": ref("WID"), "name": str(), "formattedTitle": str(), "t": integer(), "unreadCount": integer(), "unreadCountIsPartial": boolean(), "archive": boolean(), "isArchived": boolean(), "pin": boolean(), "isGroup": boolean(), "isUser": boolean()}, "id", "name", "t", "unreadCount", "unreadCountIsPartial")
	phone := stringList(100)
	presence := object(schema{"phone": phone, "isGroup": boolean(), "isLid": boolean(), "value": boolean(), "all": boolean(), "duration": schema{"type": "integer", "minimum": 1, "maximum": 10000, "description": "Milliseconds; waits then pauses. Batch timeout is 30 seconds."}, "chatstate": schema{"oneOf": []schema{schema{"type": "integer", "enum": []int{0, 1, 2}}, schema{"type": "string", "enum": []string{"0", "1", "2"}}}}})
	media := object(schema{"phone": phone, "isGroup": boolean(), "base64": str(), "base64Ptt": str(), "mimetype": str(), "filename": str(), "caption": str(), "message": str()}, "phone")
	media["description"] = "Decoded media limit 16 MiB. base64 or base64Ptt is required for JSON. Voice requires Ogg/Opus. Upload paths and URLs are unsupported."
	return schema{
		"WID": id, "Contact": contact, "Chat": chat,
		"Error":        object(schema{"status": str(), "message": str(), "error": str(), "response": schema{}, "completed": schema{}, "capability": str(), "provider": str()}),
		"Success":      object(schema{"status": str(), "session": str()}, "status"),
		"Session":      object(schema{"status": str(), "session": str(), "qrcode": str(), "urlcode": str()}, "status", "session"),
		"Token":        object(schema{"status": str(), "session": str(), "token": str(), "full": str()}, "status", "session", "token", "full"),
		"MediaRequest": media, "PresenceRequest": presence,
		"MediaSent":     envelope(array(object(schema{"id": str(), "to": str(), "mimetype": str(), "filename": str()}, "id", "to", "mimetype", "filename"))),
		"MediaDownload": object(schema{"base64": str(), "mimetype": str()}, "base64", "mimetype"),
		"Group":         object(schema{"id": str(), "name": str(), "subject": str(), "participants": array(schema{})}),
		"GroupSummary":  object(schema{"id": str(), "name": str(), "participants": integer()}, "id", "name", "participants"),
		"Presence":      object(schema{"status": str(), "response": schema{"nullable": true, "oneOf": []schema{boolean(), integer()}}, "known": boolean(), "observedAt": schema{"type": "string", "format": "date-time", "nullable": true}}, "status", "response", "known", "observedAt"),
		"ChatFilter":    object(schema{"id": str(), "count": schema{"type": "integer", "minimum": 1, "maximum": 1000, "default": 20}, "direction": schema{"type": "string", "enum": []string{"before", "after"}, "default": "after"}, "onlyGroups": boolean(), "onlyUsers": boolean(), "onlyWithUnreadMessage": boolean(), "onlyArchived": boolean(), "withLabels": array(str()), "onlyCommunities": boolean()}),
	}
}

type routeContract struct {
	tag, description                     string
	success                              int
	response, body                       schema
	optionalBody, multipart, unsupported bool
}

func contractFor(name string) routeContract {
	c := routeContract{tag: "sessions", success: 200, response: ref("Success")}
	switch name {
	case "healthz", "readyz":
		c.tag = "operation"
		c.description = "Liveness, or readiness after restoration. Independent of individual WhatsApp connectivity."
		c.response = nil
	case "openapi.json":
		c.tag = "operation"
		c.description = "OpenAPI 3.0.3 contract for this checkout."
		c.response = schema{"type": "object"}
	case "dashboard/stats":
		c.tag = "operation"
		c.response = object(schema{"updatedAt": str(), "overview": schema{"type": "object"}, "sessions": array(schema{"type": "object"})}, "updatedAt", "overview", "sessions")
	case "generate-token":
		c.success = 201
		c.response = ref("Token")
		c.description = "Bootstrap with SECRET_KEY in the path. Do not log the URL. Returned bcrypt token is bound to session + secret."
	case "start-session":
		c.response = ref("Session")
		c.description = "Create/connect a session. Name must match [A-Za-z0-9_-]{1,64}. QR stream belongs to session lifetime, not HTTP request."
	case "status-session":
		c.response = ref("Session")
	case "qrcode-session":
		c.description = "PNG QR, or JSON QRCODE_NOT_AVAILABLE while no valid QR exists."
		c.response = schema{"type": "object"}
	case "close-session":
		c.description = "Disconnect, close resources, preserve credentials and disable restart restoration until start-session."
	case "logout-session":
		c.description = "Revoke linked device and remove credentials. Requires a connected session; failure preserves the handle. New pairing is required after success."
	case "send-message":
		c.tag = "messages"
		c.success = 201
		c.body = object(schema{"phone": stringList(100), "message": str(), "isGroup": boolean()}, "phone", "message")
	case "send-seen":
		c.tag = "messages"
		c.body = object(schema{"phone": str()}, "phone")
	case "check-number-status":
		c.tag = "contacts"
		c.response = envelope(object(schema{"numberExists": boolean(), "id": str()}, "numberExists", "id"))
	case "send-image", "send-file", "send-file-base64", "send-voice", "send-voice-base64", "send-audio-base64":
		c.tag = "media"
		c.success = 201
		c.body = ref("MediaRequest")
		c.response = ref("MediaSent")
		c.multipart = true
		c.description = "Bounded media upload; at most four concurrent media operations per process."
	case "download-media", "get-media-by-message":
		c.tag = "media"
		c.response = ref("MediaDownload")
		c.description = "Session descriptor cache only; missing/expired media is 404. Does not accept client URLs or decryption keys."
		if name == "download-media" {
			c.body = object(schema{"messageId": schema{"oneOf": []schema{str(), schema{"type": "object"}}}}, "messageId")
		}
	case "all-contacts":
		c.tag = "contacts"
		c.response = envelope(array(ref("Contact")))
	case "contact":
		c.tag = "contacts"
		c.response = envelope(ref("Contact"))
		c.description = "Synchronized store only; unknown contact is 404. PN/LID aliases are resolved locally."
	case "contact/pn-lid":
		c.tag = "contacts"
		c.response = object(schema{"phoneNumber": ref("WID"), "lid": ref("WID"), "contact": ref("Contact")})
		c.description = "Returns available local PN/LID association fields, without a remote lookup."
	case "profile-status":
		c.tag = "contacts"
		c.response = envelope(object(schema{"id": str(), "status": str()}, "id", "status"))
	case "profile-pic":
		c.tag = "contacts"
		c.response = envelope(object(schema{"id": str(), "imgFull": str(), "img": str(), "url": str()}, "id", "url"))
	case "all-chats":
		c.tag = "chats"
		c.response = envelope(array(ref("Chat")))
		c.description = "Observed SQLite metadata, not a full browser message database."
	case "all-chats-archived":
		c.tag = "chats"
		c.success = 201
		c.response = array(ref("Chat"))
	case "list-chats":
		c.tag = "chats"
		c.response = array(ref("Chat"))
		c.body = ref("ChatFilter")
		c.optionalBody = true
		c.description = "after includes anchor; before excludes it. No anchor + before returns []. Labels/communities filters return 501."
	case "chat-by-id":
		c.tag = "chats"
		c.response = ref("Chat")
	case "chat-is-online", "last-seen":
		c.tag = "presence"
		c.response = ref("Presence")
		c.description = "Presence observation TTL 5 minutes. Unknown/hidden values are null, known=false. last-seen uses Unix seconds."
	case "typing", "recording", "chat-state", "subscribe-presence", "set-online-presence":
		c.tag = "presence"
		c.body = ref("PresenceRequest")
		c.response = envelope(schema{"type": "object"})
		c.description = "Individual global presence and chat signals. value/isOnline defaults true. chatstate 0=typing,1=recording,2=paused. Up to 100 destinations."
		if name == "set-online-presence" {
			c.body = object(schema{"isOnline": schema{"type": "boolean", "default": true}})
		}
	case "all-chats-with-messages", "all-messages-in-chat", "all-new-messages", "unread-messages", "all-unread-messages", "message-by-id", "load-messages-in-chat", "get-messages":
		c.tag = "unsupported"
		c.unsupported = true
		c.description = "Always 501 after authentication and session connection checks. Browser message database is not implemented."
	case "all-groups":
		c.tag = "groups"
		c.response = envelope(array(ref("GroupSummary")))
	case "common-groups":
		c.tag = "groups"
		c.response = array(ref("GroupSummary"))
	case "group-info", "group-info-from-invite-link":
		c.tag = "groups"
		c.response = envelope(ref("Group"))
		if name == "group-info-from-invite-link" {
			c.body = object(schema{"invitecode": str()}, "invitecode")
		}
	case "group-members", "group-members-ids", "group-admins":
		c.tag = "groups"
		c.response = envelope(array(schema{}))
		c.description = "Available protocol fields; group-admins returns one nested array per group."
	case "group-invite-link", "group-revoke-link":
		c.tag = "groups"
		c.response = envelope(str())
		c.description = "group-revoke-link mutates the invitation despite using GET for Node compatibility."
	case "create-group":
		c.tag = "groups"
		c.success = 201
		c.response = envelope(schema{"type": "object"})
		c.body = object(schema{"name": stringList(10), "participants": stringList(100)}, "name", "participants")
	case "add-participant-group", "remove-participant-group", "promote-participant-group", "demote-participant-group":
		c.tag = "groups"
		c.success = 201
		if name == "remove-participant-group" {
			c.success = 200
		}
		c.response = envelope(schema{"type": "object"})
		c.body = object(schema{"groupId": stringList(20), "phone": stringList(100)}, "groupId", "phone")
		c.description = "207 means partial completion; inspect per-participant result before retrying."
	case "group-subject", "group-description", "group-property", "messages-admins-only", "change-privacy-group", "leave-group":
		c.tag = "groups"
		c.response = envelope(schema{})
		c.body = object(schema{"groupId": stringList(20), "title": str(), "description": str(), "property": schema{"type": "string", "enum": []string{"restrict", "announcement", "ephemeral"}}, "value": schema{}, "status": schema{}}, "groupId")
	case "join-code":
		c.tag = "groups"
		c.success = 201
		c.response = envelope(schema{"type": "object"})
		c.body = object(schema{"inviteCode": str()}, "inviteCode")
		c.description = "Membership may still require administrator approval."
	case "group-pic":
		c.tag = "groups"
		c.body = object(schema{"groupId": stringList(20), "base64": str(), "mimetype": str(), "filename": str()}, "groupId", "base64")
		c.multipart = true
		c.response = envelope(schema{})
	default:
		panic("missing OpenAPI contract for " + name)
	}
	return c
}

func buildOpenAPI() []byte {
	paths := schema{}
	pattern := regexp.MustCompile(`\{([^}]+)\}`)
	router := (&Server{}).router().(chi.Routes)
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		name := strings.TrimPrefix(path, "/api/{session}/")
		if strings.HasSuffix(path, "/generate-token") {
			name = "generate-token"
		} else if strings.HasPrefix(path, "/api/dashboard/") {
			name = "dashboard/stats"
		} else if strings.HasPrefix(name, "contact/pn-lid/") {
			name = "contact/pn-lid"
		} else {
			name = strings.Split(strings.TrimPrefix(name, "/"), "/")[0]
		}
		c := contractFor(name)
		responses := schema{}
		for _, code := range []string{"400", "401", "403", "404", "408", "409", "410", "413", "415", "422", "429", "500", "501", "502", "503", "504"} {
			responses[code] = contentResponse("Operation error; inspect status/message and any completed work.", ref("Error"))
		}
		if !c.unsupported {
			code := "200"
			if c.success == 201 {
				code = "201"
			}
			if c.response == nil {
				responses[code] = schema{"description": "Healthy"}
			} else {
				responses[code] = contentResponse("Success with the documented available protocol fields.", c.response)
			}
			if strings.Contains(name, "participant-group") {
				responses["207"] = contentResponse("Partial participant operation", envelope(schema{"type": "object"}))
			}
			if name == "qrcode-session" {
				responses["200"].(schema)["content"].(schema)["image/png"] = schema{"schema": schema{"type": "string", "format": "binary"}}
			}
		}
		operation := schema{"operationId": strings.ToLower(method) + "_" + strings.NewReplacer("/", "_", "-", "_", ".", "_").Replace(name), "summary": name, "description": c.description, "tags": []string{c.tag}, "responses": responses, "x-implemented": !c.unsupported}
		params := make([]schema, 0)
		for _, match := range pattern.FindAllStringSubmatch(path, -1) {
			param := str()
			if match[1] == "session" {
				param["description"] = "Session name; legacy session:token URL form is accepted but exposes credentials in URLs."
			}
			params = append(params, schema{"name": match[1], "in": "path", "required": true, "schema": param})
		}
		if c.tag == "contacts" || c.tag == "chats" || c.tag == "presence" {
			for _, key := range []string{"isGroup", "isLid", "isNewsletter"} {
				params = append(params, schema{"name": key, "in": "query", "schema": boolean(), "description": "Unsupported or conflicting chat type flags are rejected."})
			}
		}
		if len(params) > 0 {
			operation["parameters"] = params
		}
		if !strings.Contains(path, "{session}") || name == "generate-token" {
			operation["security"] = []any{}
		}
		if c.body != nil {
			content := schema{"application/json": schema{"schema": c.body}}
			if c.multipart {
				required := []string{"phone", "file"}
				if name == "group-pic" {
					required = []string{"groupId", "file"}
				}
				content["multipart/form-data"] = schema{"schema": object(schema{"phone": str(), "groupId": str(), "isGroup": str(), "caption": str(), "filename": str(), "file": schema{"type": "string", "format": "binary"}}, required...)}
			}
			operation["requestBody"] = schema{"required": !c.optionalBody, "content": content}
		}
		if _, ok := paths[path]; !ok {
			paths[path] = schema{}
		}
		paths[path].(schema)[strings.ToLower(method)] = operation
		return nil
	})
	if err != nil {
		panic(err)
	}
	doc := schema{"openapi": "3.0.3", "info": schema{"title": "WPPConnect Server Go", "version": "checkout", "description": "Implemented HTTP contract and explicit unsupported operations. Does not claim full Node parity. Use Bearer authentication per session; SECRET_KEY is only for token bootstrap. See docs for data/privacy limits."}, "servers": []schema{{"url": "/"}}, "security": []schema{{"bearerAuth": []string{}}}, "paths": paths, "components": schema{"securitySchemes": schema{"bearerAuth": schema{"type": "http", "scheme": "bearer", "bearerFormat": "session-bound bcrypt token"}}, "schemas": contractSchemas()}}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
