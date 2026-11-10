# Authenticate webhook deliveries

This feature requires a server build containing webhook signing support. Older
published image tags do not gain it when a new environment variable is set.

Set `WEBHOOK_URL` and an independent, random `WEBHOOK_SECRET` of at least 32 bytes
(for example, generate one with `openssl rand -hex 32`). Configure the same key
on the receiver. Keep it separate from `SECRET_KEY` and `ADMIN_TOKEN`.
Without `WEBHOOK_SECRET`, deliveries remain unsigned for compatibility.

Each HTTP attempt includes:

| Header | Value |
| --- | --- |
| `X-WPP-Event-ID` | Stable event ID across retries and replay |
| `X-WPP-Attempt` | Delivery attempt number |
| `X-WPP-Timestamp` | Unix seconds at the time of this delivery attempt |
| `X-WPP-Signature` | `v1=` followed by a lowercase hex HMAC-SHA256 |

The signed bytes are the UTF-8 encoding of
`timestamp + "." + eventID + "." + attempt + "."`, followed by the **exact raw
request body**. Do not parse and reserialize JSON before verifying. The key is
used as its literal UTF-8 bytes, including when it was generated as hex text.
The server signs again on every retry/replay using the current configured key.
No signing key is saved in event payloads or the outbox.

The receiver must check the timestamp against its own clock (for example, reject
an absolute difference greater than 300 seconds), compare the expected signature
in constant time, and atomically deduplicate by event ID in durable storage.
Timestamp validation limits replay age; it does not replace deduplication.
Acknowledge with 2xx only after accepting the event durably, including duplicates
that were already accepted. Use HTTPS outside a trusted local network.

Example Node.js verification, called with raw bytes before JSON parsing:

```js
import { createHmac, timingSafeEqual } from "node:crypto"

export function verifyWebhook(rawBody, headers, secret, now = Date.now()) {
  const timestamp = headers.get("x-wpp-timestamp") || ""
  const eventID = headers.get("x-wpp-event-id") || ""
  const attempt = headers.get("x-wpp-attempt") || ""
  const signature = headers.get("x-wpp-signature") || ""
  if (!/^\d+$/.test(timestamp) || !/^\d+$/.test(attempt) || !eventID ||
      !/^v1=[a-f0-9]{64}$/.test(signature)) return false
  const seconds = Number(timestamp)
  if (!Number.isSafeInteger(seconds) || Math.abs(now / 1000 - seconds) > 300) return false
  const expected = createHmac("sha256", secret)
    .update(`${timestamp}.${eventID}.${attempt}.`)
    .update(rawBody)
    .digest()
  return timingSafeEqual(expected, Buffer.from(signature.slice(3), "hex"))
}
```

HTTP redirects remain disabled. The signing configuration applies to the global
`WEBHOOK_URL` target. It does not add per-session webhook configuration or make
the `start-session` webhook parameter effective. Rotate keys by allowing the
previous and new key briefly on the receiver, restarting the server with the new
key, then removing the old receiver key after the freshness window has elapsed.


## Tenant-supplied receivers

Set WEBHOOK_PUBLIC_ONLY=true for untrusted destinations. This mode requires a
signing secret and an HTTPS receiver on port 443, rejects URL credentials and
fragments, disables environment proxies and redirects, and validates DNS answers
on every new connection. Private, loopback, link-local, multicast, documentation
and reserved addresses are rejected before dialing; mixed public/private DNS
answers fail closed. Connections dial the validated IP directly while TLS still
verifies the original hostname. Keep network-level egress controls as well.

Managed Cloud provisions a separate container for each configured receiver.
A receiver is configured before starting the WhatsApp session and remains in
Docker configuration across restarts. It is not a dynamic receiver shared among
tenants. Changing an active receiver requires an explicit controlled rollout;
this feature does not silently redirect an existing event backlog.
