# Expanded API and operations guide

This guide describes the completed expansion in source. The published `v0.1.6`
baseline includes native advanced messages; these new capabilities require an
image built from this change or a later release containing it. No live WhatsApp
account was paired during validation. Protocol permissions and account/client
restrictions still apply. This is a capability expansion, not a claim of full
Evolution Go API or delivery parity.

## Build and deploy this source

```bash
docker build -t wppconnect-go:roadmap .
export SECRET_KEY="$(openssl rand -hex 24)"
export ADMIN_TOKEN="$(openssl rand -hex 32)"
docker run -d --name wppconnect-roadmap --restart unless-stopped \
  --stop-timeout 35 -p 127.0.0.1:21465:21465 \
  -e SECRET_KEY -e ADMIN_TOKEN -v wppconnect_roadmap_data:/data \
  wppconnect-go:roadmap
```

For Portainer, publish this locally built image to a registry accessible by your
Docker host, then replace the `image:` in the supplied Standalone or Swarm stack.
Add `ADMIN_TOKEN` and the optional adapter settings to its environment. Use one
replica per data volume. The SQLite stores and durable workers are intended for
one server process; do not share that volume among replicas. Swarm must retain
the node placement constraint. Existing release tags do not gain new features.

Open `/manager` for the English console. `ENABLE_MANAGER=false` disables its
assets. Empty `ADMIN_TOKEN` disables administrator access. The administrator token
must contain at least 32 bytes and is separate from session bcrypt tokens. Browser
login uses an 8-hour HttpOnly, SameSite=Strict cookie and an in-memory CSRF token.
Use HTTPS in production. Logout and server restart invalidate browser sessions.
Bearer administrator access also works for session routes and `/metrics`.
Dashboard stats now require administrator authentication.

Examples below use a previously generated session token:

```bash
export BASE=http://localhost:21465
export SESSION=mysession
# TOKEN is the session-bound token from the quick start.
api() { curl -fsS "$BASE/api/$SESSION/$1" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "$2"; }
```

## History, context and message actions

Messages observed after this upgrade, including available SDK history sync, are
stored per session. Existing browser history is not reconstructed. History sync
imports at most 10,000 messages per event within 30 seconds. Responses mark
`isPartial=true`. SQLite files retain protocol data privately for downloads and
poll decryption; HTTP responses never expose raw protobuf, media keys or poll
secrets. Backups contain sensitive credentials and message content.

```bash
curl -fsS "$BASE/api/$SESSION/messages?chatId=5511999999999@s.whatsapp.net&limit=50" \
  -H "Authorization: Bearer $TOKEN"
# Continue with &cursor=<nextCursor>; newest ingestion sequence comes first.
api send-message '{"phone":"5511999999999","message":"Hello @team","mentionedList":["5511888888888"]}'
api send-reply '{"phone":"5511999999999","message":"Following up","quotedMessageId":"MESSAGE_ID"}'
api send-reaction '{"phone":"5511999999999","messageId":"MESSAGE_ID","reaction":"👍"}'
api edit-message '{"phone":"5511999999999","messageId":"OWN_TEXT_ID","message":"Corrected text"}'
api delete-message '{"phone":"5511999999999","messageId":"OWN_MESSAGE_ID"}'
api mark-read '{"phone":"5511999999999","messageIds":["RECEIVED_ID"]}'
api mark-played '{"phone":"5511999999999","messageIds":["RECEIVED_AUDIO_ID"]}'
```

Quotes and action IDs must exist in the same session and chat. Editing supports
own text within the SDK's 20-minute window. Revocation supports own messages;
group administrator revocation of other authors is not exposed by this API.
An empty reaction removes it. Explicit receipts accept 1..100 received IDs.
Send success means protocol acknowledgement, not recipient delivery. A failed
multi-recipient operation reports completed work; do not resend it blindly.

`GET messages/{messageId}`, `message-receipts/{messageId}` and
`poll-results/{messageId}` accept `chatId`. Without it, ambiguous IDs are rejected.
Eight legacy browser-history paths now query this same observed archive, with
cursor envelopes rather than claiming full Node/browser response equivalence.
`DELETE messages?chatId=<JID>` clears retained local messages for that chat;
`DELETE messages?all=true` clears this session's message archive. These do not
delete remote messages, labels, delivery records or separately archived S3 files.

Defaults are 30 days, 100,000 messages and 256 MiB of serialized message payloads
per session. Limits trigger oldest-message pruning; SQLite indexes, labels,
receipts and WAL overhead are additional. Pages contain at most 200 records and
approximately 4 MiB of JSON. Per-message protocol/normalized content is bounded.
Retention is enforced on access/insertion and during the storage cleanup sweep.

## Account, chats and labels

```bash
api set-profile-name '{"name":"Support"}'
api set-profile-status '{"status":"Available for support"}'
api privacy-settings '{"name":"online","value":"match_last_seen"}'
api block-contact '{"phone":"5511999999999"}'
api unblock-contact '{"phone":"5511999999999"}'
api archive-chat '{"phone":"5511999999999","value":true}'
api pin-chat '{"phone":"5511999999999","value":true}'
api mute-chat '{"phone":"5511999999999","value":true,"duration":3600}'
api mark-chat-unread '{"phone":"5511999999999","value":true}'
api edit-label '{"labelId":"1","name":"Customer","color":2}'
api label-chat '{"phone":"5511999999999","labelId":"1","value":true}'
api label-message '{"phone":"5511999999999","messageId":"MESSAGE_ID","labelId":"1","value":true}'
api list-chats '{"withLabels":["1"],"count":20}'
```

`GET privacy-settings`, `blocklist`, `labels` and `chat-labels/{phone}` read current
data. Message labels use `messageId` on the last query. Profile pictures accept
base64 or multipart `file`, limited to 2 MiB/16 million pixels and normalized to
JPEG. Profile name/status limits are 25/139 characters. Privacy exception lists
are not managed. See OpenAPI for accepted policy/value pairs.

Chat changes use acknowledged SDK app-state patches; remote rejections leave the
local index untouched. Muting accepts up to 365 days; duration zero means forever.
`delete-chat` requires an observed last message, deletes the remote chat and clears
its local history after acknowledgement. Archived S3 objects keep their own TTL.
Label definitions and associations follow observed updates, not a complete
business account export. Limits include tombstones: 1,000 definitions and 100,000
associations per session. Numeric label IDs are 1..65535 and colors 0..19.
`withLabels` matches all requested labels, up to 20; `onlyCommunities` consults
joined parent groups and filters the observed chat index.

## Durable delivery and replay

The global `.events.db` persists event payloads and per-target delivery attempts
atomically before dispatch. Defaults: 7 days, 100,000 events, 128 MiB payloads,
1 MiB per event. Pending deliveries are never evicted to make room. Saturation
rejects new events and increments `wppconnect_rejected_events_total`; monitor it.
This is bounded at-least-once delivery after successful persistence, not exactly
once. Events may arrive out of order. Consumers must deduplicate stable IDs.

`WEBHOOK_URL` uses stable `X-WPP-Event-ID` and `X-WPP-Attempt` headers. Transport
errors, 408/425/429/5xx retry with bounded exponential backoff and Retry-After.
Other non-2xx responses fail permanently. Shutdown drains within its budget;
unacknowledged leases recover after restart. Target retries are independent.

Optional transports:

| Setting | Delivery semantics |
| --- | --- |
| `NATS_URL=nats://nats:4222` | Core NATS subject `wppconnect.events.<session>.<event>`, WPP-Event-ID header, broker flush acknowledgement. No JetStream/subscriber durability guarantee. |
| `AMQP_URL=amqp://user:password@rabbitmq:5672/` | Durable topic exchange and bound durable queue `wppconnect.events`, persistent messages, publisher confirmations and MessageId. Broker persistence still depends on its deployment. |

```bash
curl -fsS "$BASE/api/$SESSION/events?cursor=0&limit=50" -H "Authorization: Bearer $TOKEN"
api events/replay '{"eventId":"RETAINED_EVENT_UUID"}'
```

Replay returns 202 and resets attempts for configured targets while preserving
the ID. Wrong-session/expired IDs return 404; active leases return 409. Removing
a target leaves its existing pending records intact; re-enable it or migrate
the data deliberately. `WEBHOOK_QUEUE_SIZE` is retained for configuration
compatibility; durable capacity is now controlled by `OUTBOX_MAX_*`.

`GET events/ws?cursor=<sequence>` upgrades to a same-origin WebSocket, sends
retained events then polls for new ones. Use Bearer headers or the administrator
cookie; query tokens are not accepted. Up to 32 streams per process. On expired
cursors only retained records remain. Additional normalized families cover
receipts, labels, calls, newsletters, privacy, profile changes and blocklists.
Call audio/video transport and call initiation are not implemented.

## Private S3-compatible media

Configure a pre-existing **private** bucket and least-privilege credentials:

```dotenv
S3_ENDPOINT=minio:9000
S3_USE_SSL=false
S3_ACCESS_KEY_ID=replace-with-private-key
S3_SECRET_ACCESS_KEY=replace-with-private-secret
S3_BUCKET=wppconnect-private
S3_REGION=us-east-1
MEDIA_RETENTION_DAYS=7
```

TLS defaults to true. The server never creates a bucket or sets a public policy.
Use TLS outside an isolated test network. Configure bucket lifecycle as an
additional safeguard, not as a replacement for API ownership checks.

```bash
api archive-media '{"chatId":"5511999999999@s.whatsapp.net","messageId":"MEDIA_ID"}'
curl -fsS "$BASE/api/$SESSION/stored-media/MEDIA_ID?chatId=5511999999999@s.whatsapp.net" \
  -H "Authorization: Bearer $TOKEN"
```

Archival is explicit and requires a connected session for SDK download. Cached
S3 downloads work offline and verify plaintext SHA-256. Responses contain
base64, MIME and expiration, never object keys or public/presigned URLs. Delete
uses the same path with DELETE. Limits: 16 MiB/object, 1,000 objects and 512 MiB
per session. Successful retention is 1..30 days. Failed uploads retain a quota
reservation for up to one hour. The minute cleanup sweep removes expired objects
and retries failed deletion; metadata is forgotten only after S3 deletion succeeds.
Keep history DB metadata when migrating the bucket. Backup/restore all databases
and objects together; there is no automatic migration from external products.

## Pairing and settings

```bash
api pair-phone '{"phone":"5511999999999"}'
curl -fsS -X PUT "$BASE/api/admin/session-settings/$SESSION" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"autoRead":true,"alwaysOnline":false,"proxyUrl":"socks5://proxy:1080"}'
curl -fsS "$BASE/metrics" -H "Authorization: Bearer $ADMIN_TOKEN"
```

Phone pairing starts an unpaired session, waits for QR transport readiness and
returns a temporary code. Enter it in WhatsApp's linked-device phone-code flow.
Automatic reading and online presence are opt-in. Always-online publishes on
connection, not an infinite renewal scheduler. Proxies support http/https/socks5,
apply on session restart and are stored privately; GET returns only `hasProxy`.
Omitted settings remain unchanged. The console exposes create/start/close/logout,
QR/phone pairing, event replay, observed history, text/quotes and these settings.

## Evaluated SDK capabilities

| Area | HTTP scope and limitations |
| --- | --- |
| Communities | create-community, community-info, link-community-group, unlink-community-group. Parent group classification checked; WhatsApp enforces roles. |
| Newsletters | List subscribed channels; create/info/follow/unfollow/mute, subscribe to live updates, bounded messages, send text. Subscription response reports expiry; renewal is explicit. |
| Status | send-status-text (up to 1000 characters) and send-status-media (image/MP4). Audience follows WhatsApp status privacy, not arbitrary broadcast recipients. |
| Calls | Safe lifecycle metadata and reject-call. No call initiation or media transport. |
| Polls | vote-poll validates retained poll choices, explicit [] withdraws. Decrypted latest-per-voter results are partial, with no secret or voter identity exposure. |
| Buttons/lists | Experimental native legacy forms, opt-in ENABLE_EXPERIMENTAL_INTERACTIVES=true, up to 3 buttons/10 rows. No account/client delivery guarantee. |
| Carousels | Authenticated explicit 501; no supported delivery implementation in this pinned SDK integration. |
| Business catalog/templates | Outside this roadmap's implemented scope; no parity claim. |

```bash
api create-community '{"name":"Customer community","description":"Our updates"}'
api link-community-group '{"parentId":"COMMUNITY@g.us","groupId":"GROUP@g.us"}'
api create-newsletter '{"name":"Updates","description":"Product news"}'
api follow-newsletter '{"id":"123456789@newsletter"}'
api newsletter-messages '{"id":"123456789@newsletter","count":20,"before":0}'
api send-status-text '{"message":"Support is available today"}'
api reject-call '{"phone":"5511999999999","callId":"OBSERVED_CALL_ID"}'
api vote-poll '{"phone":"5511999999999","messageId":"POLL_ID","choices":["Yes"]}'
```

## Validation and configuration

`go test ./...`, `go vet ./...`, Linux race tests and multiarch Docker smoke tests
cover the release. CI also starts actual NATS, RabbitMQ and MinIO fixtures from
`deploy/integration-test.yml`, verifies broker reconnect/persistent delivery,
private object access and retention cleanup. These fixtures contain disposable
credentials and must never be deployed as production services. To reproduce:

```bash
docker compose -p wppgo-tests -f deploy/integration-test.yml up -d --wait
INTEGRATION_REQUIRED=1 INTEGRATION_NATS_URL=nats://127.0.0.1:14222 \
 INTEGRATION_AMQP_URL=amqp://integration:integration-only-password@127.0.0.1:15673/ \
 INTEGRATION_S3_ENDPOINT=127.0.0.1:19000 \
 go test ./internal/eventbus ./internal/vault -count=1
docker compose -p wppgo-tests -f deploy/integration-test.yml down -v
```

New environment controls are documented in `.env.example`. History retention
allows 1..365 days; outbox/media 1..30. Row limits allow 1..1,000,000 and payload
budgets 1 MiB..10 GiB. Existing WEBHOOK_WORKERS/ATTEMPTS/TIMEOUT now configure all
delivery targets. Changing a retention setting does not recover already pruned
data. Apache-2.0 licensing is in LICENSE; third-party notices ship in the image.
