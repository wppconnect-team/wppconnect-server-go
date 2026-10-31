# Advanced native messages

This stage is implemented in the source checkout. The documented `0.1.3`
Docker baseline does not include these additions. Build this checkout or use
a subsequent release that explicitly includes them. No live WhatsApp account
was paired for validation; HTTP tests simulate the transport and download tests
exercise the pinned SDK's real decryption and integrity checks.

## Routes and compatibility

All routes below are `POST /api/{session}/...`, require a connected session and
its `Authorization: Bearer TOKEN`. Use `phone` as a string or an array of up to
100 recipients. Individual numbers use digits with country/area code, optionally
`@c.us` or `@s.whatsapp.net`. For groups, use `isGroup: true` and the group ID,
optionally `@g.us`. LID, status and newsletter destinations are unsupported.

| Route | Input | Success |
| --- | --- | --- |
| `send-video`, `send-video-base64` | Base64/data URL or multipart `file`, optional `caption`/`message` | `201` |
| `send-sticker` | Prepared static or animated WebP via base64/data URL or multipart `file` | `201` |
| `send-sticker-gif` | Prepared animated WebP using the same inputs | `201` |
| `contact-vcard` | `contactsId` array and optional `name`, or explicit `contacts` | `200` |
| `send-contact` | Same contact payload | `201` |
| `send-location` | Required `lat`/`lng`, optional `title`/`address` | `201` |
| `send-poll-message` | Required `name`/`choices`, optional `options.selectableCount` | `201` |

Location, poll and `contact-vcard` field names follow WPPConnect Node. Video
routes, `send-contact` and explicit `contacts` are Go extensions. Contact
responses always contain an array of accepted IDs; Node's `contact-vcard`
returns only its last send result. Contact names default to the supplied number,
without querying remote profiles or the address book.

Sticker route names follow Node, but the Go API accepts **already prepared
WebP only**. It does not convert PNG, JPEG or GIF. In particular, the
`send-sticker-gif` name does not mean GIF bytes are accepted. Use animated WebP.
For JSON media, `path` may contain a base64 data URL; filesystem paths and remote
URLs are rejected. No URL fetching, transcoding or FFmpeg runtime is required.

## Limits and validation

| Type | Limits |
| --- | --- |
| Video | MP4 container, one H.264 `avc1` video track and optional `mp4a` audio track; 16 MiB decoded, dimensions 1..4096, positive duration up to one hour. Duration is rounded up to seconds. |
| Sticker | WebP with 512x512 canvas. Static up to 100 KiB; animated up to 500 KiB, 120 frames, total 10 seconds, each frame at least 10 ms. Captions are rejected. |
| Contacts | 1..20 cards. Card numbers have 7..15 digits with optional `+`, `@c.us` or `@s.whatsapp.net`. Names up to 256 Unicode characters. Use either `contactsId` or `contacts`. |
| Location | Finite latitude -90..90, longitude -180..180. Numeric strings are accepted. Zero is valid; missing/null values are rejected. Title up to 256 characters, address up to 1024. Static location only. |
| Poll | Name 1..255 characters; 2..12 nonempty choices, each up to 100 characters. Choices must differ after trimming and case folding. `selectableCount` defaults to 1; 0 permits all choices, otherwise 1..choice count. |

Container inspection checks structure, dimensions, declared codecs and duration.
It does not decode compressed video/audio/pixels or guarantee playback on every
client. Prepare valid, interoperable MP4 H.264/AAC and WebP before uploading.
WebP structure follows the [official RIFF specification](https://developers.google.com/speed/webp/docs/riff_container).
These are explicit server limits; successful protocol submission still depends
on WhatsApp and the recipient's client.

Structured contact/location/poll bodies are limited to 64 KiB, accept JSON only
and reject unknown fields. Video/sticker reuse the existing bounded media reader
(16 MiB decoded; base64 body allowance plus 64 KiB). Sending and downloads share
four operation slots per process. Each upload/send batch has a 60 second timeout
after validation and slot acquisition. Client cancellation is propagated.

## Complete examples

These Bash examples assume an already connected session. Obtain the session
token using the [authentication quick start](../documentation/readme-reference.md#connect-your-first-session).
Do not use the server's `SECRET_KEY` as a Bearer token.

```bash
export BASE_URL=http://localhost:21465
export SESSION=mysession
export TOKEN='YOUR_SESSION_TOKEN'
export PHONE=5511999999999
```

Send a prepared MP4 as native video with a caption:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-video" \
  -H "Authorization: Bearer $TOKEN" \
  -F "phone=$PHONE" -F 'caption=Product demonstration' \
  -F 'file=@video.mp4;type=video/mp4'
```

The base64 alias supports the same caption and recipient fields:

```bash
base64 < video.mp4 | tr -d '\r\n' > video.b64
jq -n --arg phone "$PHONE" --rawfile media video.b64 \
  '{phone:$phone,base64:$media,caption:"Product demonstration"}' > video.json
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-video-base64" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data-binary @video.json
```

Send static and animated stickers, both prepared as 512x512 WebP:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-sticker" \
  -H "Authorization: Bearer $TOKEN" -F "phone=$PHONE" \
  -F 'file=@sticker.webp;type=image/webp'

curl -fsS -X POST "$BASE_URL/api/$SESSION/send-sticker-gif" \
  -H "Authorization: Bearer $TOKEN" -F "phone=$PHONE" \
  -F 'file=@animated.webp;type=image/webp'
```

Share a contact using Node field names:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/contact-vcard" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","contactsId":["5521999999999"],"name":"Support"}'
```

Share multiple individually named contacts using the Go extension:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-contact" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","contacts":[{"name":"Ana","phone":"5521999999999"},{"name":"João","phone":"5531999999999"}]}'
```

One card becomes `ContactMessage`; multiple cards become `ContactsArrayMessage`.
The server generates escaped and UTF-8-safe folded vCard 3.0 content. Arbitrary
raw vCards, images, business profiles and remote contact lookups are unsupported.

Send a static location using numeric strings, as used in Node integrations:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-location" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","lat":"-23.5505","lng":"-46.6333","title":"Office","address":"São Paulo, Brazil"}'
```

Send a single-choice poll to a group:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-poll-message" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"123456789012345678@g.us","isGroup":true,"name":"When should we meet?","choices":["09:00","14:00","16:00"],"options":{"selectableCount":1}}'
```

Use `selectableCount: 0` to allow all choices, or a positive count to bound the
selection. Poll secrets are generated by the pinned SDK separately for each
recipient and never appear in API results or webhooks. This stage creates polls
and normalizes incoming poll metadata. Vote decryption, voting, result totals,
message persistence and poll-result queries remain separate roadmap work.

## Results and partial failures

Structured sends return this shape (`200` for `contact-vcard`, `201` otherwise):

```json
{
  "status": "success",
  "response": [{"id": "ACCEPTED_ID", "to": "5511999999999@s.whatsapp.net", "type": "location"}],
  "mapper": "return"
}
```

Types in structured results are `vcard`, `location`, or `poll_creation`.
Video/sticker use the existing media result fields: `id`, `to`, `mimetype`,
`filename`. An accepted ID means submission succeeded, not a delivery receipt.

Batches stop at the first send failure. Responses include `completed` accepted
results and `failedRecipient`, with HTTP `502`, `504` (timeout) or `408`
(cancellation). Later recipients are not attempted. The failed recipient's
delivery may be unknown, so retrying the whole batch may duplicate messages.
The server does not automatically retry sends. Input errors return `400`, size
violations `413`, authentication failures `401`, disconnected sessions `404`.

```json
{
  "status": "error",
  "message": "message send failed; delivery to the failed recipient may be unknown",
  "completed": [{"id": "ACCEPTED_ID", "to": "5511999999999@s.whatsapp.net", "type": "poll_creation"}],
  "failedRecipient": "5521999999999@s.whatsapp.net"
}
```

## Incoming events and download

The existing `onmessage` webhook adds these native content shapes while retaining
`id`, `from`, `author`, `chatId`, `fromMe`, `isGroupMsg`, `body`, and Unix `t`:

| `type` | Additional fields |
| --- | --- |
| `video` | `isMedia: true`, `mimetype`, `size`, `caption`, `duration`, `width`, `height`; `body` is the caption. |
| `sticker` | `isMedia: true`, `mimetype`, `size`, `width`, `height`, `isAnimated`. |
| `vcard` | `displayName`, `body` containing bounded vCard text. |
| `multi_vcard` | `displayName`, `contacts` with `displayName`/`vcard`, `truncated`. |
| `location` | `lat`, `lng`, `title`, `address`. |
| `poll_creation` | `name`, `choices`, `selectableCount`, `truncated`. |
| `poll_update` | `pollId` only; no decrypted selection or totals. |

Incoming contact arrays are capped at 20 cards (8 KiB per vCard), poll choices at
12 (400 bytes each), and text metadata is bounded at UTF-8 boundaries. Standard
ephemeral/view-once wrappers and known poll creation variants are normalized.
Decryption keys, media URLs and encrypted votes are not exposed.

Video/sticker descriptors are registered before webhook dispatch. Download with
the same session token and message ID; encryption integrity is verified by the
SDK. The descriptor cache remains ephemeral: 1024 entries per session, 24 hour
TTL, lost on session close/restart. Missing or other-session IDs return `404`.

```bash
curl -fsS "$BASE_URL/api/$SESSION/get-media-by-message/RECEIVED_OR_SENT_ID" \
  -H "Authorization: Bearer $TOKEN" > media.json
jq -r .base64 media.json | base64 --decode > downloaded.bin
```

The response has `base64` and `mimetype`. Contacts, locations and polls are
structured content and have no downloadable attachment. For existing image,
document and audio behavior, see the [media guide](media.pt-BR.md).

## Automated evidence

`go test ./...` covers native protocol fields, MIME/container metadata, malformed
and oversized input, contacts/coordinates/poll validation, recipient/group
handling, per-recipient poll secrets, auth/session isolation, partial failures,
timeouts, event normalization and HTTP webhook delivery. HTTP flows over TCP are
validated against the exported [OpenAPI](openapi.json). Real SDK download tests
cover video/sticker encryption keys, corrupt payloads and network errors using
in-memory transport. Fuzz seeds include real synthetic MP4/WebP fixtures.
