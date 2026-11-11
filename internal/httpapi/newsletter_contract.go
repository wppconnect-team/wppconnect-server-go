package httpapi

func newsletterContract(name string) (routeContract, bool) {
	c := routeContract{tag: "channels", success: 200, response: envelope(schema{})}
	switch name {
	case "newsletter-info-invite", "follow-newsletter-invite", "unfollow-newsletter-invite":
		c.body = requestSchema(newsletterInviteRequest{}, "invite")
		c.description = "Resolve an 8..128 character channel invite code or canonical https://whatsapp.com/channel/CODE link through WhatsApp. Follow/unfollow uses only the returned newsletter identity. No arbitrary URLs are fetched. Returned metadata is the lookup snapshot, not proof of a refreshed role."
	case deleteNewsletter:
		c.body = requestSchema(newsletterDeleteRequest{}, "id")
		c.response = envelope(object(schema{"id": str(), "state": str()}, "id", "state"))
		c.description = "Permanently delete an owned channel using a compatible Web session. Requires Idempotency-Key. Confirms the same channel ID and DELETED state. Consult newsletter-admin-result after a lost response."
	case "newsletter-reaction":
		c.body = requestSchema(newsletterReactionRequest{}, "id", "serverId", "reaction")
		c.body["properties"].(schema)["reaction"] = str()
		c.body["properties"].(schema)["serverId"] = schema{"type": "integer", "minimum": 1, "maximum": 2147483647}
		c.response = envelope(object(schema{"id": str(), "serverId": integer(), "acknowledged": boolean()}, "id", "serverId", "acknowledged"))
		c.success = 202
		c.description = "Send a reaction to a numeric newsletter JID and serverId 1..2147483647. reaction is required (maximum 64 UTF-8 bytes); an empty string removes it. HTTP 202 means written to the protocol transport, not acknowledged or delivered. The generated reaction id is returned."
	case "newsletter-mark-viewed":
		c.body = requestSchema(newsletterViewRequest{}, "id", "serverIds")
		c.body["properties"].(schema)["serverIds"] = schema{"type": "array", "minItems": 1, "maxItems": 100, "uniqueItems": true, "items": schema{"type": "integer", "minimum": 1, "maximum": 2147483647}}
		c.response = envelope(object(schema{"acknowledged": boolean(), "count": integer()}, "acknowledged", "count"))
		c.description = "Mark 1..100 unique serverIds in 1..2147483647 as viewed in a newsletter, waiting at most 30 seconds for the protocol receipt acknowledgement. This can increment view counters and is distinct from synchronizing read state to other devices. Use Idempotency-Key to avoid repeating an intended operation."
	case "newsletter-message-updates":
		c.body = requestSchema(newsletterUpdatesRequest{}, "id")
		c.body["properties"].(schema)["count"] = schema{"type": "integer", "minimum": 1, "maximum": 100, "default": 50, "nullable": true}
		c.response = envelope(array(object(schema{"id": str(), "serverId": integer(), "timestamp": integer(), "views": integer(), "forwards": integer(), "reactions": schema{"type": "object", "additionalProperties": integer()}, "reactionsTruncated": boolean()}, "id", "serverId", "views", "forwards", "reactions", "reactionsTruncated")))
		c.description = "Read bounded reaction/view/forward counts without raw protocol or media keys. count 1..100, default 50; since is Unix seconds in 0..253402300799, after is a server ID in 0..2147483647. Zero omits that filter. The SDK has no before filter; unknown fields are rejected. No complete archive or gap-free cursor guarantee."
	case "newsletter-accept-tos":
		c.body = requestSchema(newsletterTOSRequest{}, "noticeId", "stage", "accepted")
		c.body["properties"].(schema)["accepted"] = schema{"type": "boolean", "enum": []bool{true}}
		c.description = "Accept a caller-selected newsletter terms notice. noticeId and stage are 1..32 digits; accepted=true must be explicit. Never called automatically by channel creation or other operations."
	case "newsletter-admin-invite":
		c.body = requestSchema(newsletterAdminInviteRequest{}, "id", "phone")
		c.success = 201
		c.description = "Send a newsletter administrator invitation message to one phone after verifying the connected account is a newsletter admin/owner. Name comes from upstream metadata. Optional thumbnail is base64 JPEG, at most 64 KiB/512x512, never a fetched URL. Caption up to 2048 characters; expiresInSeconds 60..604800, default 86400. HTTP success means protocol send accepted, not recipient delivery or granting an admin role."
	default:
		return routeContract{}, false
	}
	return c, true
}
