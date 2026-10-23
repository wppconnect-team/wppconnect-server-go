# Evolution Go capability benchmark

Reviewed on October 23, 2026. This is a source comparison and a planning input,
not a claim that WPPConnect Server Go implements the reference API or has reached
feature parity. No live WhatsApp account was used for this review.

## Reference and method

The reference is [Evolution Go commit `9337afc`](https://github.com/evolution-foundation/evolution-go/tree/9337afc47e10b86cc896a6f432240e40fee95dd1),
whose [VERSION file](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/VERSION)
contains `0.7.2`. All reference links below pin that revision. Route registration,
service implementations, startup wiring, storage models and configuration were
inspected. A registered handler is evidence of source coverage, not evidence of
successful delivery on every WhatsApp account or client.

Our original comparison baseline is [WPPConnect Server Go commit `1fbd26d`](https://github.com/wppconnect-team/wppconnect-server-go/tree/1fbd26d66802ee631e0bfb543ae2d5d5c0c04199).
The capability baseline shipped in [release `v0.1.3`](https://github.com/wppconnect-team/wppconnect-server-go/releases/tag/v0.1.3);
the current-source revision also includes subsequent dependency updates.
That revision had 59 implemented operations and eight explicit unsupported
operations. The current source adds native advanced messaging and its exported
[OpenAPI contract](openapi.json) has 67 implemented operations, with the same
eight explicit unsupported operations. The advanced additions require a source
build or a subsequent release, and are not included in `0.1.3`. Our route
and payload contract targets WPPConnect Node compatibility, while Evolution uses
different route groups and instance API keys. These are different API contracts.
Compare capabilities before comparing endpoint counts.

## Verified capability comparison

| Area | Evolution Go source at the pinned revision | WPPConnect Server Go current source | Follow-up opportunity |
| --- | --- | --- | --- |
| Sessions | Admin instance CRUD, logs, proxy settings and forced reconnect; instance connect, QR, phone pairing, reconnect, logout and advanced settings. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L80-L109) | QR linking, status, disconnect versus logout, persisted restoration and graceful shutdown. [Operation guide](operation.pt-BR.md) | Phone pairing, authenticated administration and per-session proxy/settings. |
| Text and media | Text, links, uploaded/URL media and video protocol messages have service implementations. [Sending service](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/sendMessage/service/send_service.go#L604-L1530) | Text, image, document, audio, voice and native MP4 video; authenticated download; bounded base64/multipart input. [Media guide](media.pt-BR.md), [advanced messages](advanced-messages.md) | Explicit quote/mention/context options and link previews. Evaluate URL ingestion separately with bounded fetching. |
| Additional message types | Poll creation, URL-to-WebP stickers, locations and contact cards construct protocol messages. [Implementations](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/sendMessage/service/send_service.go#L1531-L1721) | Native poll creation, prepared static/animated WebP stickers, static locations and single/multiple contact cards, with bounded input and normalized events. [Contract and tests](advanced-messages.md) | Image-to-sticker conversion, live location and poll vote/results require separate work. No remote URL ingestion. |
| Message actions | Reaction, played/read receipts, deletion for everyone and text editing are registered; services construct reaction/revoke/edit messages. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L147-L160), [service](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/message/service/message_service.go#L136-L203), [revoke/edit](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/message/service/message_service.go#L467-L526) | Basic mark-read and media download. | Reactions, explicit receipt IDs, edit/revoke authorization and delivery-receipt events. |
| Groups | Group metadata, participants, creation, invites, settings and join/leave. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L174-L191) | 22 group operations including partial participant results and normalized events. [Group guide](groups.pt-BR.md) | Preserve partial-result and permission semantics while extending capabilities. |
| Contacts and account | Contact/user/avatar lookups, privacy settings, blocklist and own profile mutation. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L129-L146), [services](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/user/service/user_service.go#L394-L542) | Contact/profile lookup and PN/LID association; presence observation/publication and typing/recording. [Directory guide](contacts-chats-presence.pt-BR.md) | Privacy, blocking and own profile updates are separate future scope. |
| Chats and labels | Chat mutations/history-sync request plus label CRUD/assignment routes. Several chat mutations carry explicit source TODOs. [Chats](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L162-L172), [labels](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L208-L225) | Persistent observed chat metadata, filters and pagination; label/community filters return `501`. [Directory guide](contacts-chats-presence.pt-BR.md) | Verified chat mutations, label persistence and label event normalization. |
| Communities and newsletters | Community creation/link/unlink and newsletter creation/query/live subscription/message retrieval call SDK methods. [Communities](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/community/service/community_service.go#L77-L157), [newsletters](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/newsletter/service/newsletter_service.go#L88-L195) | No dedicated community/newsletter API. | Define supported identifiers, permissions and event shapes before adding these APIs. |
| Interactive messages and statuses | Buttons, lists, carousel and text/media status sending have route and service implementations. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L122-L126), [interactive service](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/sendMessage/service/send_service.go#L1736-L2348), [carousel/status service](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/sendMessage/service/send_service.go#L2899-L3366) | Not exposed by our server. | Investigate the pinned SDK and actual account/client constraints. Source availability is insufficient to promise universal delivery. |
| Calls and polls | Call rejection route and stored poll-vote/result service. [Routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L193-L197), [poll persistence](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/poll/service/poll_service.go#L16-L98) | No call-management or poll-result API. | Call events/rejection and poll votes/results with session isolation and retention. |

## Persistence, delivery and operations

Evolution wires PostgreSQL/GORM for instances, message metadata and labels;
authentication storage can use PostgreSQL or SQLite.
[Startup and migrations](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/cmd/evolution-go/main.go#L264-L315),
[configuration](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/config/config.go#L193-L207).
Its optional message model stores ID, timestamp, status, source and referral JSON.
This is not evidence of a complete message-body archive.
[Message model](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/message/model/message_model.go#L10-L18).
Our SQLite session/chat persistence and in-memory attachment descriptor cache
have different retention and query semantics. [Our compatibility matrix](compatibility.pt-BR.md).

Evolution has a MinIO client for object uploads and retrieval. Its initialization
also attempts to apply a public object-read bucket policy. A future object-storage
adapter here should define private access, session ownership, retention and
short-lived downloads explicitly rather than inherit that default.
[MinIO implementation](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/storage/minio/media_storage.go#L25-L108).

Evolution wires Webhook, WebSocket, RabbitMQ and NATS producers.
[Producer setup](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/cmd/evolution-go/main.go#L94-L147),
[WebSocket endpoint](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/cmd/evolution-go/main.go#L248-L259).
NATS uses core `Publish`; this source does not establish JetStream persistence.
[NATS producer](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/events/nats/nats_producer.go#L44-L76).
Its event categories include receipts, calls, labels, newsletters and history.
[Event definitions](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/internal/event_types/event_types.go#L3-L40).
Our bounded HTTP webhook queue/retries are implemented; additional transports,
durable outbox/replay and those additional event families remain future work.
[Our dispatcher](../internal/webhook/dispatcher.go).

Evolution includes a React manager, Swagger UI, API-key authentication, proxy
configuration and a passkey pairing ceremony.
[UI/routes](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L60-L91),
[authentication](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/middleware/auth_middleware.go#L24-L58),
[passkey endpoints](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/passkey/handler/passkey_handler.go#L1-L10).
Our server exposes a JSON OpenAPI document, health/readiness probes and deployment
stacks; it does not embed that management UI or use Evolution's API keys.
[Our contract](openapi.json), [deployment guide](docker-portainer.pt-BR.md).

## Reference caveats

- Use registered routes as the API reference: the reference README's example
  `/message/sendText` differs from the registered `/send/text`.
  [Registration](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L111-L127).
- Chat pin/archive/mute and `/group/myall` have explicit "not working" TODOs;
  do not count them as proven parity targets.
  [Chat TODOs](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L165-L170),
  [group TODO](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/pkg/routes/routes.go#L186).
- The source module requires Go `1.25.0`, despite the reference README stack
  table saying `1.24+`. [Module](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/go.mod#L1-L3).
- Evolution installs a license gate and activation routes. Its LICENSE adds
  conditions to Apache 2.0, and NOTICE/TRADEMARKS describe attribution and branding.
  These describe the reference project; this benchmark does not adopt those terms,
  assets, activation workflow or source code.
  [Runtime wiring](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/cmd/evolution-go/main.go#L219-L226),
  [LICENSE](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/LICENSE#L1-L13),
  [NOTICE](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/NOTICE),
  [TRADEMARKS](https://github.com/evolution-foundation/evolution-go/blob/9337afc47e10b86cc896a6f432240e40fee95dd1/TRADEMARKS.md).

## Planning implications

1. Expand native messaging and message actions before adding transport adapters.
2. Add account/privacy and verified chat mutations, then labels and new event families.
3. Design storage retention, durable delivery and administrative authentication as
   explicit contracts; a longer feature list alone does not define reliability.
4. Evaluate communities, newsletters, status and interactive messages against our
   pinned SDK and tested client behavior. Keep unsupported responses explicit.
5. Require source-backed documentation, contract tests, isolation, bounded resources
   and reproducible release evidence for each stage. The [roadmap](roadmap.md)
   records future work separately from already delivered capabilities.
