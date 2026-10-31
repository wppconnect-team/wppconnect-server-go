**English** | [Português (Brasil)](./README.pt-BR.md)

<p align="center">
  <a href="https://wppconnect.io/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://wppconnect-team.github.io/brand/wppconnect-dark.png" />
      <img src="https://wppconnect-team.github.io/brand/wppconnect-light.png" alt="WPPConnect" width="220" height="121" />
    </picture>
  </a>
</p>

# WPPConnect Server Go

Run a browser-free WhatsApp HTTP API built with Go and whatsmeow.

A project in the WPPConnect ecosystem, maintained by WPPConnect Team.

[Getting started](#getting-started) | [Documentation](#documentation) | [Contributing](#contributing) | [Support WPPConnect](#support-the-project)

[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE) [![CI](https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml) [![Documentation](https://img.shields.io/badge/docs-WPPConnect-blue)](https://wppconnect.io/) [![Open Collective](https://img.shields.io/badge/Open%20Collective-WPPConnect-blue)](https://opencollective.com/wppconnect)

## Overview

For integrators exploring a Go runtime and gradual migration from selected WPPConnect Server routes. Compatibility is defined by the documented contract, not complete equivalence with the Node.js Server. The source tree and older published images may expose different capabilities.

## Features

- Sessions, QR/phone pairing, session-bound tokens, text/media messages, contacts, groups and presence.
- SQLite persistence, webhooks, health/readiness checks and OpenAPI.
- Expanded source includes replay/adapters, private S3 media, metrics and an optional management UI. Buttons/lists are opt-in experimental; carousels return 501.

## Getting started

Requires **Go >=1.26.10**. SQLite uses a pure Go driver; normal builds need neither CGO nor a separate database. The following commands use Bash and OpenSSL:

```sh
git clone https://github.com/wppconnect-team/wppconnect-server-go.git
cd wppconnect-server-go
go mod download
export SECRET_KEY="$(openssl rand -hex 24)"
export DATA_DIR="$PWD/userDataDir"
go run ./cmd/server
```

Keep the secret for later requests. In another terminal `curl -fsS http://localhost:21465/healthz` checks health and `/readyz` checks readiness. `/openapi.json` exposes the contract. Use the [first-session guide](documentation/readme-reference.md#connect-your-first-session) to generate a token, start a session, fetch its QR and pair. Account operations need a dedicated authorized account.

## Configuration

Source execution reads environment variables directly and does not load `.env` automatically. See [.env.example](.env.example). Protect `SECRET_KEY` and the persistent `DATA_DIR`; SQLite is not a horizontally shared session store. Pin a confirmed published image tag/digest when using Docker, and preserve the data volume on upgrades. Prefer the offline `cmd/token` helper to keep bootstrap secrets out of URL logs. Native WhatsApp operations and newly composed features need their documented live validation; offline contract tests do not prove a real session.

## Documentation

- [API contract](./docs/openapi.json)
- [Compatibility matrix](./docs/compatibility.pt-BR.md)
- [Operation guide](./docs/operation.pt-BR.md)
- [Docker and Portainer](./docs/docker-portainer.pt-BR.md)
- [Expanded operations and roadmap](./docs/roadmap-completion.md)
- [Retained technical details and acknowledgments](documentation/readme-reference.md)

## WPPConnect ecosystem

| Project | Purpose | When to use it |
| --- | --- | --- |
| [whatsmeow](https://github.com/wppconnect-team/whatsmeow) | Use the WhatsApp multi-device protocol from Go applications. | Build a Go protocol integration with a pinned fork. |
| [WPPConnect Server](https://github.com/wppconnect-team/wppconnect-server) | Run a REST API for WhatsApp sessions, messages and webhooks. | Expose session and messaging operations over HTTP. |

## Support the project

WPPConnect is maintained by an open-source community. Your support helps fund maintenance, infrastructure, documentation, and the people working on the ecosystem.

If WPPConnect helps your project or business, consider making a one-time contribution or becoming a recurring supporter.

**[Support WPPConnect on Open Collective](https://opencollective.com/wppconnect)**

Contributions are voluntary. See the official page for current financial information. You can also help with code, tests, documentation, translations and well-described issue reports.

Additional official beneficiaries in [.github/FUNDING.yml](.github/FUNDING.yml): [joaosouz4dev](https://github.com/sponsors/joaosouz4dev), [manfe](https://github.com/sponsors/manfe).

## Contributing

Contributions of code, tests, examples, documentation and translations are welcome. For larger changes, discuss the approach in an issue first. Include the project/runtime versions, a minimal reproduction and sanitized logs when reporting a problem. Update both `README.md` and `README.pt-BR.md` in the same change when installation, configuration, features, examples, licensing or funding changes.

After installing the development dependencies, use the repository checks relevant to your change:

```sh
go test ./...
go build ./cmd/server ./cmd/token
```

Account-dependent examples and tests require separate validation with an authorized test account. Never attach credentials or session files to an issue.

## Community and support

Use [repository issues](https://github.com/wppconnect-team/wppconnect-server-go/issues) for reproducible bugs and feature proposals. For usage questions, join the community through the official channels:

- [Discord](https://discord.gg/JU5JGGKGNG)
- [Telegram](https://t.me/wppconnect)
- [YouTube](https://www.youtube.com/c/wppconnect)

This project is independent and is not affiliated with or endorsed by WhatsApp or Meta. Follow applicable terms and obtain authorization for account and messaging operations.

## License

[Apache License 2.0 (Apache-2.0)](./LICENSE).

Copyright 2026 WPPConnect Team and contributors.

The original legal document and existing copyright notices are preserved. Third-party components retain their own terms.

Dependencies retain their own terms. See [NOTICE](NOTICE) and [third-party licenses](third_party/licenses.txt), including the MPL-2.0 whatsmeow fork recorded in `go.mod`. No Evolution Go source is included, as recorded in NOTICE.
