[English](./README.md) | **Português (Brasil)**

<p align="center">
  <a href="https://wppconnect.io/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://wppconnect-team.github.io/brand/wppconnect-dark.png" />
      <img src="https://wppconnect-team.github.io/brand/wppconnect-light.png" alt="WPPConnect" width="220" height="121" />
    </picture>
  </a>
</p>

# WPPConnect Server Go

Execute uma API HTTP do WhatsApp sem navegador, feita com Go e whatsmeow.

Um projeto do ecossistema WPPConnect, mantido pela WPPConnect Team.

[Primeiros passos](#primeiros-passos) | [Documentação](#documentação) | [Como contribuir](#como-contribuir) | [Apoie a WPPConnect](#apoie-o-projeto)

[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE) [![CI](https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/wppconnect-team/wppconnect-server-go/actions/workflows/ci.yml) [![Documentation](https://img.shields.io/badge/docs-WPPConnect-blue)](https://wppconnect.io/) [![Open Collective](https://img.shields.io/badge/Open%20Collective-WPPConnect-blue)](https://opencollective.com/wppconnect)

## Visão geral

Para integradores que exploram um runtime Go e a migração gradual de rotas selecionadas do WPPConnect Server. A compatibilidade é definida pelo contrato documentado, sem equivalência completa com o Server Node.js. Os fontes e imagens publicadas mais antigas podem oferecer recursos diferentes.

## Funcionalidades

- Sessões, pareamento por QR/telefone, tokens vinculados à sessão, mensagens de texto/mídia, contatos, grupos e presença.
- Persistência SQLite, webhooks, verificações de saúde/prontidão e OpenAPI.
- Os fontes ampliados incluem replay/adaptadores, mídia S3 privada, métricas e interface administrativa opcional. Botões/listas são experimentais e exigem opt-in; carrosséis retornam 501.

## Primeiros passos

Exige **Go >=1.26.10**. SQLite usa um driver Go puro; builds normais dispensam CGO e banco externo. Os comandos usam Bash e OpenSSL:

```sh
git clone https://github.com/wppconnect-team/wppconnect-server-go.git
cd wppconnect-server-go
go mod download
export SECRET_KEY="$(openssl rand -hex 24)"
export DATA_DIR="$PWD/userDataDir"
go run ./cmd/server
```

Guarde o segredo para as próximas requisições. Em outro terminal `curl -fsS http://localhost:21465/healthz` consulta a saúde e `/readyz` a prontidão. `/openapi.json` expõe o contrato. Use o [guia de operação em português](docs/operation.pt-BR.md) para gerar token, iniciar sessão, obter QR e parear. Operações de conta exigem uma conta dedicada e autorizada.

## Configuração

A execução pelos fontes lê diretamente as variáveis de ambiente e não carrega `.env` automaticamente. Consulte [.env.example](.env.example). Proteja `SECRET_KEY` e o `DATA_DIR` persistente; SQLite não é um store de sessões compartilhado horizontalmente. No Docker, fixe uma tag/digest cuja publicação foi confirmada e preserve o volume ao atualizar. Prefira o helper offline `cmd/token` para evitar segredos de bootstrap nos logs de URL. Operações nativas e recursos recém-compostos exigem a validação real documentada; testes offline de contrato não comprovam uma sessão real.

## Documentação

- [Contrato da API](./docs/openapi.json)
- [Matriz de compatibilidade](./docs/compatibility.pt-BR.md)
- [Guia de operação](./docs/operation.pt-BR.md)
- [Docker e Portainer](./docs/docker-portainer.pt-BR.md)
- [Operações ampliadas e roadmap](./docs/roadmap-completion.md)
- [Detalhes técnicos e agradecimentos preservados](documentation/readme-reference.md)

## Ecossistema WPPConnect

| Projeto | Finalidade | Quando usar |
| --- | --- | --- |
| [whatsmeow](https://github.com/wppconnect-team/whatsmeow) | Use o protocolo multidispositivo do WhatsApp em aplicações Go. | Criar integração de protocolo Go com fork fixado. |
| [WPPConnect Server](https://github.com/wppconnect-team/wppconnect-server) | Execute uma API REST para sessões, mensagens e webhooks do WhatsApp. | Expor operações de sessão e mensagens por HTTP. |

## Apoie o projeto

A WPPConnect é mantida por uma comunidade de código aberto. Seu apoio ajuda a financiar a manutenção, a infraestrutura, a documentação e o trabalho das pessoas que desenvolvem o ecossistema.

Se a WPPConnect ajuda seu projeto ou sua empresa, considere fazer uma contribuição pontual ou apoiar de forma recorrente.

**[Apoie a WPPConnect no Open Collective](https://opencollective.com/wppconnect)**

As contribuições são voluntárias. Consulte a página oficial para acompanhar as informações financeiras atuais. Você também pode ajudar com código, testes, documentação, traduções e relatos de problemas bem descritos.

Beneficiários oficiais adicionais em [.github/FUNDING.yml](.github/FUNDING.yml): [joaosouz4dev](https://github.com/sponsors/joaosouz4dev), [manfe](https://github.com/sponsors/manfe).

## Como contribuir

Contribuições de código, testes, exemplos, documentação e traduções são bem-vindas. Para mudanças maiores, discuta a proposta em uma issue primeiro. Ao relatar um problema, informe as versões do projeto/runtime, uma reprodução mínima e logs sem dados sensíveis. Atualize `README.md` e `README.pt-BR.md` na mesma alteração quando mudar instalação, configuração, funcionalidades, exemplos, licença ou arrecadação.

Após instalar as dependências de desenvolvimento, use as verificações pertinentes à sua alteração:

```sh
go test ./...
go build ./cmd/server ./cmd/token
```

Exemplos e testes que dependem de conta exigem validação separada com uma conta de teste autorizada. Nunca anexe credenciais ou arquivos de sessão a uma issue.

## Comunidade e suporte

Use as [issues do repositório](https://github.com/wppconnect-team/wppconnect-server-go/issues) para bugs reproduzíveis e propostas de funcionalidades. Para dúvidas de uso, participe pelos canais oficiais:

- [Discord](https://discord.gg/JU5JGGKGNG)
- [Telegram](https://t.me/wppconnect)
- [YouTube](https://www.youtube.com/c/wppconnect)

Este projeto é independente e não é afiliado nem endossado pelo WhatsApp ou pela Meta. Siga os termos aplicáveis e obtenha autorização para operações de conta e mensagens.

## Licença

[Apache License 2.0 (Apache-2.0)](./LICENSE).

Copyright 2026 WPPConnect Team and contributors.

O documento jurídico original e os avisos de autoria existentes são preservados. Componentes de terceiros mantêm seus próprios termos.

As dependências mantêm seus próprios termos. Consulte [NOTICE](NOTICE) e as [licenças de terceiros](third_party/licenses.txt), incluindo o fork whatsmeow MPL-2.0 registrado em `go.mod`. Conforme NOTICE, nenhum fonte do Evolution Go está incluído.
