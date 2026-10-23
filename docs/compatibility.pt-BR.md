# Matriz de compatibilidade e validação

> Source expansion update: the historical baseline below is superseded for
> message history, labels/community filters, durable events and administration
> by the [current English guide](roadmap-completion.md). New capabilities require
> a build containing this change. Experimental/unsupported features remain explicit.


Esta matriz descreve a base publicada em `0.1.3` e as adições no código-fonte,
identificadas abaixo, sem afirmar paridade com todas as rotas
ou estruturas internas do servidor Node. A referência de contrato é o checkout
`wppconnect-server` v2.10.27 e o fork whatsmeow fixado no `go.mod`.

| Capacidade | Cobertura Go | Diferenças e limites | Evidência automatizada |
| --- | --- | --- | --- |
| Autenticação | bcrypt, generate-token, Bearer bruto/URL-safe e session:token. | Sessão + segredo até 72 bytes para geração; tokens vinculados à sessão. | HTTP de bootstrap/autorização/isolamento; fixtures Node bcrypt 6.0.0 com Unicode e 72 bytes. |
| Sessões | Início, status, QR, desconexão, logout, restauração e shutdown. | Nomes ASCII seguros de até 64 caracteres; no máximo 100 sessões vivas. | SQLite real, reabertura, legado, falha de logout, cancelamento e concorrência. |
| Texto e leitura | Envio, consulta de número e rota send-seen do MVP. | Sem promessa de sincronização completa da caixa de entrada ou de todos os campos de retorno do Node. | Contrato de autenticação e documentação; sem conta WhatsApp real nesta entrega. |
| Mídia base | Imagem, arquivo, áudio/voz, normalização e download autorizado. | 16 MiB; cache limitado; sem transcodificação ou leitura arbitrária de caminhos/URLs. | HTTP de envio/download, MIME/limites/isolamento, transporte simulado e descriptografia do SDK. |
| Mensagens avançadas (código-fonte, fora de 0.1.3) | Vídeo MP4, stickers WebP estáticos/animados, vCards, localização e criação de enquetes; eventos e download de vídeo/sticker. | [Contrato completo](advanced-messages.md): vídeo/contatos explícitos são extensões Go; contact-vcard retorna array; send-sticker-gif recebe WebP animado já preparado. Sem conversão, localização ao vivo ou votos/resultados. | HTTP, limites de contêiner, segredos por destinatário, resultados parciais, OpenAPI/TCP, webhook, isolamento e descriptografia real do SDK. |
| Grupos | 22 rotas, participantes/admins, metadados, convites, entrada/saída e foto. | Aprovação de entrada, erros por participante e resultados parciais preservados. | HTTP de sucesso, permissões, `207`, falhas, imagem e webhook. |
| Contatos e perfil | Contatos sincronizados, associação PN/LID e consulta de status/foto. | Associação local e campos disponíveis no SDK; privacidade pode impedir consulta. | HTTP, alias PN/LID e falhas de protocolo simuladas. |
| Chats | Metadados persistentes, filtros/paginação e eventos de app state. | Máximo 10.000 chats, 64 IDs recentes por chat; sem corpos históricos ou banco do navegador. | SQLite real, reinício, fora de ordem, deduplicação, filtros e isolamento. |
| Presença | Assinatura, publicação, online/visto, digitação/gravação e eventos. | Presença desconhecida é null; TTL 5 min; sinais não renovam automaticamente. | HTTP, expiração, cancelamento e proteção contra pausa de comando antigo. |
| Webhooks | Eventos normalizados, fila/concorrência limitadas e retentativas. | Sem fila durável/garantia de ordem; consumidor deduplica X-WPP-Event-ID. | Servidor HTTP simulado com 503/429/4xx, payload/fila, orçamento e shutdown. |
| Mensagens históricas e filtros avançados | Oito rotas registradas e filtros sem suporte respondem 501. | Etiquetas, classificação de comunidades e consultas completas do navegador não implementadas. | Testes autenticados distinguem 400/401/404 de 501. |
| Implantação | Docker, Standalone/Swarm e imagens `0.1.3` amd64/arm64 publicadas. | Uma réplica por volume; testes de conta real continuam separados. | Smoke tests amd64/arm64 e validação das stacks aprovados no CI da release. |

## OpenAPI e e2e

`GET /openapi.json` expõe OpenAPI 3.0.3 sem autenticação. O artefato
[openapi.json](openapi.json) é exportado com `go run ./cmd/openapi`; a mesma
fonte gera a resposta HTTP. A descoberta de rotas usa o roteador real. Cada
operação tem contrato documentado, inclusive as não implementadas, evitando
listar como funcionais rotas que sempre retornam `501`.

O teste `TestOpenAPIValidAndExportCurrent` valida o documento com kin-openapi
e detecta exportação desatualizada. `TestCapabilitiesE2EAgainstOpenAPI` usa TCP
com o roteador HTTP real, valida requisições e respostas contra a especificação
e cobre bootstrap, sondas, close/logout, grupos, mídia, mensagens avançadas,
contatos, chats e presença.
O transporte WhatsApp é simulado. Testes específicos de cada capacidade cobrem
falhas e limites com mais detalhes. Isso não comprova pareamento ou envio numa
conta WhatsApp real; essa validação foi dispensada pelo usuário nesta entrega.

As fixtures de autenticação usam apenas credenciais de teste e um salt fixo,
para reprodução determinística. Foram geradas por Node bcrypt 6.0.0 e são
regeneradas no CI antes dos testes Go. Não reutilize esse salt em produção.

## Executar as verificações

```bash
go test ./...
go vet ./...
# Em ambiente com suporte ao detector de corridas e compilador C:
go test -race ./...

SECRET_KEY=ci-only-secret docker compose config --quiet
SECRET_KEY=ci-only-secret docker compose -f deploy/portainer-stack.yml config --quiet
SECRET_KEY=ci-only-secret docker stack config -c deploy/portainer-swarm.yml
```

`ci.yml` executa essas verificações em PRs. Os jobs de imagem usam o mesmo
comando `cmd/smoke-image` documentado no [guia de operação](operation.pt-BR.md).
O workflow de release só publica depois de suas verificações passarem. A release
`v0.1.3` inclui a base funcional; mudanças de documentação não alteram uma implantação existente.
