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
estão neste checkout e não na imagem
`0.1.2`. O [guia de implantação](docker-portainer.pt-BR.md) mostra os dois caminhos.

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
desta entrega. A validação manual com uma conta pareada e a publicação de uma
imagem com essas alterações ainda precisam acontecer.

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
normalização/encaminhamento e entrega dos eventos por webhook. A validação com
uma conta real e a publicação da imagem ainda precisam acontecer.

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
retornam `501`. A validação com conta real e a publicação continuam pendentes.

## 4. Operação e ciclo de sessão: implementada no código-fonte

- [x] Restaurar automaticamente sessões persistidas após reinício.
- [x] Separar desconexão de logout/revogação de credenciais.
- [x] Encerrar clientes e bancos no shutdown e testar recuperação após reinício.
- [x] Preparar publicação de imagens Linux arm64 e amd64 com testes das duas arquiteturas no CI.
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
multiarch exige os testes das imagens antes do push. A execução desse workflow
e a publicação efetiva das imagens dependem do merge/release. O build Docker
amd64 foi executado localmente; isso não equivale a executar o CI de arm64.

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

## Entrega por PR e publicação

A implementação das etapas está preparada para revisão em PR. Por orientação
do usuário, não foi feita validação com uma conta WhatsApp real e não será
criada release nesta entrega. A tag `0.1.2` continua representando a imagem
antiga. Os pipelines adicionados serão executados pelo GitHub na PR e na release;
resultados pendentes não são apresentados como já aprovados.

Histórias/status, catálogo, templates interativos e edição de perfil comercial
precisam de uma avaliação específica de suporte no whatsmeow antes de qualquer
promessa de paridade.
