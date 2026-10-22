# wppconnect-server-go

[![Docker Pulls](https://img.shields.io/docker/pulls/wppconnect/wppconnect-server-go?logo=docker)](https://hub.docker.com/r/wppconnect/wppconnect-server-go)
[![Docker Image Version](https://img.shields.io/docker/v/wppconnect/wppconnect-server-go?sort=semver&logo=docker)](https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags)
[![GitHub Release](https://img.shields.io/github/v/release/wppconnect-team/wppconnect-server-go)](https://github.com/wppconnect-team/wppconnect-server-go/releases/latest)

A Go port of [wppconnect-server](https://github.com/wppconnect-team/wppconnect-server),
backed by [whatsmeow](https://github.com/tulir/whatsmeow) instead of a browser
(Puppeteer). It keeps the **HTTP contract compatible** with the Node server -
same routes and payload field names - so existing clients can migrate with
minimal changes.

> **Status: MVP scaffold.** Session lifecycle, QR, send-message and webhooks are
> wired. The full ~148-route surface is being filled in incrementally.

## Why a Go version

- **No browser.** whatsmeow speaks the WhatsApp multidevice protocol directly -
  far lower memory/CPU than a Chromium-per-session model.
- **Single binary.** Trivial to deploy and to manage from the `wppconnect-manager`.

## Architecture

```
cmd/server            entrypoint
internal/config       env-based config (PORT, SECRET_KEY, WEBHOOK_URL, DATA_DIR)
internal/session      SessionManager over whatsmeow (SQLite store)
internal/media        bounded attachments, per-session descriptors and normalized content
internal/groups       group contracts, participant IDs and normalized group events
internal/directory    contacts, persistent observed chat index and recent presence
internal/webhook      Dispatcher - posts normalized events (event + data)
internal/httpapi      chi router with Node-compatible routes
cmd/openapi           export the runtime OpenAPI contract
cmd/smoke-image       test Docker startup, healthcheck and restart
```

## Endpoints (MVP)

| Method | Path                                          | Node equivalent |
| ------ | --------------------------------------------- | --------------- |
| POST   | `/api/{session}/{secretkey}/generate-token`   | same (this checkout) |
| POST   | `/api/{session}/start-session`                | same            |
| GET    | `/api/{session}/status-session`               | same            |
| POST   | `/api/{session}/send-message`                 | same            |
| POST   | `/api/{session}/send-image`                   | same (base64)   |
| POST   | `/api/{session}/send-file`                    | base64/multipart |
| POST   | `/api/{session}/send-file-base64`             | same (base64)   |
| POST   | `/api/{session}/send-voice-base64`            | same (`base64Ptt`, Ogg/Opus) |
| POST   | `/api/{session}/send-voice`                   | base64/multipart, no path loading |
| POST   | `/api/{session}/send-audio-base64`            | Go extension (regular audio) |
| POST   | `/api/{session}/download-media`              | same (`messageId`) |
| GET    | `/api/{session}/get-media-by-message/{messageId}` | same (base64 + mimetype) |
| POST   | `/api/{session}/send-seen`                    | same            |
| GET    | `/api/{session}/check-number-status/{phone}`  | same            |
| GET    | `/api/{session}/all-groups`                   | same            |
| POST   | `/api/{session}/create-group`                 | same (`name`, `participants`) |
| GET    | `/api/{session}/group-members/{groupId}`       | available participant fields |
| GET    | `/api/{session}/group-info/{groupId}`          | group metadata  |
| POST   | `/api/{session}/close-session`                | same            |
| POST   | `/api/{session}/logout-session`               | revoke linked device |
| GET    | `/healthz`                                     | same            |
| GET    | `/readyz`                                      | restoration readiness |
| GET    | `/openapi.json`                                | implemented Go contract |

Payloads match the Node server: `send-message` takes `phone` (string or array),
`message`, `isGroup`; `send-image` takes `phone`, `base64` (raw or data URL),
`caption`, `isGroup`; `send-seen` takes `phone`.

Media sending, normalized image/document/audio webhooks and authenticated
downloads are implemented in this checkout, with a 16 MiB file limit and a
bounded per-session descriptor cache. See the [media guide](docs/media.pt-BR.md)
for complete examples, response formats and compatibility limits. These new
media features are not included in published image `0.1.2`.

Group management is implemented in this checkout, including participant
actions, administrators, metadata, properties, invite links, join/leave, photos
and group webhooks. See the [complete groups guide](docs/groups.pt-BR.md) for all
22 routes, Node-compatible request fields, partial results and compatibility
limits. The new group routes are not included in published image `0.1.2`.

Contacts, synchronized chat metadata and presence are implemented in this
checkout through 16 routes. The [contacts/chats/presence guide](docs/contacts-chats-presence.pt-BR.md)
covers PN/LID lookup, the persistent SQLite chat index, filters/pagination,
online status, typing/recording, webhooks and explicit unsupported features.
Unknown presence is returned as unknown. These features also require a new
image built from this checkout.

Operation and lifecycle support includes automatic restoration, separate
disconnect/logout, graceful shutdown, bounded webhook retries and a multiarch
release pipeline gated on image tests. See the [operation guide](docs/operation.pt-BR.md).
The [compatibility matrix](docs/compatibility.pt-BR.md) records supported fields,
limits and automated evidence. The contract is served at `/openapi.json` and
exported to [docs/openapi.json](docs/openapi.json). These changes are submitted
for review; published image `0.1.2` is unchanged.

## Docker (recommended)

**Portainer:** see the complete [Docker and Portainer guide in Portuguese](docs/docker-portainer.pt-BR.md),
with [Standalone](deploy/portainer-stack.yml) and [Swarm](deploy/portainer-swarm.yml)
stacks, environment variables, token generation, QR pairing, updates and backups.

The official image is available on
[Docker Hub](https://hub.docker.com/r/wppconnect/wppconnect-server-go):

```bash
docker run -d \
  --name wppconnect-server-go \
  --restart unless-stopped \
  -p 21465:21465 \
  -e SECRET_KEY=change-me \
  -e DATA_DIR=/data \
  -v wppconnect_go_data:/data \
  wppconnect/wppconnect-server-go:latest
```

For Docker Compose, clone the repository and run:

```bash
SECRET_KEY=change-me docker compose up -d
docker compose ps
curl http://localhost:21465/healthz
```

Set `WPP_SERVER_GO_TAG` to pin a release instead of following `latest`:

```bash
WPP_SERVER_GO_TAG=0.1 SECRET_KEY=change-me docker compose up -d
```

Available release tags follow the GitHub version: `vX.Y.Z`, `X.Y.Z`, `X.Y`,
`X`, and `latest`. Immutable `sha-*` tags are also published for every release.
See all tags on [Docker Hub](https://hub.docker.com/r/wppconnect/wppconnect-server-go/tags).

The container accepts `PORT`, `SECRET_KEY`, `WEBHOOK_URL`, and `DATA_DIR`.
This checkout also supports restoration/shutdown and webhook limit settings
listed in [`.env.example`](.env.example).
The Compose setup stores WhatsApp session data in the `wpp_go_data` volume, so
sessions survive container upgrades. Change the default secret before exposing
the API outside your machine.

To upgrade the Compose deployment:

```bash
docker compose pull
docker compose up -d
```

## Run from source

```bash
go run ./cmd/server
```

Then start a session and watch the logs for the QR (also delivered via webhook
as the `qrcode` event):

```bash
# In another terminal, use the same SECRET_KEY as the server.
export SECRET_KEY=THISISMYSECURETOKEN
TOKEN=$(curl -fsS -X POST "localhost:21465/api/mysession/$SECRET_KEY/generate-token" | jq -er .token)
curl -X POST localhost:21465/api/mysession/start-session -H "Authorization: Bearer $TOKEN"
curl localhost:21465/api/mysession/status-session -H "Authorization: Bearer $TOKEN"
```

Token generation returns HTTP 201 with `status`, `session`, `token` and `full`.
The new route is available in this checkout; published image `0.1.2` does not
include it. For that image, generate a raw bcrypt token locally with
`go run ./cmd/token -session mysession -raw` and send it in the Bearer header.
The helper requires `SECRET_KEY` in the environment. The session name plus
secret must fit in 72 bytes. See the Portainer guide for a complete example.

## whatsmeow fork (`wppconnect-team/whatsmeow`)

This project depends on whatsmeow through the WPPConnect fork, wired with a
`replace` directive in `go.mod`:

```
replace go.mau.fi/whatsmeow => github.com/wppconnect-team/whatsmeow main
```

The code still imports `go.mau.fi/whatsmeow` (idiomatic); Go redirects it to the
fork at build time. This lets collaborators **fix a bug in the fork, push to its
`main`, use it here immediately**, and then open a PR upstream to `tulir/whatsmeow`.

To pull a new fork revision after pushing to the fork's `main`:

```bash
go get go.mau.fi/whatsmeow@main   # resolves via the replace to the fork
go mod tidy
```

To temporarily build against upstream instead, comment out the `replace` line.

## Compatibility notes (Node/WPPConnect vs Go/whatsmeow)

Some WhatsApp-Web-only features have **no equivalent** in whatsmeow and will not
be ported (they return an explicit "not supported" once those routes exist):
stories/status, product catalog, interactive buttons/list templates, and
business-profile editing. Core messaging, groups, contacts, presence and media
are supported.

## Roadmap (MVP to parity)

1. ✅ Session + QR + send text + webhook
2. ✅ Media (image/file/audio) send & receive + authenticated download (this checkout)
3. ✅ Groups (create/list/participants/metadata/invites) + group events (this checkout)
4. ✅ Contacts, observed chats, presence/typing/recording (this checkout)
5. ✅ Auth: bcrypt token + `generate-token` + Node URL-safe/full formats (this checkout)
6. ✅ Session restoration/shutdown, bounded webhook retries, multiarch CI (this checkout)
7. ✅ OpenAPI, compatibility matrix, capability e2e tests and Node bcrypt fixtures (this checkout)

Docker/Portainer deployment guidance and complete stack examples are included.
See the [detailed roadmap](docs/roadmap.md) for acceptance criteria, remaining
implementation evidence and publication status. This PR does not publish a
release or validate against a real WhatsApp account.
