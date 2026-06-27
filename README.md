# wppconnect-server-go

A Go port of [wppconnect-server](https://github.com/wppconnect-team/wppconnect-server),
backed by [whatsmeow](https://github.com/tulir/whatsmeow) instead of a browser
(Puppeteer). It keeps the **HTTP contract compatible** with the Node server —
same routes and payload field names — so existing clients can migrate with
minimal changes.

> **Status: MVP scaffold.** Session lifecycle, QR, send-message and webhooks are
> wired. The full ~148-route surface is being filled in incrementally.

## Why a Go version

- **No browser.** whatsmeow speaks the WhatsApp multidevice protocol directly —
  far lower memory/CPU than a Chromium-per-session model.
- **Single binary.** Trivial to deploy and to manage from the `wppconnect-manager`.

## Architecture

```
cmd/server            entrypoint
internal/config       env-based config (PORT, SECRET_KEY, WEBHOOK_URL, DATA_DIR)
internal/session      SessionManager over whatsmeow (SQLite store)
internal/webhook      Dispatcher — posts normalized events (event + data)
internal/httpapi      chi router with Node-compatible routes
```

## Endpoints (MVP)

| Method | Path                                          | Node equivalent |
| ------ | --------------------------------------------- | --------------- |
| POST   | `/api/{session}/start-session`                | same            |
| GET    | `/api/{session}/status-session`               | same            |
| POST   | `/api/{session}/send-message`                 | same            |
| POST   | `/api/{session}/send-image`                   | same (base64)   |
| POST   | `/api/{session}/send-seen`                    | same            |
| GET    | `/api/{session}/check-number-status/{phone}`  | same            |
| GET    | `/api/{session}/all-groups`                   | same            |
| POST   | `/api/{session}/close-session`                | same            |
| GET    | `/healthz`                                     | same            |

Payloads match the Node server: `send-message` takes `phone` (string or array),
`message`, `isGroup`; `send-image` takes `phone`, `base64` (raw or data URL),
`caption`, `isGroup`; `send-seen` takes `phone`.

## Run

```bash
go run ./cmd/server
```

## Docker

The official image is published to Docker Hub as
`wppconnect/wppconnect-server-go`.

Run the production compose example:

```bash
SECRET_KEY=change-me docker compose up -d
curl http://localhost:21465/healthz
```

Use `WPP_SERVER_GO_TAG` to pin a specific image tag:

```bash
WPP_SERVER_GO_TAG=0.1.0 SECRET_KEY=change-me docker compose up -d
```

The container is configured through `PORT`, `SECRET_KEY`, `WEBHOOK_URL`, and
`DATA_DIR`. The compose example persists `DATA_DIR` at `/data`.

Docker image publishing is handled by GitHub Actions:

- pushes to `main` publish the moving `main` tag and a `sha-*` tag;
- pushes to `develop` publish the moving `develop` tag and a `sha-*` tag;
- tags like `v0.1.0` publish `0.1.0`, `0.1`, `0`, the git ref tag, `sha-*`,
  and `latest`;
- pull requests build the image without pushing it.

The Docker Hub organization must contain a public repository named
`wppconnect-server-go`, and this GitHub repository must have the secrets
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.

Then start a session and watch the logs for the QR (also delivered via webhook
as the `qrcode` event):

```bash
curl -X POST localhost:21465/api/mysession/start-session
curl localhost:21465/api/mysession/status-session    # contains urlcode to render
```

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

## Roadmap (MVP → parity)

1. ✅ Session + QR + send text + webhook
2. Media (image/file/audio) send & receive + download
3. Groups (create/list/participants) + group events
4. Contacts, chats, presence/typing
5. Auth parity (bcrypt token like the Node server) + `generate-token`
6. OpenAPI spec + e2e tests
