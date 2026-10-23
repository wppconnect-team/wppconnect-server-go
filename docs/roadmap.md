# Roadmap: MVP até paridade

O Go server tem uma parte do contrato HTTP do servidor Node. Uma rota existente
não significa paridade completa de respostas, eventos ou comportamento. Este
arquivo registra entregas do código-fonte; a disponibilidade na imagem depende
da release publicada.

## Entregue

- [x] Ciclo básico de sessão, QR em PNG, envio de texto e webhook.
- [x] Envio básico de imagem base64, consulta de número, marcação de leitura e listagem de grupos.
- [x] Dockerfile, Compose e publicação versionada da imagem oficial Linux amd64.
- [x] Guia em português e stacks completas para Portainer Standalone e Swarm.
- [x] Geração de token bcrypt via `POST /api/{session}/{secretkey}/generate-token`.
- [x] Autenticação com hash bruto, token URL-safe e formato `session:token` do Node.
- [x] Gerador local `go run ./cmd/token`, com `-raw` para imagens antigas.
- [x] Testes HTTP de geração, autorização, isolamento entre sessões e troca de segredo.

As entregas de autenticação, mídia, grupos, contatos/chats/presença e operação
foram publicadas na [release v0.1.3](https://github.com/wppconnect-team/wppconnect-server-go/releases/tag/v0.1.3),
com imagens Linux amd64/arm64. A imagem antiga `0.1.2` não inclui essas etapas.
O [guia de implantação](docker-portainer.pt-BR.md) usa `0.1.3` como base.

## 1. Mídia: concluída no código-fonte

- [x] Detectar e preservar MIME no envio de imagens; remover o JPEG fixo.
- [x] Enviar arquivos e áudio com os campos e rotas base64/multipart do servidor Node.
- [x] Normalizar mensagens de imagem, documento, áudio e texto estendido no webhook.
- [x] Disponibilizar download de mídia com autenticação e isolamento por sessão.
- [x] Limitar payloads e testar base64 inválido, arquivo grande, MIME e falhas de upload/download.

Critério de conclusão: envio e recebimento dos tipos acima, download autorizado
e testes do contrato HTTP com falhas de rede simuladas, sem depender de uma
conta WhatsApp nos testes automatizados.

Validado por testes HTTP, webhook e descriptografia real do SDK com transporte
simulado. Veja [contratos, exemplos e limites de mídia](media.pt-BR.md).
O índice de descritores é limitado e fica em memória; caminhos locais/URLs de
upload, transcodificação, vídeo, stickers e opções avançadas do Node ficam fora
desta entrega. A validação manual com conta real continua sendo uma atividade
separada. A imagem `0.1.3` inclui essas alterações.

## 2. Grupos: concluída no código-fonte

- [x] Implementar `create-group` e `group-members`, removendo os placeholders `501`.
- [x] Adicionar operações de participantes e metadados conforme o contrato Node.
- [x] Encaminhar eventos de grupos e testar permissões/erros.

Inclui 22 rotas: listagem, membros/admins, grupos em comum, criação, quatro ações
de participantes, assunto/descrição/restrições, convites, entrada/saída e foto.
Rejeições individuais são preservadas e resultados parciais não são tratados
como sucesso integral. Webhooks cobrem participantes, entrada da conta e
metadados. Veja [contratos, exemplos e limites de grupos](groups.pt-BR.md).

Critério de conclusão atendido por testes HTTP de sucesso, autorização por
sessão, permissões, resultados parciais, limites e falhas de protocolo, além de
normalização/encaminhamento e entrega dos eventos por webhook. A imagem `0.1.3`
foi publicada; validação com conta real continua sendo uma atividade separada.

## 3. Contatos, chats e presença: concluída no código-fonte

- [x] Consulta de contatos e chats com envelopes e campos compatíveis, respeitando os dados disponíveis.
- [x] Presença, digitação e indicadores de gravação de áudio.
- [x] Registrar limitações e retornar erros explícitos para recursos sem suporte.

Inclui 16 rotas implementadas, índice SQLite persistente por sessão, ingestão
de histórico/eventos, paginação/filtros, contatos e associação PN/LID, consultas
de perfil, assinatura/publicação de presença e webhooks. Presença desconhecida
não é convertida em offline. O índice de chats contém metadados observados,
sem equivalência com o banco completo de mensagens do navegador.

Critério de conclusão atendido por testes HTTP, SQLite real com reabertura,
eventos fora de ordem, concorrência, expiração, timers, isolamento e entrega de
webhooks simulados. Veja [contratos e exemplos](contacts-chats-presence.pt-BR.md).
Filtros por etiquetas/comunidades e oito consultas históricas sem suporte
retornam `501`. A imagem `0.1.3` inclui a etapa; validação com conta real é separada.

## 4. Operação e ciclo de sessão: implementada no código-fonte

- [x] Restaurar automaticamente sessões persistidas após reinício.
- [x] Separar desconexão de logout/revogação de credenciais.
- [x] Encerrar clientes e bancos no shutdown e testar recuperação após reinício.
- [x] Publicar imagens Linux arm64 e amd64 após testes das duas arquiteturas no CI.
- [x] Adicionar verificações de inicialização, healthcheck, shutdown e reinício da imagem no CI.
- [x] Adicionar retentativas e limites à entrega de webhooks.

Restauração respeita fechamento manual e revogação, migra bancos legados
pareados e ignora bancos vazios. Cada cliente tem contexto de vida próprio;
cancelar a requisição HTTP após `start-session` não interrompe a sessão.
Shutdown trata SIGTERM/SIGINT, encerra HTTP, clientes, índices e bancos e drena
webhooks dentro de um prazo. Fila, workers, tentativas, payload e timeout de
webhook são limitados. Veja o [guia de operação](operation.pt-BR.md).

Os testes locais cobrem SQLite real, restauração, bancos corrompidos,
concorrência, cancelamento, logout recusado e retentativas HTTP. O workflow
multiarch exige os testes das imagens antes do push. A release `v0.1.3` foi
publicada com as duas arquiteturas após aprovação de testes Go com detector
de corridas, análise CodeQL e smoke tests das imagens no CI.

## 5. Contrato e qualidade: implementada no código-fonte

- [x] Expor OpenAPI refletindo as rotas implementadas e as respostas `501`.
- [x] Criar matriz de compatibilidade com o Node e testes e2e por capacidade.
- [x] Automatizar testes Go e validação das stacks em pull requests.
- [x] Cobrir compatibilidade de autenticação com fixtures geradas pelo Node.

`GET /openapi.json` e `go run ./cmd/openapi` usam o mesmo contrato, com cópia
em [openapi.json](openapi.json). O CI verifica a exportação, valida o documento
com kin-openapi, compara requisições/respostas de fluxos HTTP sobre TCP e roda
os testes Go com detector de corridas. A [matriz](compatibility.pt-BR.md)
registra cobertura e diferenças. Fixtures bcrypt são geradas com Node bcrypt
6.0.0 e verificadas no Go, incluindo Unicode e limite de 72 bytes.

## Base publicada e comparação com Evolution Go

A PR #5 foi integrada e originou a release `v0.1.3`. Os testes de transporte
WhatsApp são simulados; por orientação do usuário, não foi feito pareamento
com conta real nesta entrega. Isso não equivale a paridade completa com o
servidor Node ou com Evolution Go.

O [benchmark em inglês](evolution-go-benchmark.md) fixa Evolution Go `0.7.2`
no commit `9337afc47e10b86cc896a6f432240e40fee95dd1`, compara o código efetivo e
registra diferenças entre rotas anunciadas, implementações e itens com TODO.
As próximas etapas são futuras entregas; o README não as anuncia como prontas.

## 6. Expansão de capacidades: próximas etapas

- [x] Mensagens nativas adicionais: vídeo, stickers, contatos, localização e criação de enquetes (código-fonte).
- [ ] Ações/contexto: reação, quote/menções, edição/revogação e recibos com IDs explícitos.
- [ ] Persistência de mensagens/histórico com paginação, retenção e isolamento por sessão.
- [ ] Perfil, privacidade e bloqueios, com permissões e efeitos documentados.
- [ ] Mutações verificadas de chats e persistência/eventos de etiquetas.
- [ ] Outbox durável, replay e adaptadores opcionais para WebSocket, RabbitMQ e NATS.
- [ ] Armazenamento de mídia opcional em S3/MinIO com acesso privado e retenção.
- [ ] Pareamento por telefone, administração autenticada e configurações por sessão.
- [ ] Avaliar comunidades/newsletters, status, interativos, chamadas e votos de enquetes.
- [ ] Painel de gestão opcional, métricas e documentação complementar em inglês.
- [ ] Definir e publicar um arquivo `LICENSE` próprio do projeto, sem presumir a licença da referência.

A etapa de mensagens nativas adiciona oito rotas autenticadas: vídeo MP4/H.264
com legenda, stickers WebP estáticos/animados, vCards individuais/múltiplos,
localização estática e criação de enquetes com segredo independente por
destinatário. Inclui eventos normalizados, download de vídeo/sticker isolado
por sessão, validação de formato/tamanho, falhas parciais e exemplos completos
no [guia em inglês](advanced-messages.md). Não está incluída na imagem `0.1.3`.

Critério de conclusão: testes HTTP de sucesso/falha, limites e autorização;
contratos OpenAPI e fluxos TCP por capacidade; normalização e entrega de
webhooks; registro de mídia antes do evento; descriptografia/integridade real
do SDK para vídeo/stickers com transporte simulado. Sem pareamento de conta
real, conforme orientação do usuário. Os testes usam fixtures MP4/WebP reais
geradas sinteticamente. Não há transcodificação, leitura de caminhos/URLs,
localização ao vivo ou processamento de votos/resultados de enquetes nesta etapa.

Cada capacidade deve incluir contrato HTTP/OpenAPI, normalização de eventos
quando aplicável, limites, autorização por sessão e testes de sucesso/falha.
Adaptadores externos devem ter teste de integração e recuperação de falhas;
armazenamento deve definir retenção e migração. Endpoints sem implementação
continuam explícitos como não suportados. Não haverá conclusão por contagem
de rotas ou pela simples presença de um método no SDK.

Histórias/status, catálogo, templates interativos e edição de perfil comercial
precisam de uma avaliação específica de suporte no whatsmeow antes de qualquer
promessa de paridade.
