# Server Go, Server Node e ZuckZapGo v2.4.2: comparação técnica e plano de evolução

Data da investigação: 10/11/2026. Escopo: tornar o Go o runtime único da WPPConnect Cloud e definir o trabalho necessário para aproximá-lo da cobertura de aplicação do ZuckZapGo. Este documento registra o estado das referências abaixo, antes das alterações locais de hardening desta tarefa.

## Decisão recomendada

Adotar `wppconnect-server-go` como único runtime oferecido para novas sessões Cloud é coerente com a direção do produto. A migração exige validação na API, no Manager e no worker, além de remover o seletor visual. Sessões existentes precisam manter sua identidade e seu volume; mudar somente o campo `provider` não converte credenciais de navegador em credenciais whatsmeow.

O Go já ultrapassou um MVP de envio: contém histórico observado, ações de mensagem, labels, comunidades, newsletters, outbox durável, replay, RabbitMQ/NATS, S3 privado, WebSocket, administração e Manager. Reimplementar essas bases seria desperdício. As maiores lacunas são o contrato de integração, o ciclo operacional e áreas funcionais específicas, principalmente comércio, interativos modernos e chamadas. [Código e guia do Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/docs/roadmap-completion.md).

Não há evidência para declarar os três produtos equivalentes ou atribuir um ganho numérico de memória, latência ou capacidade ao Go. Esta investigação não pareou contas nem executou o binário ZuckZapGo. A comparação da imagem é estática e verificável; funcionalidades de protocolo continuam dependendo de teste real.

## Referências fixadas e método

| Referência | Revisão usada | Como foi conferida |
| --- | --- | --- |
| WPPConnect Server Go | `main`, `052208a8818c942241d867d385d59c88df3792f9` | Branch principal consultada na API GitHub, `git fetch origin main`, código do checkout dedicado |
| WPPConnect Server Node | `main`, `37b7bc1a662b5262b8276597701f38c4dbcad58a` | API GitHub, fetch e exportação dessa revisão |
| Cloud Manager Worker | `main`, `3ca6010203f4bc5d4dcd759aade35c577551d081` | API GitHub, fetch e leitura de `src/service/docker.service.ts` |
| Documentação pública ZuckZapGo | `64a157a2fe47d0a4ec36c816382ba5d070701198` | Clone de `guilhermejansen/use-zuckzapgo`; manifesto declara versão `2.4.2`, mas fonte interna `c5d342a...` |
| Imagem solicitada | `setupautomatizado/zuckzapgo-private:v2.4.2` | Manifest OCI, config, camadas de arquivos estáticos, binário e entrypoint; sem iniciar container |

Os checkouts originais locais Go e Node não estavam na mesma revisão das respectivas principais. A comparação usa as revisões acima. Não confundir o branch local Node com `main`: aquele tinha trabalho adicional de providers/call media que não aparece na principal consultada.

Foram lidos roteadores, handlers, autenticação, persistência, despacho de eventos, configuração, Dockerfiles e testes relevantes do Go e do Node. No ZuckZapGo foram inspecionados contrato OpenAPI e guias **extraídos da imagem exata**, metadados OCI e informações de build do executável com `go version -m`. Isso inspeciona o arquivo, sem executá-lo. O repositório privado retornou HTTP 404 para a conta disponível; não houve auditoria do código interno dele.

O `graphify-out/GRAPH_REPORT.md` e o `graphify-out/wiki/index.md` exigidos pela instrução global não existem neste workspace. A análise de relações foi feita pelo código-fonte e contratos.

### Identidade da imagem

- Digest do índice multiarch: `sha256:b196150d19103cb821394c9c7f61dd593f3df50c9083a4e91068496180a18523`.
- Manifest Linux AMD64: `sha256:f073c6e57ed95b6dd9a75574c2e60bfef0b302243df1d07fc11260caa6381c1a`.
- Manifest Linux ARM64: `sha256:7bb462d48d6b016af2c441eb756f9f82ca88b02ed13d6404c338b03c851dc88c`.
- Labels: versão `v2.4.2`, revision `6463689b089cae2e58b3ca7cf8a6b0ce39880fd4`, licença `Commercial`, data `2026-10-16T19:45:46Z`.
- Runtime: Alpine 3.20.10, usuário `zuckzapgo`/UID 1001, porta 8080, ffmpeg e cliente PostgreSQL presentes nas instruções de construção.
- Camada estática conferida por SHA-256: `445c4ea20b712de535f422c65c38eedee66916a48a65eade41c991ea8ea7810e`.
- SHA-256 do `app/static/api/spec.yml` extraído: `2096ea73209320fb4489f3751fd563fd47bf9c00a6acb6191314a1cd814db7a7`.
- Camada do binário conferida: `2e1f8120de81a9df0e9c4f8f1183dfb57011ea6093ff1a76e9dca85599d63e4a`.
- Camada final de permissões conferida: `76db0c67e137b6578a5108ffe4099e9bc910341200893486ec379e9c9b19b688`. O spec e o binário dessa camada final são idênticos aos previamente extraídos; SHA-256 do binário final: `46bc5fdbd31ae140a53e4b8ce2812caee5630e76efbe8fefbfc9508fc0edee82`.

Fonte externa para reprodução: [imagem DockerHub](https://hub.docker.com/r/setupautomatizado/zuckzapgo-private), [release pública v2.4.2](https://github.com/guilhermejansen/use-zuckzapgo/releases/tag/v2.4.2). Os digests foram retornados pelo registry via `docker buildx imagetools inspect`, e os hashes de camada calculados localmente coincidem com o manifest.

Um detalhe decisivo: o binário declara Go `1.26.3` e dependência `github.com/polymorfa/hypermeow`, substituída por `./hypermeow-private (devel)`. Também declara `vcs.modified=true`. Logo, a imagem não é simplesmente um wrapper do whatsmeow público, e a revisão do label não prova uma construção integralmente reproduzível desse commit. Isso é uma limitação de rastreabilidade, não evidência de comprometimento. O Go WPPConnect usa seu fork fixado em `800567d5cf16`. [go.mod do Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/go.mod).

O buildinfo também confirma bibliotecas de SQS/S3 AWS SDK v2, Redis, pgx, SQLite, OpenTelemetry, Sentry, Pion/Opus e Tailscale `v1.98.8`. A presença delas reforça a arquitetura documentada, mas não comprova que cada recurso esteja habilitado ou funcione numa instalação específica.

### Divergência entre documentação pública e imagem

A documentação pública atual declara `2.4.2`, mas seu manifesto aponta para outra revisão interna e seu OpenAPI contém rotas posteriores ausentes no arquivo da imagem, como `/group/participant-operations` e `/group/history/originals`. Por isso, não se usou a documentação pública atual para inflar a cobertura da tag. [Manifesto público fixado](https://github.com/guilhermejansen/use-zuckzapgo/blob/64a157a2fe47d0a4ec36c816382ba5d070701198/.public-docs-manifest.json).

Mesmo dentro da imagem há guias com observações históricas e exemplos de versões anteriores. Contrato documentado, dependência compilada e comportamento validado são níveis de evidência diferentes.

## Tamanho da superfície de API

| Artefato contado | Paths distintos | Operações HTTP documentadas |
| --- | ---: | ---: |
| Go `docs/openapi.json` | 135 | 140 |
| Node `src/swagger.json` | 157 | 158 |
| ZuckZapGo `app/static/api/spec.yml`, da imagem | 270 | 303 |

Contagem por método e path, excluindo schemas e parâmetros. No Go, o contrato é gerado a partir das rotas registradas e inclui respostas explícitas 501. No Node, a contagem inclui documentação de rotas legadas, inclusive algumas que retornam 410. Não são números de funcionalidades comprovadas. [Geração do contrato Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/openapi.go), [Swagger Node](https://github.com/wppconnect-team/wppconnect-server/blob/37b7bc1a662b5262b8276597701f38c4dbcad58a/src/swagger.json).

Somente **81 pares método+path** aparecem nos dois documentos Go/Node. Há funcionalidades equivalentes com nomes diferentes e outras sem equivalente. A igualdade de path tampouco garante igualdade de payload, status, envelopes, paginação, IDs ou eventos. Portanto, um percentual de paridade calculado dividindo esses números seria enganoso.

Na imagem ZuckZapGo, as 303 operações se distribuem em: business 45; session 43; chat 41; call 36; admin 33; user 24; group 19; newsletter 18; community 9; connector 9; privacy 6; status 4; webhook 4; device 4; label 4; sync 3; health 1. O peso de comércio, chamadas e operação explica parte relevante da diferença.

## Matriz funcional

Legenda: **código** = implementado na referência de código inspecionada, sem promessa de validação ao vivo nesta tarefa; **contrato da imagem** = descrito nos arquivos da tag exata, sem execução; **anunciado** = apenas documentação comercial/pública.

| Domínio | Server Go | Server Node main | ZuckZapGo v2.4.2 | Consequência para Go |
| --- | --- | --- | --- | --- |
| Transporte WhatsApp | whatsmeow fork, sem navegador | WPPConnect/WhatsApp Web com Chromium | HyperMeow privado no binário | Adaptar capacidades no protocolo/SDK correto, não apenas copiar endpoints |
| Sessões e pairing | QR, phone pairing, start/close/logout, restore, tokens por sessão | Sessões de navegador, QR e gestão existente | Sessões, QR/phone, connector, passkey e import no contrato | QR/phone atendem base Cloud; connector seguro é próximo ganho de produto |
| Texto e mídia | Texto, imagem, documento, áudio/voz, vídeo MP4, sticker WebP, contato, localização, enquete | Conjunto amplo e recursos de conversão da stack Node | Texto/link, mídia, PTV, enquete, eventos e contact-request | Base existe; falta enriquecer formatos e pipeline de conversão |
| Ações de mensagem | Reply, reação, edição de texto próprio, revogação própria, recibos, voto | Reação, encaminhamento, favoritos, apagar, respostas e outros | Edit/reply/retry, pin/star/favorite, reações e outros | Priorizar encaminhar, favoritos/pin e contrato de recibos conforme demanda |
| Histórico | Arquivo SQLite observado, parcial, paginação por cursor e retenção | Consultas ao estado/histórico do navegador | Solicitações de sync/history e histórico completo no contrato | Não anunciar migração do histórico do navegador nem completude retroativa |
| Contatos e identidade | Contatos sincronizados, PN/LID, status/foto, consultas | Contatos e perfil expostos pelo navegador | Contato add/remove/info, LID mappings, username/identity-code | Faltam operações de administração de contatos e identidade |
| Grupos | Ampla base de grupos, participantes, convites, aprovação, foto | Ampla base via WPPConnect | Inclui histórico compartilhado, ephemeral e outros controles | Completar diferenças de política/histórico com teste de permissões |
| Comunidades | Criar, info, vincular/desvincular grupo | Administração e participantes via rotas próprias | Subgrupos, requests, announcement e administração | Cobertura Go é um subconjunto, payloads não são intercambiáveis |
| Newsletters | Criar/info/follow/unfollow/mute, subscribe, mensagens, envio de texto | Rotas de criação/edição/exclusão/mute | 18 operações incluindo mídia, reactions/views/admin-invite | Faltam mídia e operações administrativas relevantes |
| Perfil, privacidade e labels | Nome/status/foto, blocklist, privacy, labels e chat state | Perfil/business/labels/chat state | Contratos amplos, incluindo disappearing timer e business | Alinhar nomes e semântica antes de vender compatibilidade |
| Status | Texto e imagem/MP4 | Texto/imagem/vídeo | Texto/imagem/vídeo/áudio | Áudio e refinamento de audiência dependem do SDK |
| Botões/listas/carrosséis/flows | Botões/listas legacy experimentais; carrossel 501; sem flows | Botões/listas e recursos disponíveis no WPPConnect | Contrato de botões, listas, carousel e flow; recursos sujeitos a licença | Área de maior diferença perceptível, exige prova real por cliente/conta |
| Catálogo/pedidos/pagamentos | Não implementado na API | Produtos, coleções, pedidos, PIX e business | 45 operações business, incluindo catálogo e ciclo de pedido | Grande frente própria; não basta renomear rotas |
| Calls | Eventos seguros e reject-call | Não assumir o call-media do branch local como parte de main | Controle, áudio PCM, vídeo, grupo/link e streams no contrato | Projeto separado de mídia/VoIP, não uma correção pequena |
| Webhooks | Um destino global por processo, outbox/retry/replay | Destino por sessão, integração Chatwoot existente | Destinos/filtros por instância e global, fallback no contrato | Lacuna urgente de configuração e isolamento por tenant |
| Brokers | RabbitMQ com confirms e NATS Core | Não há a mesma arquitetura de outbox/brokers no main analisado | RabbitMQ, SQS, Redis Streams e WebSocket documentados | Não falta criar outbox; faltam adapters e controles operacionais |
| S3 | Arquivamento explícito e privado, quota/TTL/ownership/hash | Mídia e armazenamento da implementação Node | Global e por sessão, upload automático e URL assinada documentados | Automatizar ingestão de mídia mantendo isolamento e privacidade |
| Operação/UI | Manager simples, métricas básicas, capabilities, SQLite local | Manager completo da stack Node | Dashboard, usuários, eventos, DLQ/archive/config/test no contrato | Elevar experiência operacional e visibilidade de falhas |
| Integrações externas | Sem adaptador Chatwoot dedicado | Chatwoot com autenticação e destinos permitidos | Chatwoot/n8n anunciados; não há grupo de endpoints Chatwoot no spec extraído | Definir integração concreta e testes, sem inferir profundidade pela propaganda |

Fontes Go por domínio: [rotas base](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/router.go), [mensagens avançadas](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/advanced_messages.go), [ações](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/message_actions.go), [grupos](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/groups.go), [canais/comunidades/calls](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/channels.go), [interativos/status](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/status_interactive.go). Fontes Node: [roteador](https://github.com/wppconnect-team/wppconnect-server/blob/37b7bc1a662b5262b8276597701f38c4dbcad58a/src/routes/index.ts), [Chatwoot](https://github.com/wppconnect-team/wppconnect-server/blob/37b7bc1a662b5262b8276597701f38c4dbcad58a/src/util/chatWootClient.ts). ZuckZapGo: spec e guias da camada identificada acima; [anúncio público de integrações](https://github.com/guilhermejansen/use-zuckzapgo/blob/64a157a2fe47d0a4ec36c816382ba5d070701198/README.md).

## Achados que afetam a Cloud imediatamente

### P0: webhook aceito na criação, mas não aplicado ao Go

No worker principal, `startRuntimeSession` envia `params.webhook` no JSON de `/api/{session}/start-session`. Porém, `startSessionWppRuntime` cria o container sem `WEBHOOK_URL`. Há ainda uma camada anterior: `/api/startSession` retira um container já pronto com `removeOldestContainer` e chama `activatePooledSession(start)`, sem encaminhar as opções de webhook do pedido. Esse pool foi criado antes de se conhecer a configuração do cliente. O handler Go `startSession` não decodifica o body: chama `mgr.Start` diretamente. O dispatcher Go recebe somente `cfg.WebhookURL` na inicialização. Resultado: a criação pode responder sucesso e a sessão conectar, mas o webhook informado nunca é configurado.

Evidência: [entrada e consumo do pool](https://github.com/joaosouz4dev/wppconnect-cloud-manager-worker/blob/3ca6010203f4bc5d4dcd759aade35c577551d081/src/index.ts), [worker fixado, ativação e provisionamento](https://github.com/joaosouz4dev/wppconnect-cloud-manager-worker/blob/3ca6010203f4bc5d4dcd759aade35c577551d081/src/service/docker.service.ts), [handler Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/router.go#L267), [wiring do dispatcher](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/cmd/server/main.go).

Correção inicial para o desenho atual de um container por sessão: encaminhar e normalizar `webhookUrl`/`webhook` até o worker, depois aplicar configuração persistida durante a ativação do pool. Uma alternativa é provisionar container novo com `WEBHOOK_URL` validado quando houver configuração específica, em vez de consumir um container genérico. Alterar somente o array Env de `startSessionWppRuntime` não corrige o caminho de ativação do pool. Testar pedido público, alocação, evento no destino e restauração após restart. Para múltiplas sessões no mesmo processo, criar configuração por sessão com segredo, filtros e política de destino. Um parâmetro não suportado deve produzir erro explícito, nunca sucesso silencioso. Não implementar destino arbitrário controlado pelo cliente sem política SSRF e isolamento.

### P0: garantir Go em todos os pontos de entrada

Remover o seletor não impede chamadas diretas. Criação, duplicação, proxy de `start-session`, mudanças de configuração e jobs devem rejeitar runtime/provider divergente antes de alocar recursos. O Manager deve validar o retorno do worker para evitar registrar uma sessão de outro runtime como Go. O worker deve usar imagem Go fixada e labels coerentes.

Clientes antigos precisam receber código estável, como `PROVIDER_NOT_AVAILABLE`, e mensagem explícita. A regra é específica da origem Cloud; fluxos de outros produtos no mesmo Manager/worker devem conservar seus contratos. Inventariar sessões existentes por runtime e evitar conversão silenciosa de volume/token. Esse é um requisito derivado da arquitetura de provisionamento acima, não uma capacidade comprovada do runtime sozinho.

### P0: o contrato Node não é substituível apenas pela URL

Exemplos observados de diferenças:

| Operação desejada | Contrato Node | Contrato Go |
| --- | --- | --- |
| Reagir | `react-message` | `send-reaction` |
| Listas | `send-list-message` | `send-list`, experimental, payload diferente |
| Nome do perfil | `change-username` | `set-profile-name` |
| Status do perfil | `profile-status` | `set-profile-status` |
| Foto do perfil | `set-profile-pic` | `set-profile-picture` |
| Labels | `get-all-labels`, `add-new-label`, `add-or-remove-label` | `labels`, `edit-label`, `label-chat`, `label-message` |
| Comunidades | `add-community-subgroup`, `remove-community-subgroup` | `link-community-group`, `unlink-community-group` |
| Mídia | Caminhos/URLs e conversão conforme rota Node | Base64/data URL ou multipart, mídia pré-formatada, até 16 MiB |
| Históricos | Respostas do estado de navegador | Arquivo observado, cursor, `isPartial=true` |

Usar testes de contrato orientados às operações realmente chamadas pela Cloud e pelos SDKs. Para cada alias, conferir schema, envelope, status, JID, efeitos e erros. Não fazer proxy genérico supondo que nomes parecidos sejam equivalentes. [Contrato Go estendido](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/openapi_extended.go), [contrato Node](https://github.com/wppconnect-team/wppconnect-server/blob/37b7bc1a662b5262b8276597701f38c4dbcad58a/src/swagger.json).

## Confiabilidade, segurança e escala

### O que o Go já faz bem

O log de eventos persiste corpo e tentativas por destino antes do envio, usa leases recuperáveis e confirmações RabbitMQ. Retry diferencia erros transitórios, respeita `Retry-After` dentro de limites, e replay preserva o identificador. O arquivo de histórico isola sessão/chat, limita retenção e paginação e não devolve protobuf/chaves de mídia. S3 é privado e downloads verificam ownership e SHA-256. A interface administrativa usa cookie HttpOnly/SameSite, CSRF e limite de tentativas de login. Esses são ativos a preservar. [Outbox](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/eventbus/store.go), [transports](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/eventbus/targets.go), [histórico](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/history/store.go), [vault](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/vault/vault.go), [admin](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/admin.go).

### Lacunas operacionais concretas

1. **Assinatura de webhook.** Na referência, o transporte HTTP envia ID/tentativa, mas não assinatura autenticada. Acrescentar segredo independente, HMAC-SHA256, timestamp, definição canônica e orientação de deduplicação. Consumidores devem autenticar o corpo original e limitar janela de replay. Alteração opcional preserva consumidores antigos. [HTTP target](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/eventbus/targets.go).
2. **Exaustão e perdas antes da persistência.** Pending não é descartado para abrir espaço; quando o log enche, `Publish` falha e o callback registra erro/contador. A garantia começa depois da persistência, não na chegada de todo evento do WhatsApp. Expor ocupação/idade do backlog/rejeições por destino, alertar antes da saturação e definir política de admissão e recuperação. [Bus.Publish/post/worker](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/eventbus/bus.go).
3. **DLQ e operação de entregas.** Há estado failed e replay, mas faltam filtros e investigação por destino, motivo seguro da falha, replay em lote, quotas e auditoria de operador comparáveis aos grupos `/admin/dlq` e `/admin/archive` documentados na imagem. Evitar replay cego que gere duplicação.
4. **Idempotência no envio.** Não há chave idempotente de API persistida para o comando de envio. O próprio handler informa que timeout pode deixar entrega desconhecida. Persistir intenção/resultado por sessão+chave, com fingerprint do payload e resposta recuperável; nunca prometer exactly-once no protocolo. [sendError](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/advanced_messages.go).
5. **Autenticação e orçamento de CPU.** `auth.Verify` executa `bcrypt.CompareHashAndPassword` sobre hash fornecido na requisição. Não foi localizado limite de custo do hash nem gate de concorrência antes dessa operação; o limitador de login admin não cobre esse caminho. Rever limite de custo aceitável, cache limitado de tokens válidos, rate limiting e duração/cancelamento. É risco por inspeção, sem ensaio de negação de serviço. A concatenação sessão+segredo também tem limite de 72 bytes na geração. [Tokens Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/auth/token.go).
6. **Segredos e rotação.** Bootstrap legado põe o segredo na URL; preferir helper offline e futuro bootstrap autenticado por header. Tokens bcrypt derivados do segredo não têm registro individual de expiração/revogação. Planejar tokens de API por workspace/sessão, hash em armazenamento, scopes e rotação sem derrubar todo o parque. Não trocar o esquema sem compatibilidade/migração explícita.
7. **Container e HTTP.** O Dockerfile Go da referência não declara `USER`, enquanto a imagem Zuck declara usuário sem root. Introduzir UID/GID estável com migração de permissão dos volumes, filesystem somente leitura onde possível e limites de recursos. O servidor Go define ReadHeaderTimeout/IdleTimeout, mas deve ter política testada para corpos lentos e operações longas, preservando WebSockets. [Dockerfile Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/Dockerfile), [servidor HTTP](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/cmd/server/main.go).
8. **Observabilidade.** `/metrics` traz sessões e estados de entrega, mas faltam histogramas HTTP, latência até ACK, reconexão, backlog age, uso de mídia/banco, versão/SHA e tracing correlacionado. Não colocar JIDs, conteúdo ou tokens como labels. Zuck possui dependências OTEL/Sentry compiladas e documentação de tracing, sem benchmark de custo verificado aqui. [Métricas Go](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/admin.go#L275).
9. **Escala.** O Go limita 100 sessões vivas por processo; mediaSlots=4 e streams=32 são limites globais. SQLite/outbox são desenhados para um processo, e o banco usa uma conexão por store. Não compartilhar um volume entre réplicas. Sharding por sessão com afinidade é o passo natural para a Cloud atual. PostgreSQL, se adotado, precisa de ownership/leases/fencing para evitar dois clientes na mesma sessão; mudar só o driver não resolve HA. [Manager](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/session/manager.go), [router](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/internal/httpapi/router.go).
10. **Isolamento de integrações.** Destinos globais recebem eventos de todas as sessões do processo. Para um container por sessão isso é administrável; para consolidar tenants é obrigatório roteamento/segredo/filtros/quota por sessão. Não copiar o envio de `userToken` em eventos globais documentado pelo concorrente nem tokens permanentes em query string.

### Diferenças de aplicação que exigem módulos novos

- **Connector:** fluxo embutível para QR/phone, token curto com escopo de pairing, expiração e origem permitida. Passkey é uma extensão do protocolo, não requisito para concluir o Go-only inicial.
- **Chatwoot/n8n:** adaptador de domínio para contato/conversa/mensagem/mídia/status, deduplicação de ecos, mapeamento PN/LID, retry e autenticação. Um webhook genérico é infraestrutura, não equivale a integração pronta.
- **Mídia:** download automático autorizado + job de armazenamento, conversão ffmpeg isolada e limitada, conteúdo pré-validado, descarte de temporários, objeto privado e link temporário autorizado quando necessário. Manter a semântica atual segura como default.
- **SQS/Redis Streams:** implementar sobre `eventbus.Target`, aproveitando persistência e retry existentes. FIFO exige chave por sessão/chat, deduplicação, contrato de ordering e testes de falha; NATS Core atual não equivale a JetStream durável.
- **Comércio:** começar com leitura de perfil/catalog/produtos, depois mutações, envio de produtos e pedidos/pagamentos. Modelar dinheiro, moeda, estado e idempotência; não tratar resposta de protocolo como confirmação de pagamento.
- **Interativos modernos:** verificar capacidade do fork, compor mensagens e validar em Android/iOS/Web, conta comum/business e grupos/privado. Manter feature flags/capabilities e 501 enquanto não suportado. Licença premium do concorrente não é prova de entrega universal.
- **Chamadas:** engine de sinalização/mídia, codecs, streams autenticados, controle de recursos e UX de áudio; testar rede/UDP, NAT, consentimento operacional e cancelamento. Os guias da própria imagem listam lacunas históricas de áudio/browser, portanto nem o seu contrato basta para provar qualidade ponta a ponta.
- **Rota mobile:** Zuck documenta Tailscale/tsnet por sessão com versionamento/fail-closed e bloqueio de calls em rota TCP-only. É uma capacidade de infraestrutura opcional, separada do proxy HTTP/SOCKS existente no Go. Só priorizar se houver necessidade de produto demonstrada.

### Melhorias menores já apoiadas pelo SDK fixado

Uma inspeção adicional do fork `800567d5cf16` separa trabalho de exposição HTTP de trabalho de protocolo novo. Existem métodos `NewsletterMarkViewed`, `NewsletterSendReaction` e `GetNewsletterMessageUpdates`, além de `GetBusinessProfile`, `GetContactQRLink`, `GetUserDevices`, `SetDisappearingTimer` e o builder `appstate.BuildStar`. São bons candidatos a pequenas entregas com autorização, validação, OpenAPI e testes de contrato, seguidos de validação real. [Newsletter SDK](https://github.com/wppconnect-team/whatsmeow/blob/800567d5cf16/newsletter.go), [usuários/perfil](https://github.com/wppconnect-team/whatsmeow/blob/800567d5cf16/user.go), [timer](https://github.com/wppconnect-team/whatsmeow/blob/800567d5cf16/send.go), [appstate](https://github.com/wppconnect-team/whatsmeow/blob/800567d5cf16/appstate/encode.go).

Isso não implica que catálogo CRUD, flows ou VoIP completo estejam prontos nesse fork. O módulo `business.go` consultado expõe `GetOrderDetails`; ampliar comércio requer investigar métodos/protobuf/permissões e não presumir equivalência com o HyperMeow privado. [Business SDK](https://github.com/wppconnect-team/whatsmeow/blob/800567d5cf16/business.go).

## Plano de implementação com critérios de aceite

| Ordem | Entrega | Critério objetivo |
| --- | --- | --- |
| P0.1 | Go-only no catálogo/UI/API/Manager/worker | Nenhuma criação Cloud consegue selecionar Node/outro provider; payload forjado é rejeitado antes de container; defaults sempre Go; outros produtos preservados |
| P0.2 | Corrigir webhook na ativação do pool/provisionamento Go | Normalizar webhookUrl/webhook, criar pela API pública usando pool, receber no destino, reiniciar e receber novamente; testar também container novo; URL inválida rejeitada |
| P0.3 | Segurança de webhook e auth | HMAC validado sobre bytes originais, timestamp/ID íntegros, rotação documentada; custo/concurrency bcrypt limitados e regressão de compatibilidade |
| P0.4 | Contrato Cloud mínimo executável | Testes reais de QR/phone, conexão/restauração, texto/mídia, recebimento, ACK, logout e isolamento; nenhuma dependência silenciosa de rota Node |
| P1.1 | Webhook/filtros por sessão e DLQ operacional | Dois tenants não recebem eventos um do outro; replay auditado; endpoint indisponível não bloqueia outros destinos |
| P1.2 | Idempotência e fila de comandos de envio | Repetição da mesma chave recupera resultado; payload divergente retorna conflito; crash/timeout não dispara reenvio automático ambíguo |
| P1.3 | Observabilidade, backup/restore e imagem sem root | Dashboard de backlog/erros/latência, restore consistente de bancos+objetos, teste de volume antigo, SHA/versionamento verificável |
| P1.4 | Integração Chatwoot e fluxo embutível de pairing | Texto/mídia/recibos nos dois sentidos, sem loop, PN/LID correto, token curto e isolamento |
| P2.1 | Mídia automática e SQS/Redis Streams | Retentativa e recuperação após restart com brokers reais; permissões/TTL S3; limites de CPU/memória da conversão |
| P2.2 | Completar rotas úteis Node | Matriz de aliases com schemas e fixtures; casos negativos; funcionalidades sem equivalente explicitamente indisponíveis |
| P3 | Interativos, newsletters avançadas e comércio | Matriz de entrega real, permissões, rollback local e erros parciais; capabilities refletem runtime/configuração |
| P4 | Calls, passkey/import e rota mobile | Provas específicas de protocolo, rede e mídia; módulos independentes e sem afetar estabilidade de mensagens |

Não é recomendável tentar implementar simultaneamente todas as 303 operações do concorrente. Priorizar jornadas comercializadas pela Cloud, confiabilidade e observabilidade antes de ampliar superfície.

### Alterações locais preparadas nesta tarefa

Além desta pesquisa, foram preparadas alterações de Go-only na Cloud e no Manager e assinatura HMAC opcional no transporte de webhook Go, com configuração e documentação. Essas alterações locais não fazem parte dos SHAs de referência analisados. A política completa P0.1 ainda depende da conferência do worker e da implantação; P0.2 permanece pendente, pois o worker não foi alterado nesta etapa. O HMAC protege autenticidade quando há destino configurado, mas não resolve a ausência de configuração do webhook no pool. Resultados de testes/builds das mudanças são registrados na entrega da implementação, separados desta comparação estática.

## Validação necessária para poder dizer que ficou equiparado

1. **Contrato:** comparar schemas/retornos/eventos por jornada, não por contagem de endpoints. Usar golden fixtures sanitizadas e dois tenants para isolamento.
2. **Protocolo real:** contas de teste autorizadas, contatos PN/LID, privado/grupo/comunidade, Android/iOS/Web e account types relevantes. Registrar versões, resultados e limitações.
3. **Falhas:** queda do processo durante persistência/envio, volume cheio, 429/503/timeouts, indisponibilidade de broker/S3 e restart. Medir perda, duplicação, duração até recuperação e resultado ambíguo.
4. **Carga controlada:** mesmo hardware/limites, mesmas sessões e mix texto/mídia, rampas 1/10/50/100 sessões, aquecimento e soak. Medir RSS, CPU, disco/WAL, goroutines, p50/p95/p99, backlog, erros e reconexão. Separar HTTP aceito, ACK do protocolo e entrega ao destinatário.
5. **Release:** imagem imutável, SBOM/SHA, version/capabilities, canário, rollback de imagem e compatibilidade de volume. SQLite com WAL exige backup consistente, não cópia casual de arquivo aberto.

O repositório Go já possui CI com testes race/vet, brokers e MinIO reais, além de smoke multiarch. A existência do workflow não significa que ele tenha sido executado ou aprovado nesta investigação. [CI fixado](https://github.com/wppconnect-team/wppconnect-server-go/blob/052208a8818c942241d867d385d59c88df3792f9/.github/workflows/ci.yml). Não foram executados benchmarks nem testes ao vivo do WhatsApp como parte desta pesquisa.

## Evidências locais e reprodução

Os arquivos de auditoria ficaram em `artifacts/server-go-parity-20261110/`, fora do código de produção:

- `zuck-v242-manifest.json`, `zuck-v242-image-config.json`: manifest/config OCI.
- `zuck-v242-static.tar.gz`, `zuck-v242-binary.tar.gz`, `zuck-v242-entrypoint.tar.gz`, `zuck-v242-final.tar.gz`: camadas verificadas; não executadas.
- `image-static/app/static/api/spec.yml` e `image-static/app/static/docs/`: contrato/guias da imagem.
- `zuck-v242-buildinfo.txt`: resultado de `go version -m` no executável extraído.
- `node-operations.txt`, `go-operations.txt`, `zuck-v242-operations.txt`: inventário de método/path.
- `zuckzapgo-docs/`: clone público fixado para contraste; não é o código privado do produto.

Comandos principais de reprodução:

```text
docker buildx imagetools inspect setupautomatizado/zuckzapgo-private:v2.4.2
docker buildx imagetools inspect setupautomatizado/zuckzapgo-private:v2.4.2 --format '{{json .Image}}'
go version -m <arquivo-extraido>/app/zuckzapgo
```

Para repetir a extração, obter token anônimo de pull no registry Docker, resolver o manifest AMD64 por digest, baixar blobs por seus digests, conferir SHA-256 e extrair somente os arquivos desejados. Não iniciar o entrypoint nem o binário para obter documentação. Os artefatos comerciais baixados são evidência local de análise e não devem ser incorporados à distribuição WPPConnect. Implementações futuras devem partir dos contratos e do código/dependências com autorização apropriada, sem copiar internals privados.
