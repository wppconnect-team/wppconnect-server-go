<p align="center">
  <a href="https://github.com/wppconnect-team">WPPConnect Team</a>
</p>

<h1 align="center">WPPConnect Server Go</h1>

<p align="center">
  A browser-free WhatsApp integration API built with Go and whatsmeow.
</p>

<p align="center">
  <a href="https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml"><img src="https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" /></a>
  <a href="https://github.com/wppconnect-team/wppconnect-server-go/releases/latest"><img src="https://img.shields.io/github/v/release/wppconnect-team/wppconnect-server-go" alt="Latest release" /></a>
  <a href="https://hub.docker.com/r/wppconnect/wppconnect-server-go"><img src="https://img.shields.io/docker/pulls/wppconnect/wppconnect-server-go?logo=docker" alt="Docker pulls" /></a>
  <a href="https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags"><img src="https://img.shields.io/docker/v/wppconnect/wppconnect-server-go?sort=semver&logo=docker" alt="Docker version" /></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#api-reference">API reference</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#documentation">Documentation</a> ·
  <a href="#roadmap-and-evolution-go-benchmark">Roadmap</a>
</p>

## About

WPPConnect Server Go provides WhatsApp sessions, messaging, groups, contacts,
chat metadata and presence through an HTTP API. It uses the
[WPPConnect whatsmeow fork](https://github.com/wppconnect-team/whatsmeow) to
connect directly to WhatsApp, without running Chromium or Puppeteer.

The project implements selected routes and payload conventions from
[WPPConnect Server](https://github.com/wppconnect-team/wppconnect-server).
It is designed for gradual migration of integrations that use those routes.
Support is defined by the documented contract, rather than full equivalence
with the Node server or WhatsApp Web's internal database.

**Published baseline:** [v0.1.6](https://github.com/wppconnect-team/wppconnect-server-go/releases/tag/v0.1.6)
includes advanced native messages and Linux amd64/arm64 images. The expansion
described below is implemented in source and requires a build containing this
change or a subsequent release. Earlier image tags do not acquire these features.
See the [expanded operations guide](docs/roadmap-completion.md) for complete
deployment, authentication, adapter and API examples.

## Features

| Area | Available capabilities |
| --- | --- |
| Sessions | QR pairing, connection status, automatic restoration, separate disconnect/logout and graceful shutdown. |
| Authentication | Session-bound bcrypt tokens, token generation, Bearer authentication and legacy Node token formats. |
| Messaging | Text messages, contact cards, static locations, native polls, number checks and the basic `send-seen` route. Advanced types are source checkout additions. |
| Media | Images, documents, regular audio, Ogg/Opus voice, MP4 video and prepared static/animated WebP stickers; base64/multipart uploads and authenticated download. Video/stickers are source checkout additions. |
| Groups | 22 routes for participants/admins, metadata, restrictions, invitations, join/leave and group pictures. |
| Contacts | Synchronized contacts, local phone number/LID association and profile status/picture queries. |
| Chats | Persistent observed chat metadata, pagination, filters and updates from history/app-state events. |
| Presence | Online/last-seen observations, presence subscriptions and typing/recording signals. |
| Webhooks | Normalized events, bounded workers and queue, retries, stable delivery IDs and shutdown draining. |
| Deployment | Docker, Compose, Portainer Standalone/Swarm examples, health/readiness endpoints and multiarch CI. |
| API contract | OpenAPI 3.0.3, a compatibility matrix and HTTP contract tests. |

The expanded source also includes message actions and quoted replies, a bounded
SQLite archive, account/privacy/blocklists, acknowledged chat mutations and
labels, durable event replay, WebSocket/RabbitMQ/NATS adapters, private S3 media,
phone pairing, per-session settings, Prometheus metrics and an optional English
management console at `/manager`. Communities, newsletters, status and poll
voting use native SDK operations. Buttons/lists are opt-in experimental;
carousels explicitly return `501`. No live WhatsApp account was used in tests.

## Quick start

### Docker

Requirements: Docker with Linux containers. The examples below use Bash;
API examples also use `curl` and `jq`.

```bash
export SECRET_KEY="$(openssl rand -hex 24)"

docker run -d \
  --name wppconnect-server-go \
  --restart unless-stopped \
  --stop-timeout 35 \
  -p 127.0.0.1:21465:21465 \
  -e SECRET_KEY \
  -e DATA_DIR=/data \
  -v wppconnect_go_data:/data \
  wppconnect/wppconnect-server-go:0.1.3

curl -fsS http://localhost:21465/healthz
curl -fsS http://localhost:21465/readyz
docker logs --tail 50 wppconnect-server-go
```

Keep the generated secret for subsequent API requests and restarts. The named
volume holds credentials and chat metadata. Bind to an appropriate interface
or place an HTTPS reverse proxy in front of the API for remote access.

Pin a release tag or image digest for repeatable deployments. See
[Docker Hub tags](https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags)
and [GitHub releases](https://github.com/wppconnect-team/wppconnect-server-go/releases)
for published versions; a Git tag alone does not confirm an image was published.

### Docker Compose

```bash
git clone https://github.com/wppconnect-team/wppconnect-server-go.git
cd wppconnect-server-go
cp .env.example .env
# Edit .env: set a private SECRET_KEY and WPP_SERVER_GO_TAG=0.1.3.

docker compose --env-file .env up -d
docker compose ps
curl -fsS http://localhost:21465/healthz
```

The root Compose file publishes port `21465`. To customize the host port with
`WPP_SERVER_GO_PORT`, use the Standalone stack:

```bash
docker compose --env-file .env -f deploy/portainer-stack.yml up -d
```

### Portainer

Use [deploy/portainer-stack.yml](deploy/portainer-stack.yml) for Docker
Standalone or [deploy/portainer-swarm.yml](deploy/portainer-swarm.yml) for Swarm.
Set `SECRET_KEY` and choose `WPP_SERVER_GO_TAG=0.1.3` or a newer published tag
before deployment. Preserve the same volume when updating the stack.

Swarm uses a single replica pinned to one labeled data node. The embedded
SQLite stores are not a shared, horizontally replicated session backend.
See the [complete Portainer guide, in Portuguese](docs/docker-portainer.pt-BR.md)
for stack setup, networking, backup and restore instructions.

### Run from source

Requirements: **Go 1.26.10 or later**. SQLite is provided by a pure Go driver;
normal builds do not require CGO or an external database server.

```bash
git clone https://github.com/wppconnect-team/wppconnect-server-go.git
cd wppconnect-server-go
go mod download

export SECRET_KEY="$(openssl rand -hex 24)"
export DATA_DIR="$PWD/userDataDir"
go run ./cmd/server
```

Source execution reads environment variables directly; it does not load `.env`
automatically. To build the server and token helper:

```bash
mkdir -p bin
go build -o ./bin/ ./cmd/server ./cmd/token
```

## Connect your first session

### 1. Generate a token

Use the same `SECRET_KEY` configured on the server:

```bash
export BASE_URL=http://localhost:21465
export SESSION=mysession

TOKEN=$(curl -fsS -X POST \
  "$BASE_URL/api/$SESSION/$SECRET_KEY/generate-token" | jq -er .token)
```

The response uses HTTP `201` and contains `status`, `session`, `token` and
`full`. The token is a bcrypt hash of the session name plus secret. It is not
the secret itself, and a token for one session does not authorize another.

The bootstrap route includes the secret in its URL. An offline helper avoids
putting that secret in reverse-proxy access logs:

```bash
# With the server container from the Docker example:
TOKEN=$(docker exec wppconnect-server-go /app/token -session "$SESSION" | jq -er .token)

# Or with Go installed and SECRET_KEY exported locally:
TOKEN=$(go run ./cmd/token -session "$SESSION" | jq -er .token)
```

Session names accept 1-64 ASCII letters, digits, underscores or hyphens.
Token generation requires the session name plus secret to fit in **72 bytes**.
Raw bcrypt, URL-safe bcrypt and the legacy `session:token` URL format are
accepted. Prefer the Bearer header so tokens are not included in request URLs.

### 2. Start and pair

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/start-session" \
  -H "Authorization: Bearer $TOKEN"

curl -fsS "$BASE_URL/api/$SESSION/status-session" \
  -H "Authorization: Bearer $TOKEN" | jq

# Once the status is QRCODE:
curl -fsS -D qrcode.headers "$BASE_URL/api/$SESSION/qrcode-session" \
  -H "Authorization: Bearer $TOKEN" -o qrcode.png
```

A ready QR response has `Content-Type: image/png`. If no QR is available, the
endpoint returns HTTP `200` with JSON `QRCODE_NOT_AVAILABLE`; inspect the status
and headers before opening the downloaded file as an image. Scan the QR using
WhatsApp's **Linked devices** screen and wait for `CONNECTED`.

### 3. Send a message

Replace the example number with your intended recipient, including country code:

```bash
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-message" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","message":"Hello from WPPConnect Server Go!","isGroup":false}'
```

A successful text send returns HTTP `201` with `{"status":"success"}`.
For group text messages, pass the group ID without its `@g.us` suffix and
set `isGroup: true`.

## API reference

The runtime contract is available at **`GET /openapi.json`**. Import it into an
OpenAPI client or documentation viewer; the server does not bundle Swagger UI.
A checked-in [OpenAPI export](docs/openapi.json) is generated from the same
contract. The [compatibility matrix](docs/compatibility.pt-BR.md) documents
response shapes, missing fields and test coverage.

All paths below have the prefix **`/api/{session}`**, unless shown as a full
public path. Session operations require `Authorization: Bearer <token>`, apart
from token generation, which checks `{secretkey}`.

| Area | Method | Routes |
| --- | --- | --- |
| Session | POST | `/start-session`, `/close-session`, `/logout-session` |
| Session | GET | `/status-session`, `/qrcode-session` |
| Token bootstrap | POST | `/{secretkey}/generate-token` |
| Text and read receipts | POST | `/send-message`, `/send-seen` |
| Number lookup | GET | `/check-number-status/{phone}` |
| Media upload | POST | `/send-image`, `/send-file`, `/send-file-base64`, `/send-audio-base64`, `/send-voice`, `/send-voice-base64` |
| Advanced media, source checkout | POST | `/send-video`, `/send-video-base64`, `/send-sticker`, `/send-sticker-gif` |
| Structured messages, source checkout | POST | `/contact-vcard`, `/send-contact`, `/send-location`, `/send-poll-message` |
| Media download | POST / GET | `/download-media` / `/get-media-by-message/{messageId}` |
| Contacts and profile | GET | `/all-contacts`, `/contact/{phone}`, `/contact/pn-lid/{pnLid}`, `/profile-pic/{phone}`, `/profile-status/{phone}` |
| Chats | GET | `/all-chats`, `/all-chats-archived`, `/chat-by-id/{phone}` |
| Chat search | POST | `/list-chats` |
| Presence observation | GET | `/chat-is-online/{phone}`, `/last-seen/{phone}` |
| Presence commands | POST | `/subscribe-presence`, `/set-online-presence`, `/typing`, `/recording`, `/chat-state` |
| Group queries | GET | `/all-groups`, `/common-groups/{wid}`, `/group-info/{groupId}`, `/group-members/{groupId}`, `/group-members-ids/{groupId}`, `/group-admins/{groupId}` |
| Group invitations | GET | `/group-invite-link/{groupId}`, `/group-revoke-link/{groupId}` |
| Group creation and membership | POST | `/create-group`, `/add-participant-group`, `/remove-participant-group`, `/promote-participant-group`, `/demote-participant-group` |
| Group settings | POST | `/group-subject`, `/group-description`, `/group-property`, `/messages-admins-only`, `/change-privacy-group`, `/group-pic` |
| Group join/leave | POST | `/join-code`, `/group-info-from-invite-link`, `/leave-group` |
| Operations, full public paths | GET | `/healthz`, `/readyz`, `/openapi.json`, `/api/dashboard/stats` |

`group-revoke-link` changes invitation state despite using GET for compatibility.
Group actions can partially succeed; inspect HTTP `207` and per-participant
results before retrying. An existing route does not imply identical internal
models or all advanced options from the Node server.

Advanced sends stop on the first failure and return accepted IDs in `completed`
with `failedRecipient`. A failed recipient can have unknown delivery status.
Sticker routes require prepared WebP, including animated WebP for
`send-sticker-gif`; they do not convert GIF input. See
[formats, full examples and response contracts](docs/advanced-messages.md).

### Media, chats and presence examples

```bash
# Send a document with multipart upload:
curl -fsS -X POST "$BASE_URL/api/$SESSION/send-file" \
  -H "Authorization: Bearer $TOKEN" \
  -F phone=5511999999999 \
  -F 'file=@report.pdf;type=application/pdf'

# List observed chats with unread messages:
curl -fsS -X POST "$BASE_URL/api/$SESSION/list-chats" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"count":20,"onlyUsers":true,"onlyWithUnreadMessage":true}'

# Publish a recording indicator for five seconds, then pause:
curl -fsS -X POST "$BASE_URL/api/$SESSION/recording" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"phone":"5511999999999","value":true,"duration":5000}'

# Create a group:
curl -fsS -X POST "$BASE_URL/api/$SESSION/create-group" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Support team","participants":["5511999999999"]}'
```

Presence `duration` uses milliseconds and waits before sending pause. It does
not schedule continuous renewal. Chat pagination uses Node's observed
semantics: `after` includes its anchor, while `before` excludes it.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `21465` | HTTP listen port; range 1-65535. |
| `SECRET_KEY` | `THISISMYSECURETOKEN` | Token bootstrap secret. Replace the compatibility default before exposing the API. |
| `DATA_DIR` | `./userDataDir` from source; `/data` in Docker | WhatsApp SQLite stores, session records and chat indexes. |
| `WEBHOOK_URL` | empty | HTTP(S) event receiver; empty disables delivery. |
| `AUTO_RESTORE_SESSIONS` | `true` | Restore eligible paired sessions at startup. |
| `SHUTDOWN_TIMEOUT` | `25s` | Total shutdown budget; 1 ms to 5 minutes. |
| `WEBHOOK_WORKERS` | `2` | Concurrent deliveries; range 1-32. |
| `WEBHOOK_QUEUE_SIZE` | `256` | Legacy compatibility setting; durable capacity uses `OUTBOX_MAX_*`. |
| `WEBHOOK_ATTEMPTS` | `4` | Total attempts per event; range 1-10. |
| `WEBHOOK_TIMEOUT` | `10s` | Timeout per attempt; 1 ms to 1 minute. |
| `WPP_SERVER_GO_TAG` | example/stack setting | Docker image tag, not a server setting. |
| `WPP_SERVER_GO_PORT` | `21465` in Portainer stacks | Host port for the Standalone/Swarm templates, not the root Compose file. |

| `ADMIN_TOKEN` | empty | At least 32 bytes; enables separate administrator Bearer/cookie authentication. |
| `ENABLE_MANAGER` | `true` | Serve the optional English operations console at `/manager`. |
| `HISTORY_RETENTION_DAYS` / `HISTORY_MAX_MESSAGES` / `HISTORY_MAX_BYTES` | `30` / `100000` / `268435456` | Per-session observed message retention and payload quotas. |
| `OUTBOX_RETENTION_DAYS` / `OUTBOX_MAX_EVENTS` / `OUTBOX_MAX_BYTES` | `7` / `100000` / `134217728` | Global retained events and payload quotas; pending deliveries are protected from eviction. |
| `NATS_URL` / `AMQP_URL` | empty | Optional Core NATS and confirmed persistent RabbitMQ delivery. |
| `S3_ENDPOINT` / `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` / `S3_BUCKET` | empty | Enable explicit archival in a pre-existing private bucket. |
| `S3_REGION` / `S3_USE_SSL` / `MEDIA_RETENTION_DAYS` | empty / `true` / `7` | S3 region, TLS and object retention. |
| `ENABLE_EXPERIMENTAL_INTERACTIVES` | `false` | Opt in to legacy buttons/lists with client/account limitations. |

See [`.env.example`](.env.example) for deployment settings. Set the Docker stop
grace period above `SHUTDOWN_TIMEOUT`; the supplied stacks allow `35s` for the
default `25s` shutdown budget.

## Events and webhooks

Set `WEBHOOK_URL` to an address reachable from inside the API container.
`localhost` refers to that container, rather than the Docker host or another
service. Containers in separate stacks need a shared network or a routable URL.

| Event | Payload purpose |
| --- | --- |
| `qrcode` | Pairing code in `urlcode`. |
| `status-find` | Connection and lifecycle status. |
| `onmessage` | Normalized text/image/document/audio and source additions: video, stickers, contact cards, location and poll metadata. |
| `onparticipantschanged` | Group participant and administrator changes. |
| `onaddedtogroup` | The account joined a group. |
| `ongroupchange` | Group metadata and picture updates. |
| `onpresencechanged` | Individual presence and typing/recording signals. |

Every payload includes `event` and `session`. For example:

```json
{
  "event": "status-find",
  "session": "mysession",
  "status": "CONNECTED"
}
```

Deliveries carry `X-WPP-Event-ID` and `X-WPP-Attempt` headers. Retries reuse the
same ID and body, allowing receivers to deduplicate. HTTP `2xx` confirms a
successful delivery. Transport errors, `408`, `425`, `429` and `5xx` can retry
with exponential backoff and bounded `Retry-After` handling.

In expanded source, the SQLite outbox persists events and per-target attempts
before delivery. Replays preserve IDs, pending deliveries recover after restart,
and target failures are independent. Payloads above 1 MiB or saturated logs are
rejected with a metric. Retained-event quotas replace the old in-memory queue.
Authenticated WebSocket replay, Core NATS flush and RabbitMQ publisher confirms
have distinct guarantees described in the [expanded guide](docs/roadmap-completion.md).

## Persistence and lifecycle

Each session owns its credentials and stores under `DATA_DIR`:

```text
/data/
  mysession.db             WhatsApp device credentials and SDK stores
  mysession.directory.db   Observed chat metadata
  mysession.session.json   Restore/revocation intent and private settings
  mysession.history.db     Observed messages, receipts, labels and media ownership
  .events.db               Global durable delivery and retained events
```

| Action | Credentials | Next process startup |
| --- | --- | --- |
| `close-session` | Preserved; connections and stores closed. | Stays closed until `start-session`. |
| Successful `logout-session` | Linked device revoked and credentials removed. | New pairing required. |
| SIGTERM / SIGINT | Preserved for active paired sessions. | Eligible sessions restore automatically. |

Logout requires a connected, authenticated session. A failed revocation returns
an error and preserves the handle. Empty stores do not start unsolicited QR
pairing during restoration. Session status can be `INITIALIZING`, `QRCODE`,
`CONNECTED`, `DISCONNECTED`, `UNPAIRED` or `CLOSED`.

Chat metadata and observed messages persist across restart. Presence remains
ephemeral. Retained protocol data enables media downloads and poll decryption;
it is excluded from HTTP responses. Back up the complete volume while stopped,
including SQLite sidecars and S3 metadata/objects when enabled. Keep backups private.

`/healthz` checks process liveness. `/readyz` returns `503` during initial
restoration, then reflects completion of that startup phase. Neither endpoint
guarantees that every individual WhatsApp session is connected.

## Limits and compatibility

| Scope | Current bound or behavior |
| --- | --- |
| Live sessions | Up to 100 per process. |
| Media files | 16 MiB decoded; up to 4 concurrent media operations per process. |
| Media descriptor cache | Up to 1,024 entries per session, with a 24-hour TTL; lost on restart. |
| Chat index | Up to 10,000 entries and 64 recent message IDs per chat; metadata only. |
| Presence cache | Up to 10,000 contacts per session, with a 5-minute observation TTL. |
| Groups/directory/structured message JSON bodies | 64 KiB; media has separate size limits. |
| Presence commands | Up to 100 destinations; optional duration 1-10,000 ms. |
| Webhook payloads | At most 1 MiB serialized. |

An unknown or hidden presence value is returned as `null` with `known: false`,
rather than being presented as offline. Contact names and PN/LID mappings depend
on what the SDK has synchronized. Chat unread counts can be partial and expose
`unreadCountIsPartial` accordingly.

The eight legacy history operations now query the observed archive with cursor
envelopes. They do not provide a complete browser inbox. Label/community filters
are supported using observed labels and joined community metadata. Additional
limits and endpoint examples are in the [expanded guide](docs/roadmap-completion.md).
Carousel delivery returns `501`; buttons/lists are experimental and disabled by
default. Business catalog/templates and call media transport remain outside the
implemented scope. Universal client/account compatibility is not promised.

## Security and operations

Use HTTPS and restrict network access to the API. Liveness, readiness and
OpenAPI are public. Dashboard stats, metrics and administration require the
separate administrator credential; session tokens cannot access those routes. Avoid logging
the token bootstrap URL, Bearer headers, QR contents or device credentials.

Session record access validates names at the storage boundary and uses Go's
`os.Root` to prevent paths and symbolic links from escaping the data directory.
Tokens remain valid for the same session/secret pair; changing `SECRET_KEY`
invalidates previously issued tokens. This project does not add expiration to
bcrypt tokens.

For a suspected vulnerability, use a private reporting channel available through
the repository's [Security page](https://github.com/wppconnect-team/wppconnect-server-go/security).
Keep credentials and exploit details out of public issues and logs.

## Project structure

```text
cmd/
  server/        HTTP server and shutdown handling
  token/         Offline bcrypt token helper
  openapi/       OpenAPI export command
  smoke-image/   Docker startup, healthcheck and restart verification
internal/
  auth/          Session-bound token compatibility
  config/        Environment configuration and validation
  session/       WhatsApp clients, persistence and lifecycle
  media/         Bounded uploads, descriptors, downloads and event normalization
  groups/        Group identifiers, contracts and normalized events
  directory/     Contacts, observed chat index and recent presence
  eventbus/      Durable outbox, replay and delivery adapters
  history/       Bounded message archive, labels and poll results
  vault/         Private S3-compatible media ownership and retention
  account/       Own profile and privacy validation
  channels/      Communities, newsletters, polls and calls SDK seam
  webhook/       Legacy delivery implementation and compatibility tests
  httpapi/       HTTP handlers, authentication and OpenAPI contract
scripts/         Independent Node bcrypt fixture generation
deploy/          Portainer Standalone and Swarm stacks
docs/            API export, detailed guides, compatibility and roadmap
.github/         CI, security analysis and release workflows
```

| Component | Technology |
| --- | --- |
| Runtime | Go 1.26.10+ |
| HTTP routing | chi |
| WhatsApp protocol | Pinned WPPConnect whatsmeow fork |
| Persistence | SQLite through modernc.org/sqlite, without CGO for normal builds |
| Authentication | bcrypt through golang.org/x/crypto |
| API documentation | OpenAPI 3.0.3; kin-openapi validation in tests |
| Deployment | Docker, Compose and Portainer; Linux amd64/arm64 |

The code imports `go.mau.fi/whatsmeow`; the versioned `replace` directive in
[`go.mod`](go.mod) selects the WPPConnect fork. Dependency upgrades should update
that pinned revision deliberately and rerun protocol/contract tests.

## Development and verification

```bash
go test ./...
go vet ./...

# Requires CGO and a supported C toolchain for the race detector:
go test -race ./...

# Export the runtime contract after an API change:
go run ./cmd/openapi > docs/openapi.json

# Validate deployment files with a nonproduction secret:
SECRET_KEY=ci-only-secret docker compose config --quiet
SECRET_KEY=ci-only-secret docker compose -f deploy/portainer-stack.yml config --quiet
SECRET_KEY=ci-only-secret docker stack config -c deploy/portainer-swarm.yml

# Build and verify a local Docker image:
docker buildx build --platform linux/amd64 --load -t wppconnect-go:check .
go run ./cmd/smoke-image -image wppconnect-go:check -platform linux/amd64
```

Use `linux/arm64` in both image commands to test ARM on a compatible host or
through emulation. CI validates both architectures before the release workflow
publishes an image. Tests cover HTTP contracts, SQLite reopening, isolation,
protocol failures, media integrity, webhook limits and independently generated
Node bcrypt fixtures. WhatsApp transport is simulated in automated tests;
passing CI does not certify behavior with every live account.

## Documentation

English is the primary language of this README. Existing detailed guides are
currently available in Portuguese:

| Resource | Purpose |
| --- | --- |
| [OpenAPI export](docs/openapi.json) | Machine-readable routes, request/response schemas and explicit unsupported operations. |
| [Docker and Portainer](docs/docker-portainer.pt-BR.md) | Deployment, stacks, pairing, updates and backups. |
| [Media](docs/media.pt-BR.md) | Upload/download examples, MIME handling, voice requirements and limits. |
| [Advanced native messages](docs/advanced-messages.md) | Video, static/animated WebP stickers, contact cards, locations, polls and complete examples. Source checkout additions. |
| [Groups](docs/groups.pt-BR.md) | All group routes, partial results and events. |
| [Contacts, chats and presence](docs/contacts-chats-presence.pt-BR.md) | PN/LID, metadata pagination, presence and observations. |
| [Operations](docs/operation.pt-BR.md) | Restoration, disconnect/logout, shutdown, security boundaries and webhook behavior. |
| [Compatibility matrix](docs/compatibility.pt-BR.md) | Node compatibility and automated evidence per capability. |
| [Detailed roadmap](docs/roadmap.md) | Completed baseline and expansion acceptance evidence. |
| [Expanded API and operations](docs/roadmap-completion.md) | Complete English examples for the archive, actions, manager, adapters, S3 and evaluated features. |
| [Evolution Go benchmark](docs/evolution-go-benchmark.md) | Source-backed comparison, gaps and implementation priorities. |

## Roadmap and Evolution Go benchmark

[Evolution Go](https://github.com/evolution-foundation/evolution-go) is a
capability and documentation benchmark for future development. Its API,
integrations, licensing and product UI are separate from this project.
The [pinned source comparison](docs/evolution-go-benchmark.md) records which
features have been verified in its source and where our implementation differs.

All expansion items in the [roadmap](docs/roadmap.md) are addressed in source,
including the SDK support assessment. The [English guide](docs/roadmap-completion.md)
documents implemented workflows and explicit limitations. Each capability has
bounded inputs, session authorization, tests and an OpenAPI contract.

This does not establish full Evolution Go parity: contracts differ, the console
has a deliberately scoped workflow, buttons/lists remain experimental and
carousels, business catalogs/templates and call media are unsupported. Live
WhatsApp validation was omitted at the user's request. The pinned benchmark is
historical evidence; the completion guide describes the current source.

## Contributing

Open an [issue](https://github.com/wppconnect-team/wppconnect-server-go/issues)
for a bug or scoped feature proposal, or submit a pull request against `main`.
Include the expected HTTP contract, compatibility differences and relevant tests.
Keep OpenAPI and the capability documentation synchronized with behavior changes.
Run the checks above, use test credentials in examples and preserve the separation
between device disconnect and credential revocation.

## Acknowledgments and licensing

This project builds on [whatsmeow](https://github.com/tulir/whatsmeow), the
[WPPConnect fork](https://github.com/wppconnect-team/whatsmeow),
[WPPConnect Server](https://github.com/wppconnect-team/wppconnect-server) and
their contributors. Evolution Go is referenced as a feature/documentation
benchmark; this README does not import its code, branding or license terms.

Licensed under [Apache-2.0](LICENSE). See [NOTICE](NOTICE) and
[third-party licenses](third_party/licenses.txt), also shipped in the container.
Dependency source/version references are retained in `go.mod` and the notices;
whatsmeow remains under its upstream MPL-2.0 terms.
